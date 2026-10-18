// Package middleware 提供 HTTP/3 優化的中間件系統
//
// @chris
package middleware

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

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

			if !config.DisablePrintStack {
				stack := make([]byte, config.StackSize)
				stack = stack[:runtime.Stack(stack, !config.DisableStackAll)]
				fmt.Fprintf(out, "[Recovery] %s %s panic recovered: %v\n%s\n",
					c.Request.Method, c.Request.URL.Path, err, stack)
			}

			if config.ErrorHandler != nil {
				config.ErrorHandler(c, err)
			} else {
				c.AbortWithStatus(http.StatusInternalServerError)
			}
			// 一律中止：自訂 ErrorHandler 若未 Abort，panic 那個 handler 之後的 handler
			// 會被外層的 Next 迴圈繼續執行
			c.Abort()
		}()

		c.Next()
	}
}
