// @chris
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"golang.org/x/time/rate"
)

// ===== 核心工具函數 =====

// secureRandHex 使用 crypto/rand 生成安全隨機 hex 字串
func secureRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// fallback: 用時間戳（極端情況）
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ===== 日誌中間件 =====

// LoggerConfig 日誌配置
type LoggerConfig struct {
	Format        string
	TimeFormat    string
	SkipPaths     []string
	Output        io.Writer
	EnableLatency bool
	EnableSize    bool
}

// Logger 創建日誌中間件
func Logger(config LoggerConfig) hypcontext.HandlerFunc {
	skipPaths := make(map[string]bool)
	for _, path := range config.SkipPaths {
		skipPaths[path] = true
	}

	if config.TimeFormat == "" {
		config.TimeFormat = time.RFC3339
	}

	return func(c *hypcontext.Context) {
		// 跳過特定路徑
		if skipPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// 處理請求
		c.Next()

		// 計算延遲
		latency := time.Since(start)

		// 獲取客戶端 IP
		clientIP := c.ClientIP()

		// 獲取狀態碼和回應大小
		statusCode := c.Response.Status()
		bodySize := c.Response.Size()

		// 記錄協議版本
		protocol := "HTTP/1.1"
		// 從 context 中獲取協議資訊
		if protoValue, exists := c.Get("protocol"); exists {
			if proto, ok := protoValue.(string); ok {
				protocol = proto
			}
		}

		// 格式化日誌
		if raw != "" {
			path = path + "?" + raw
		}

		logMessage := fmt.Sprintf("[%s] %s | %s | %d | %v | %d bytes | %s %s | %s",
			protocol,
			time.Now().Format(config.TimeFormat),
			clientIP,
			statusCode,
			latency,
			bodySize,
			c.Request.Method,
			path,
			c.GetHeader("User-Agent"),
		)

		// HTTP/3 特定資訊
		if protocol == "HTTP/3" {
			rtt := c.GetRTT()
			congWin := c.GetCongestionWindow()
			logMessage += fmt.Sprintf(" | RTT: %v | CongWin: %d", rtt, congWin)
		}

		// 輸出日誌
		if config.Output != nil {
			fmt.Fprintln(config.Output, logMessage)
		} else {
			fmt.Println(logMessage)
		}
	}
}

// ===== 速率限制中間件 =====

// RateLimiterConfig 速率限制配置
type RateLimiterConfig struct {
	Rate       int     // 每秒請求數
	Burst      int     // 突發請求數
	KeyFunc    KeyFunc // 獲取限制鍵的函數
	ErrorMsg   string  // 錯誤訊息
	StatusCode int     // 狀態碼
	UseHTTP3   bool    // 使用 HTTP/3 優化
}

// KeyFunc 獲取速率限制鍵的函數
type KeyFunc func(c *hypcontext.Context) string

// rateLimiterEntry 帶時間戳的限制器，用於過期清理
type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter 創建速率限制中間件
// 修復：加入定期清理機制，防止 sync.Map 因大量唯一 IP 導致記憶體洩漏
func RateLimiter(config RateLimiterConfig) hypcontext.HandlerFunc {
	limiters := &sync.Map{}

	if config.KeyFunc == nil {
		config.KeyFunc = func(c *hypcontext.Context) string {
			return c.ClientIP()
		}
	}

	if config.StatusCode == 0 {
		config.StatusCode = http.StatusTooManyRequests
	}

	// 定期清理過期的限制器（每 5 分鐘，清除 10 分鐘未見的 key）
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			limiters.Range(func(key, value interface{}) bool {
				if entry, ok := value.(*rateLimiterEntry); ok {
					if now.Sub(entry.lastSeen) > 10*time.Minute {
						limiters.Delete(key)
					}
				}
				return true
			})
		}
	}()

	return func(c *hypcontext.Context) {
		key := config.KeyFunc(c)

		// 獲取或創建限制器（帶時間戳）
		now := time.Now()
		entryInterface, loaded := limiters.LoadOrStore(key, &rateLimiterEntry{
			limiter:  rate.NewLimiter(rate.Limit(config.Rate), config.Burst),
			lastSeen: now,
		})
		entry := entryInterface.(*rateLimiterEntry)
		if loaded {
			entry.lastSeen = now // 更新最後見到時間
		}
		limiter := entry.limiter

		// HTTP/3 優化：使用 QUIC 的流控制特性
		if config.UseHTTP3 {
			// 檢查是否為 HTTP/3
			if protoValue, exists := c.Get("protocol"); exists {
				if proto, ok := protoValue.(string); ok && proto == "HTTP/3" {
					// 根據 RTT 動態調整速率
					rtt := c.GetRTT()
					if rtt > 100*time.Millisecond {
						// 高延遲時稍微放寬限制 (修正: 使用浮點數計算)
						adjustedRate := float64(config.Rate) * 1.2
						limiter.SetLimit(rate.Limit(adjustedRate))
					}
				}
			}
		}

		// 檢查速率限制
		if !limiter.Allow() {
			c.Header("Retry-After", "1")
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", config.Rate))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Second).Unix()))

			if config.ErrorMsg != "" {
				c.String(config.StatusCode, config.ErrorMsg)
			} else {
				c.AbortWithStatus(config.StatusCode)
			}
			return
		}

		c.Next()
	}
}

// ===== 超時中間件 =====

// TimeoutConfig 超時配置
type TimeoutConfig struct {
	Timeout      time.Duration
	ErrorMessage string
	ErrorCode    int
}

// Timeout 創建超時中間件
func Timeout(config TimeoutConfig) hypcontext.HandlerFunc {
	if config.ErrorCode == 0 {
		config.ErrorCode = http.StatusRequestTimeout
	}

	return func(c *hypcontext.Context) {
		// HTTP/3 優化：根據 RTT 動態調整超時
		timeout := config.Timeout
		if protoValue, exists := c.Get("protocol"); exists {
			if proto, ok := protoValue.(string); ok && proto == "HTTP/3" {
				rtt := c.GetRTT()
				if rtt > 0 {
					// 根據 RTT 調整超時時間
					timeout = timeout + rtt*2
				}
			}
		}

		// 創建超時上下文 (修正: 使用標準庫的 context.WithTimeout)
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		// 更新請求上下文
		c.Request = c.Request.WithContext(ctx)

		done := make(chan struct{})
		panicChan := make(chan interface{}, 1)

		go func() {
			defer func() {
				if p := recover(); p != nil {
					panicChan <- p
				}
			}()

			c.Next()
			close(done)
		}()

		select {
		case <-done:
			// 正常完成
		case p := <-panicChan:
			// 發生 panic
			panic(p)
		case <-ctx.Done():
			// 超時
			c.AbortWithStatus(config.ErrorCode)
			if config.ErrorMessage != "" {
				c.String(config.ErrorCode, config.ErrorMessage)
			}
		}
	}
}

// ===== BodyLimit 中間件 =====

// BodyLimitConfig 請求 body 大小限制配置
type BodyLimitConfig struct {
	MaxBytes int64  // 最大 body 大小（bytes），預設 1MB
	ErrorMsg string // 自訂錯誤訊息
}

// BodyLimit 限制請求 body 大小，防止大 payload 攻擊
func BodyLimit(config BodyLimitConfig) hypcontext.HandlerFunc {
	if config.MaxBytes <= 0 {
		config.MaxBytes = 1 << 20 // 1MB
	}
	if config.ErrorMsg == "" {
		config.ErrorMsg = "Request body too large"
	}

	return func(c *hypcontext.Context) {
		if c.Request.ContentLength > config.MaxBytes {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			c.String(http.StatusRequestEntityTooLarge, config.ErrorMsg)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, config.MaxBytes)
		c.Next()
	}
}

// ===== MethodOverride 中間件 =====

// MethodOverride 支援透過 header 或表單參數覆蓋 HTTP 方法
// 用於不支援 PUT/DELETE/PATCH 的客戶端（如 HTML 表單）
func MethodOverride() hypcontext.HandlerFunc {
	return func(c *hypcontext.Context) {
		if c.Request.Method == http.MethodPost {
			// 檢查 X-HTTP-Method-Override header
			if override := c.GetHeader("X-HTTP-Method-Override"); override != "" {
				c.Request.Method = strings.ToUpper(override)
			} else if override := c.Request.FormValue("_method"); override != "" {
				// 檢查 _method 表單參數
				c.Request.Method = strings.ToUpper(override)
			}
		}
		c.Next()
	}
}

// ===== 預設中間件配置 =====

// DefaultMiddleware 創建預設中間件組合
func DefaultMiddleware() []hypcontext.HandlerFunc {
	return []hypcontext.HandlerFunc{
		// 錯誤恢復
		Recovery(RecoveryConfig{
			StackSize:         4 << 10,
			DisablePrintStack: false,
		}),
		// 日誌
		Logger(LoggerConfig{
			TimeFormat: time.RFC3339,
		}),
		// 安全頭
		Security(SecurityConfig{
			XSSProtection:      "1; mode=block",
			ContentTypeNosniff: "nosniff",
			XFrameOptions:      "DENY",
		}),
		// CORS
		CORS(CORSConfig{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders: []string{"Origin", "Content-Type", "Accept"},
		}),
	}
}

// HTTP3Middleware 創建 HTTP/3 優化的中間件組合
func HTTP3Middleware() []hypcontext.HandlerFunc {
	return []hypcontext.HandlerFunc{
		// 錯誤恢復
		Recovery(RecoveryConfig{
			StackSize:         4 << 10,
			DisablePrintStack: false,
		}),
		// 日誌
		Logger(LoggerConfig{
			TimeFormat:    time.RFC3339,
			EnableLatency: true,
			EnableSize:    true,
		}),
		// 壓縮（優先使用 Brotli）
		Compression(CompressionConfig{
			Level:        6,
			MinLength:    1024,
			PreferBrotli: true,
		}),
		// 速率限制（啟用 HTTP/3 優化）
		RateLimiter(RateLimiterConfig{
			Rate:     100,
			Burst:    50,
			UseHTTP3: true,
		}),
	}
}
