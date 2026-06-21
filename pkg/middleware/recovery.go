// Package middleware 提供 HTTP/3 優化的中間件系統
//
// @chris
package middleware

import (
	"fmt"
	"net/http"
	"runtime"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// ===== Recovery 中間件 =====

// RecoveryConfig Recovery 配置
type RecoveryConfig struct {
	StackSize         int
	DisableStackAll   bool
	DisablePrintStack bool
	LogLevel          string
	ErrorHandler      func(c *hypcontext.Context, err interface{})
}

// Recovery 創建錯誤恢復中間件
func Recovery(config RecoveryConfig) hypcontext.HandlerFunc {
	if config.StackSize == 0 {
		config.StackSize = 4 << 10 // 4KB
	}

	return func(c *hypcontext.Context) {
		defer func() {
			if err := recover(); err != nil {
				// 獲取堆疊資訊
				stack := make([]byte, config.StackSize)
				length := runtime.Stack(stack, !config.DisableStackAll)
				stack = stack[:length]

				// 記錄錯誤
				if !config.DisablePrintStack {
					fmt.Printf("[Recovery] panic recovered:\n%s\n%s\n", err, stack)
				}

				// HTTP/3 特定處理：確保流正確關閉
				if protoValue, exists := c.Get("protocol"); exists {
					if proto, ok := protoValue.(string); ok && proto == "HTTP/3" {
						// 關閉 QUIC 流
						// 這裡需要實際的流關閉邏輯
					}
				}

				// 執行自定義錯誤處理器
				if config.ErrorHandler != nil {
					config.ErrorHandler(c, err)
				} else {
					c.AbortWithStatus(http.StatusInternalServerError)
				}
			}
		}()

		c.Next()
	}
}

// SimpleErrorHandler 簡單的錯誤處理中間件
func SimpleErrorHandler() hypcontext.HandlerFunc {
	return func(c *hypcontext.Context) {
		defer func() {
			if r := recover(); r != nil {
				var err error
				switch x := r.(type) {
				case string:
					err = fmt.Errorf("%s", x)
				case error:
					err = x
				default:
					err = fmt.Errorf("unknown panic: %v", r)
				}

				// 記錄錯誤並返回 500
				fmt.Printf("[Error] Panic recovered: %v\n", err)
				c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "Internal Server Error",
				})
			}
		}()

		c.Next()
	}
}
