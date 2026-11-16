// Package middleware 提供 HTTP/3 優化的中間件系統
//
// @chris
package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	hyperrors "github.com/maoxiaoyue/hypgo/pkg/errors"
)

// clientGone 判斷 panic 是否只是客戶端中途斷線：panic 值是 error（不是 nil pointer 這類
// 執行期錯誤），而且請求的 context 已被取消——net/http 在寫入失敗或連線關閉時取消它
func clientGone(c *hypcontext.Context, v interface{}) bool {
	err, ok := v.(error)
	if !ok {
		return false
	}
	if _, isRuntime := err.(runtime.Error); isRuntime {
		return false
	}
	return errors.Is(c.Request.Context().Err(), context.Canceled)
}

// ===== Recovery 中間件 =====

// RecoveryConfig Recovery 配置
type RecoveryConfig struct {
	StackSize         int
	DisableStackAll   bool
	DisablePrintStack bool
	ErrorHandler      func(c *hypcontext.Context, err interface{})
	Output            io.Writer // panic 記錄寫到哪；nil 時為 os.Stdout，server 會接到自己的 logger
}

// Recovery 創建錯誤恢復中間件
func Recovery(config RecoveryConfig) hypcontext.HandlerFunc {
	if config.StackSize == 0 {
		config.StackSize = 4 << 10 // 4KB
	}
	out := config.Output
	if out == nil {
		out = os.Stdout
	}

	return func(c *hypcontext.Context) {
		defer func() {
			err := recover()
			if err == nil {
				return
			}
			// net/http 慣例：http.ErrAbortHandler 用來安靜地中止連線（不記錄、不回應），
			// 必須繼續往上拋；先前被吞掉並當成一般錯誤印出堆疊、回 500
			if err == http.ErrAbortHandler {
				panic(err)
			}

			// Timeout 轉交的 panic：用它在 handler goroutine 擷取的堆疊（這裡的堆疊沒有出事的函式）
			var stack []byte
			if pe, ok := err.(*PanicError); ok {
				err, stack = pe.Value, pe.Stack
				if len(stack) > config.StackSize {
					stack = stack[:config.StackSize]
				}
			}

			// 客戶端斷線時 c.JSON 等寫入失敗會 panic：這不是程式錯誤，記一行即可。
			// 先前每次斷線都印一份堆疊並試著回 500
			if clientGone(c, err) {
				fmt.Fprintf(out, "[Recovery] %s %s client disconnected: %v\n",
					logSafe(c.Request.Method), logSafe(c.Request.URL.Path), err)
				c.Abort()
				return
			}

			if !config.DisablePrintStack {
				if stack == nil {
					stack = make([]byte, config.StackSize)
					stack = stack[:runtime.Stack(stack, !config.DisableStackAll)]
				}
				fmt.Fprintf(out, "[Recovery] %s %s panic recovered: %v\n%s\n",
					logSafe(c.Request.Method), logSafe(c.Request.URL.Path), err, stack)
			}

			if config.ErrorHandler != nil {
				config.ErrorHandler(c, err)
			} else {
				// 寫回應本身也可能失敗（連線已壞）；這裡再 panic 就沒有人接了
				func() {
					defer func() { recover() }()
					abortError(c, http.StatusInternalServerError, hyperrors.ErrInternalError)
				}()
			}
			// 一律中止：自訂 ErrorHandler 若未 Abort，panic 那個 handler 之後的 handler
			// 會被外層的 Next 迴圈繼續執行
			c.Abort()
		}()

		c.Next()
	}
}
