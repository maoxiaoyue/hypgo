// Package middleware 提供 HTTP/3 優化的中間件系統
package middleware

import (
	"bytes"
	"compress/gzip"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/context"
	"golang.org/x/time/rate"
)

// ===== CORS 中間件 =====

// CORSConfig CORS 配置
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

// CORS 創建 CORS 中間件
func CORS(config CORSConfig) context.HandlerFunc {
	// 預處理允許的來源
	allowOrigins := make(map[string]bool)
	for _, origin := range config.AllowOrigins {
		allowOrigins[origin] = true
	}

	return func(c *context.Context) {
		origin := c.GetHeader("Origin")

		// 檢查來源是否允許
		if origin != "" {
			allowed := false
			if allowOrigins["*"] {
				allowed = true
			} else if allowOrigins[origin] {
				allowed = true
			}

			if allowed {
				c.Header("Access-Control-Allow-Origin", origin)

				if config.AllowCredentials {
					c.Header("Access-Control-Allow-Credentials", "true")
				}

				if len(config.ExposeHeaders) > 0 {
					c.Header("Access-Control-Expose-Headers", strings.Join(config.ExposeHeaders, ","))
				}
			}
		}

		// 處理預檢請求
		if c.Request.Method == "OPTIONS" {
			c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowMethods, ","))
			c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ","))

			if config.MaxAge > 0 {
				c.Header("Access-Control-Max-Age", fmt.Sprintf("%d", config.MaxAge))
			}

			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
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
func Logger(config LoggerConfig) context.HandlerFunc {
	skipPaths := make(map[string]bool)
	for _, path := range config.SkipPaths {
		skipPaths[path] = true
	}

	if config.TimeFormat == "" {
		config.TimeFormat = time.RFC3339
	}

	return func(c *context.Context) {
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
		switch c.Protocol {
		case context.HTTP2:
			protocol = "HTTP/2"
		case context.HTTP3:
			protocol = "HTTP/3"
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
		if c.Protocol == context.HTTP3 {
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
type KeyFunc func(c *context.Context) string

// RateLimiter 創建速率限制中間件
func RateLimiter(config RateLimiterConfig) context.HandlerFunc {
	limiters := &sync.Map{}

	if config.KeyFunc == nil {
		config.KeyFunc = func(c *context.Context) string {
			return c.ClientIP()
		}
	}

	if config.StatusCode == 0 {
		config.StatusCode = http.StatusTooManyRequests
	}

	return func(c *context.Context) {
		key := config.KeyFunc(c)

		// 獲取或創建限制器
		limiterInterface, _ := limiters.LoadOrStore(key, rate.NewLimiter(rate.Limit(config.Rate), config.Burst))
		limiter := limiterInterface.(*rate.Limiter)

		// HTTP/3 優化：使用 QUIC 的流控制特性
		if config.UseHTTP3 && c.Protocol == context.HTTP3 {
			// 根據 RTT 動態調整速率
			rtt := c.GetRTT()
			if rtt > 100*time.Millisecond {
				// 高延遲時稍微放寬限制
				limiter.SetLimit(rate.Limit(config.Rate * 1.2))
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

// ===== 壓縮中間件 =====

// CompressionConfig 壓縮配置
type CompressionConfig struct {
	Level         int
	MinLength     int
	ExcludedPaths []string
	ExcludedTypes []string
	PreferBrotli  bool // HTTP/3 優化：優先使用 Brotli
}

// Compression 創建壓縮中間件
func Compression(config CompressionConfig) context.HandlerFunc {
	if config.Level == 0 {
		config.Level = gzip.DefaultCompression
	}

	if config.MinLength == 0 {
		config.MinLength = 1024
	}

	excludedPaths := make(map[string]bool)
	for _, path := range config.ExcludedPaths {
		excludedPaths[path] = true
	}

	return func(c *context.Context) {
		if excludedPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		// 檢查客戶端支援的編碼
		acceptEncoding := c.GetHeader("Accept-Encoding")

		// HTTP/3 優化：優先使用 Brotli
		if config.PreferBrotli && c.Protocol == context.HTTP3 && strings.Contains(acceptEncoding, "br") {
			// 使用 Brotli 壓縮
			c.Header("Content-Encoding", "br")
			// 實現 Brotli 壓縮邏輯
		} else if strings.Contains(acceptEncoding, "gzip") {
			// 使用 Gzip 壓縮
			c.Header("Content-Encoding", "gzip")
			c.Header("Vary", "Accept-Encoding")

			gz := gzip.NewWriter(c.Response)
			defer gz.Close()

			c.Response = &gzipWriter{ResponseWriter: c.Response, Writer: gz}
		}

		c.Next()
	}
}

// gzipWriter 包裝 ResponseWriter 以支援 gzip
type gzipWriter struct {
	context.ResponseWriter
	io.Writer
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	return g.Writer.Write(data)
}

// ===== 認證中間件 =====

// AuthConfig 認證配置
type AuthConfig struct {
	Realm      string
	Authorized map[string]string // username -> password
	Validator  AuthValidator
}

// AuthValidator 認證驗證器
type AuthValidator func(username, password string, c *context.Context) bool

// BasicAuth 創建基本認證中間件
func BasicAuth(config AuthConfig) context.HandlerFunc {
	if config.Realm == "" {
		config.Realm = "Restricted"
	}

	return func(c *context.Context) {
		// 獲取認證頭
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.Header("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, config.Realm))
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 解析認證資訊
		const prefix = "Basic "
		if !strings.HasPrefix(auth, prefix) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 解碼認證資訊
		decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 分離使用者名稱和密碼
		parts := bytes.SplitN(decoded, []byte(":"), 2)
		if len(parts) != 2 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		username := string(parts[0])
		password := string(parts[1])

		// 驗證認證資訊
		valid := false
		if config.Validator != nil {
			valid = config.Validator(username, password, c)
		} else if expectedPass, ok := config.Authorized[username]; ok {
			valid = subtle.ConstantTimeCompare([]byte(password), []byte(expectedPass)) == 1
		}

		if !valid {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 設置使用者資訊
		c.Set("user", username)
		c.Next()
	}
}

// ===== JWT 中間件 =====

// JWTConfig JWT 配置
type JWTConfig struct {
	SigningKey    []byte
	SigningMethod string
	ContextKey    string
	TokenLookup   string // "header:Authorization" or "query:token" or "cookie:token"
	TokenHeadName string // "Bearer"
	Claims        interface{}
	ErrorHandler  func(c *context.Context, err error)
}

// JWT 創建 JWT 中間件
func JWT(config JWTConfig) context.HandlerFunc {
	if config.ContextKey == "" {
		config.ContextKey = "user"
	}

	if config.TokenLookup == "" {
		config.TokenLookup = "header:Authorization"
	}

	if config.TokenHeadName == "" {
		config.TokenHeadName = "Bearer"
	}

	return func(c *context.Context) {
		// 提取 token
		token := extractToken(c, config.TokenLookup, config.TokenHeadName)
		if token == "" {
			if config.ErrorHandler != nil {
				config.ErrorHandler(c, fmt.Errorf("token not found"))
			} else {
				c.AbortWithStatus(http.StatusUnauthorized)
			}
			return
		}

		// 驗證 token
		// 這裡需要實際的 JWT 驗證邏輯
		// claims, err := validateToken(token, config.SigningKey)

		// 設置使用者資訊
		// c.Set(config.ContextKey, claims)

		c.Next()
	}
}

// extractToken 從請求中提取 token
func extractToken(c *context.Context, lookup, headName string) string {
	parts := strings.Split(lookup, ":")
	if len(parts) != 2 {
		return ""
	}

	switch parts[0] {
	case "header":
		token := c.GetHeader(parts[1])
		if token != "" && headName != "" {
			parts := strings.SplitN(token, " ", 2)
			if len(parts) == 2 && parts[0] == headName {
				return parts[1]
			}
		}
		return token
	case "query":
		return c.Query(parts[1])
	case "cookie":
		cookie, _ := c.Cookie(parts[1])
		return cookie
	}

	return ""
}

// ===== Recovery 中間件 =====

// RecoveryConfig Recovery 配置
type RecoveryConfig struct {
	StackSize         int
	DisableStackAll   bool
	DisablePrintStack bool
	LogLevel          string
	ErrorHandler      func(c *context.Context, err interface{})
}

// Recovery 創建錯誤恢復中間件
func Recovery(config RecoveryConfig) context.HandlerFunc {
	if config.StackSize == 0 {
		config.StackSize = 4 << 10 // 4KB
	}

	return func(c *context.Context) {
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
				if c.Protocol == context.HTTP3 {
					// 關閉 QUIC 流
					// 這裡需要實際的流關閉邏輯
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

// ===== 請求 ID 中間件 =====

// RequestIDConfig 請求 ID 配置
type RequestIDConfig struct {
	Header    string
	Generator func() string
}

// RequestID 創建請求 ID 中間件
func RequestID(config RequestIDConfig) context.HandlerFunc {
	if config.Header == "" {
		config.Header = "X-Request-ID"
	}

	if config.Generator == nil {
		config.Generator = generateRequestID
	}

	return func(c *context.Context) {
		// 檢查是否已有請求 ID
		requestID := c.GetHeader(config.Header)
		if requestID == "" {
			requestID = config.Generator()
		}

		// 設置請求 ID
		c.Set("request_id", requestID)
		c.Header(config.Header, requestID)

		c.Next()
	}
}

// generateRequestID 生成請求 ID
func generateRequestID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), fastrand())
}

// fastrand 快速隨機數生成
func fastrand() uint32 {
	return uint32(time.Now().UnixNano())
}

// ===== 超時中間件 =====

// TimeoutConfig 超時配置
type TimeoutConfig struct {
	Timeout      time.Duration
	ErrorMessage string
	ErrorCode    int
}

// Timeout 創建超時中間件
func Timeout(config TimeoutConfig) context.HandlerFunc {
	if config.ErrorCode == 0 {
		config.ErrorCode = http.StatusRequestTimeout
	}

	return func(c *context.Context) {
		// HTTP/3 優化：根據 RTT 動態調整超時
		timeout := config.Timeout
		if c.Protocol == context.HTTP3 {
			rtt := c.GetRTT()
			if rtt > 0 {
				// 根據 RTT 調整超時時間
				timeout = timeout + rtt*2
			}
		}

		// 創建超時上下文
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

// ===== HTTP/3 Server Push 中間件 =====

// ServerPushConfig Server Push 配置
type ServerPushConfig struct {
	Rules []PushRule
}

// PushRule Server Push 規則
type PushRule struct {
	Path      string
	Resources []string
	Condition func(c *context.Context) bool
}

// ServerPush 創建 Server Push 中間件
func ServerPush(config ServerPushConfig) context.HandlerFunc {
	// 預編譯路徑匹配
	rules := make(map[string][]string)
	conditions := make(map[string]func(c *context.Context) bool)

	for _, rule := range config.Rules {
		rules[rule.Path] = rule.Resources
		if rule.Condition != nil {
			conditions[rule.Path] = rule.Condition
		}
	}

	return func(c *context.Context) {
		// 只在 HTTP/2 和 HTTP/3 中啟用
		if c.Protocol < context.HTTP2 {
			c.Next()
			return
		}

		path := c.Request.URL.Path

		// 查找匹配的規則
		if resources, ok := rules[path]; ok {
			// 檢查條件
			if condition, hasCondition := conditions[path]; hasCondition {
				if !condition(c) {
					c.Next()
					return
				}
			}

			// 推送資源
			for _, resource := range resources {
				if err := c.Push(resource, nil); err != nil {
					// 記錄推送失敗
					fmt.Printf("Failed to push %s: %v\n", resource, err)
				}
			}
		}

		c.Next()
	}
}

// ===== 快取中間件 =====

// CacheConfig 快取配置
type CacheConfig struct {
	TTL          time.Duration
	KeyGenerator func(c *context.Context) string
	Validator    func(c *context.Context) bool
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
func Cache(config CacheConfig) context.HandlerFunc {
	if config.KeyGenerator == nil {
		config.KeyGenerator = func(c *context.Context) string {
			return c.Request.Method + ":" + c.Request.URL.Path
		}
	}

	if config.TTL == 0 {
		config.TTL = 5 * time.Minute
	}

	return func(c *context.Context) {
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
		if c.Protocol == context.HTTP3 {
			key = "h3:" + key
		}

		// 嘗試從快取獲取
		if cached, found := config.Store.Get(key); found {
			// 設置快取頭
			c.Header("X-Cache", "HIT")
			if config.CacheControl != "" {
				c.Header("Cache-Control", config.CacheControl)
			}

			// 返回快取內容
			c.Data(http.StatusOK, "application/json", cached)
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
	context.ResponseWriter
	body *bytes.Buffer
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	r.body.Write(data)
	return r.ResponseWriter.Write(data)
}

// ===== 重試中間件（HTTP/3 優化）=====

// RetryConfig 重試配置
type RetryConfig struct {
	MaxAttempts int
	Delay       time.Duration
	MaxDelay    time.Duration
	BackoffFunc func(attempt int) time.Duration
	ShouldRetry func(c *context.Context, err error) bool
}

// Retry 創建重試中間件（針對 HTTP/3 優化）
func Retry(config RetryConfig) context.HandlerFunc {
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 3
	}

	if config.Delay == 0 {
		config.Delay = 100 * time.Millisecond
	}

	if config.MaxDelay == 0 {
		config.MaxDelay = 5 * time.Second
	}

	if config.BackoffFunc == nil {
		config.BackoffFunc = func(attempt int) time.Duration {
			delay := config.Delay * time.Duration(1<<uint(attempt))
			if delay > config.MaxDelay {
				return config.MaxDelay
			}
			return delay
		}
	}

	return func(c *context.Context) {
		var lastErr error

		for attempt := 0; attempt < config.MaxAttempts; attempt++ {
			// 執行請求
			err := executeWithRecovery(c)

			if err == nil {
				// 成功
				return
			}

			lastErr = err

			// 檢查是否應該重試
			if config.ShouldRetry != nil && !config.ShouldRetry(c, err) {
				break
			}

			// HTTP/3 優化：根據 RTT 調整延遲
			delay := config.BackoffFunc(attempt)
			if c.Protocol == context.HTTP3 {
				rtt := c.GetRTT()
				if rtt > 0 {
					// 根據網路延遲調整重試延遲
					delay = delay + rtt
				}
			}

			// 等待後重試
			if attempt < config.MaxAttempts-1 {
				time.Sleep(delay)
			}
		}

		// 所有重試都失敗
		if lastErr != nil {
			c.Error(lastErr)
			c.AbortWithStatus(http.StatusServiceUnavailable)
		}
	}
}

// executeWithRecovery 執行請求並捕獲錯誤
func executeWithRecovery(c *context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	c.Next()

	// 檢查是否有錯誤
	if len(c.Errors) > 0 {
		return c.Errors[0].Err
	}

	// 檢查狀態碼
	status := c.Response.Status()
	if status >= 500 {
		return fmt.Errorf("server error: %d", status)
	}

	return nil
}

// ===== 安全頭中間件 =====

// SecurityConfig 安全配置
type SecurityConfig struct {
	XSSProtection         string
	ContentTypeNosniff    string
	XFrameOptions         string
	HSTSMaxAge            int
	HSTSIncludeSubdomains bool
	ContentSecurityPolicy string
	ReferrerPolicy        string
}

// Security 創建安全頭中間件
func Security(config SecurityConfig) context.HandlerFunc {
	return func(c *context.Context) {
		// XSS 保護
		if config.XSSProtection != "" {
			c.Header("X-XSS-Protection", config.XSSProtection)
		} else {
			c.Header("X-XSS-Protection", "1; mode=block")
		}

		// 防止 MIME 類型嗅探
		if config.ContentTypeNosniff != "" {
			c.Header("X-Content-Type-Options", config.ContentTypeNosniff)
		} else {
			c.Header("X-Content-Type-Options", "nosniff")
		}

		// 防止點擊劫持
		if config.XFrameOptions != "" {
			c.Header("X-Frame-Options", config.XFrameOptions)
		} else {
			c.Header("X-Frame-Options", "DENY")
		}

		// HSTS
		if config.HSTSMaxAge > 0 {
			value := fmt.Sprintf("max-age=%d", config.HSTSMaxAge)
			if config.HSTSIncludeSubdomains {
				value += "; includeSubDomains"
			}
			c.Header("Strict-Transport-Security", value)
		}

		// CSP
		if config.ContentSecurityPolicy != "" {
			c.Header("Content-Security-Policy", config.ContentSecurityPolicy)
		}

		// Referrer Policy
		if config.ReferrerPolicy != "" {
			c.Header("Referrer-Policy", config.ReferrerPolicy)
		}

		c.Next()
	}
}
