package middleware

import (
	"bytes"
	"context"
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

// fastrand 快速隨機數生成
func fastrand() uint32 {
	return uint32(time.Now().UnixNano())
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

// RateLimiter 創建速率限制中間件
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

	return func(c *hypcontext.Context) {
		key := config.KeyFunc(c)

		// 獲取或創建限制器
		limiterInterface, _ := limiters.LoadOrStore(key, rate.NewLimiter(rate.Limit(config.Rate), config.Burst))
		limiter := limiterInterface.(*rate.Limiter)

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

// ===== 快取中間件 =====

// CacheConfig 快取配置
type CacheConfig struct {
	TTL          time.Duration
	KeyGenerator func(c *hypcontext.Context) string
	Validator    func(c *hypcontext.Context) bool
	Store        CacheStore
	CacheControl string
	VaryHeaders  []string
}

// CacheStore 快取存儲介面
type CacheStore interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte, ttl time.Duration)
	Delete(key string)
}

// Cache 創建快取中間件
func Cache(config CacheConfig) hypcontext.HandlerFunc {
	if config.KeyGenerator == nil {
		config.KeyGenerator = func(c *hypcontext.Context) string {
			return c.Request.Method + ":" + c.Request.URL.Path
		}
	}

	if config.TTL == 0 {
		config.TTL = 5 * time.Minute
	}

	return func(c *hypcontext.Context) {
		// 只快取 GET 請求
		if c.Request.Method != http.MethodGet {
			c.Next()
			return
		}

		// 檢查是否應該快取
		if config.Validator != nil && !config.Validator(c) {
			c.Next()
			return
		}

		// 生成快取鍵
		key := config.KeyGenerator(c)

		// HTTP/3 優化：包含協議版本在快取鍵中
		if protoValue, exists := c.Get("protocol"); exists {
			if proto, ok := protoValue.(string); ok && proto == "HTTP/3" {
				key = "h3:" + key
			}
		}

		// 嘗試從快取獲取
		if cached, found := config.Store.Get(key); found {
			// 設置快取頭
			c.Header("X-Cache", "HIT")
			if config.CacheControl != "" {
				c.Header("Cache-Control", config.CacheControl)
			}

			// 返回快取內容
			c.Header("Content-Type", "application/json")
			c.Status(http.StatusOK)
			c.Response.Write(cached)
			c.Abort()
			return
		}

		// 創建響應記錄器
		rec := &responseRecorder{
			ResponseWriter: c.Response,
			body:           &bytes.Buffer{},
		}
		c.Response = rec

		c.Next()

		// 如果請求成功，快取響應
		if rec.Status() == http.StatusOK {
			config.Store.Set(key, rec.body.Bytes(), config.TTL)

			// 設置快取頭
			c.Header("X-Cache", "MISS")
			if config.CacheControl != "" {
				c.Header("Cache-Control", config.CacheControl)
			}

			// 設置 Vary 頭
			if len(config.VaryHeaders) > 0 {
				c.Header("Vary", strings.Join(config.VaryHeaders, ", "))
			}
		}
	}
}

// responseRecorder 記錄響應
type responseRecorder struct {
	hypcontext.ResponseWriter
	body *bytes.Buffer
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	r.body.Write(data)
	return r.ResponseWriter.Write(data)
}

// ===== 中間件鏈 =====

// Chain 中間件鏈
type Chain struct {
	middlewares []hypcontext.HandlerFunc
}

// NewChain 創建中間件鏈
func NewChain(middlewares ...hypcontext.HandlerFunc) Chain {
	return Chain{
		middlewares: middlewares,
	}
}

// Append 添加中間件
func (c Chain) Append(middlewares ...hypcontext.HandlerFunc) Chain {
	newMiddlewares := make([]hypcontext.HandlerFunc, 0, len(c.middlewares)+len(middlewares))
	newMiddlewares = append(newMiddlewares, c.middlewares...)
	newMiddlewares = append(newMiddlewares, middlewares...)
	return Chain{middlewares: newMiddlewares}
}

// Then 執行中間件鏈
func (c Chain) Then(handler hypcontext.HandlerFunc) hypcontext.HandlerFunc {
	if handler == nil {
		panic("handler cannot be nil")
	}

	// 構建中間件鏈
	return func(ctx *hypcontext.Context) {
		// 將所有中間件和最終處理器組合
		handlers := make([]hypcontext.HandlerFunc, 0, len(c.middlewares)+1)
		handlers = append(handlers, c.middlewares...)
		handlers = append(handlers, handler)

		// 設置處理器鏈
		for _, h := range handlers {
			h(ctx)
			// 如果上下文被中止，停止執行
			if ctx.Response.Written() {
				break
			}
		}
	}
}

// ===== 中間件組 =====

// Group 中間件組
type Group struct {
	prefix      string
	middlewares []hypcontext.HandlerFunc
}

// NewGroup 創建中間件組
func NewGroup(prefix string, middlewares ...hypcontext.HandlerFunc) *Group {
	return &Group{
		prefix:      prefix,
		middlewares: middlewares,
	}
}

// Use 添加中間件到組
func (g *Group) Use(middlewares ...hypcontext.HandlerFunc) {
	g.middlewares = append(g.middlewares, middlewares...)
}

// Handle 處理請求
func (g *Group) Handle(handler hypcontext.HandlerFunc) hypcontext.HandlerFunc {
	chain := NewChain(g.middlewares...)
	return chain.Then(handler)
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
		// Server Push
		ServerPush(ServerPushConfig{
			Rules: []PushRule{
				{
					Path:      "/",
					Resources: []string{"/static/css/main.css", "/static/js/app.js"},
				},
			},
		}),
		// 速率限制（啟用 HTTP/3 優化）
		RateLimiter(RateLimiterConfig{
			Rate:     100,
			Burst:    50,
			UseHTTP3: true,
		}),
	}
}
