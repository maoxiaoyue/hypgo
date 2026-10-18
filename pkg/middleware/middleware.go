// @chris
package middleware

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	Output        io.Writer // nil 時寫到 os.Stdout；server 會接到自己的 logger
	EnableLatency bool
	EnableSize    bool
}

// sensitiveQueryKeys 記錄日誌時遮蔽值的 query 參數（不分大小寫）。
// JWT / APIKey 中間件都支援從 query 取憑證，原樣寫進 log 等於把憑證落地
var sensitiveQueryKeys = map[string]bool{
	"token": true, "access_token": true, "refresh_token": true, "id_token": true,
	"api_key": true, "apikey": true, "key": true, "password": true, "passwd": true,
	"secret": true, "client_secret": true, "signature": true, "sig": true, "code": true, "auth": true,
}

// redactQuery 把敏感參數的值換成 REDACTED，其餘維持原樣與順序
func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	changed := false
	for i, p := range parts {
		if k, _, ok := strings.Cut(p, "="); ok && sensitiveQueryKeys[strings.ToLower(k)] {
			parts[i] = k + "=REDACTED"
			changed = true
		}
	}
	if !changed {
		return raw
	}
	return strings.Join(parts, "&")
}

// Logger 創建日誌中間件。
// 以 defer 記錄：即使下游 panic 也會留下一筆 500（再把 panic 交給外層 Recovery）——
// 先前 Recovery 在外、Logger 在內時，panic 直接穿過 Logger，出錯的請求反而沒有日誌
func Logger(config LoggerConfig) hypcontext.HandlerFunc {
	skipPaths := make(map[string]bool)
	for _, path := range config.SkipPaths {
		skipPaths[path] = true
	}
	if config.TimeFormat == "" {
		config.TimeFormat = time.RFC3339
	}
	out := config.Output
	if out == nil {
		out = os.Stdout
	}

	return func(c *hypcontext.Context) {
		if skipPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		start := time.Now()
		path := c.Request.URL.Path
		if raw := redactQuery(c.Request.URL.RawQuery); raw != "" {
			path += "?" + raw
		}

		defer func() {
			p := recover()
			status := c.Response.Status()
			if p != nil {
				status = http.StatusInternalServerError // panic 時尚未寫出狀態碼；外層 Recovery 會回 500
			}
			// 協議取自請求本身（HTTP/1.1、HTTP/2.0、HTTP/3.0）：先前讀 c.Get("protocol")，
			// 這個 key 從來沒有人設，所有請求都被記成 HTTP/1.1
			fmt.Fprintf(out, "[%s] %s | %s | %d | %v | %d bytes | %s %s | %s\n",
				c.Request.Proto,
				time.Now().Format(config.TimeFormat),
				c.ClientIP(),
				status,
				time.Since(start),
				c.Response.Size(),
				c.Request.Method,
				path,
				c.GetHeader("User-Agent"),
			)
			if p != nil {
				panic(p)
			}
		}()

		c.Next()
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
	lastSeen atomic.Int64 // UnixNano；多個請求並行更新、清理時讀取（先前為 time.Time 欄位直接讀寫，data race）
}

const (
	rateLimiterSweepEvery = 5 * time.Minute  // 多久清理一次
	rateLimiterIdleTTL    = 10 * time.Minute // 多久未見的 key 會被清掉
)

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
	limitHeader := strconv.Itoa(config.Rate)

	// 清理改為由請求順帶觸發（每 rateLimiterSweepEvery 最多一次、在短命 goroutine 執行）：
	// 先前每呼叫一次 RateLimiter() 就啟動一個永不結束的 ticker goroutine
	var lastSweep atomic.Int64
	lastSweep.Store(time.Now().UnixNano())
	sweep := func(now int64) {
		limiters.Range(func(key, value interface{}) bool {
			if now-value.(*rateLimiterEntry).lastSeen.Load() > int64(rateLimiterIdleTTL) {
				limiters.Delete(key)
			}
			return true
		})
	}

	return func(c *hypcontext.Context) {
		key := config.KeyFunc(c)
		now := time.Now().UnixNano()

		// 先 Load：已存在的 key 不必建立新的 limiter。先前每個請求都先 rate.NewLimiter 再
		// LoadOrStore，命中時白白配置（3 allocs / 128 B）
		v, ok := limiters.Load(key)
		if !ok {
			v, _ = limiters.LoadOrStore(key, &rateLimiterEntry{
				limiter: rate.NewLimiter(rate.Limit(config.Rate), config.Burst),
			})
		}
		entry := v.(*rateLimiterEntry)
		entry.lastSeen.Store(now)
		if last := lastSweep.Load(); now-last > int64(rateLimiterSweepEvery) && lastSweep.CompareAndSwap(last, now) {
			go sweep(now)
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

		if !limiter.Allow() {
			c.Header("Retry-After", "1")
			c.Header("X-RateLimit-Limit", limitHeader)
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))

			if config.ErrorMsg != "" {
				c.String(config.StatusCode, config.ErrorMsg)
			}
			// 一律 Abort：先前帶 ErrorMsg 時只寫了 429 沒中止，後面的 handler 照樣執行，
			// 回應變成「錯誤訊息 + handler 輸出」，限流形同虛設
			c.AbortWithStatus(config.StatusCode)
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
	ErrorCode    int // 預設 503（逾時是伺服器端處理太久；408 是指客戶端送請求太慢）
}

// Timeout 創建超時中間件。
//
// handler 在另一個 goroutine 執行，輸出寫進緩衝；逾時前完成才整份寫到真正的回應，
// 逾時則立即回 ErrorCode，handler 之後的寫入一律丟棄。逾時後仍會等 handler 結束才返回：
// router 在本中間件返回後就把 Context 歸還物件池，handler 若還在用它，會改到下一個請求的
// Context。先前逾時即返回，handler 與主 goroutine 同時寫 Context / ResponseWriter（data race），
// 並在 Context 被下一個請求取用後繼續寫入。
//
// 因此 handler 應觀察 c.Request.Context()（逾時即 Done）盡快結束；串流回應會被緩衝到結束才送出
func Timeout(config TimeoutConfig) hypcontext.HandlerFunc {
	if config.ErrorCode == 0 {
		config.ErrorCode = http.StatusServiceUnavailable
	}

	return func(c *hypcontext.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), config.Timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		real := c.Writer
		tw := &timeoutWriter{ResponseWriter: real, h: real.Header().Clone(), code: http.StatusOK}
		c.Writer, c.Response = tw, tw

		done := make(chan struct{})
		var panicVal interface{}
		go func() {
			defer close(done)
			defer func() { panicVal = recover() }()
			c.Next()
		}()

		select {
		case <-done:
			c.Writer, c.Response = real, real
			if panicVal != nil {
				panic(panicVal) // 交給外層 Recovery
			}
			tw.copyTo(real)
		case <-ctx.Done():
			tw.mu.Lock()
			tw.timedOut = true
			tw.mu.Unlock()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) { // 客戶端斷線時不必回應
				real.WriteHeader(config.ErrorCode)
				if config.ErrorMessage != "" {
					real.WriteString(config.ErrorMessage)
				}
				real.WriteHeaderNow()
				real.Flush()
			}
			<-done // 等 handler 結束才歸還 Context（見函式說明）
			c.Writer, c.Response = real, real
			c.Abort()
			// 逾時後的寫入回傳 http.ErrHandlerTimeout，c.String / c.JSON 會因此 panic——預期中，吞掉；
			// 其餘 panic 仍交給外層 Recovery 記錄（回應已送出，狀態碼不會再變）
			if e, ok := panicVal.(error); panicVal != nil && !(ok && errors.Is(e, http.ErrHandlerTimeout)) {
				panic(panicVal)
			}
		}
	}
}

// timeoutWriter 緩衝 handler 的輸出；Header 為 handler 專用的副本，逾時後的寫入一律丟棄。
// Hijack / Push 等其餘方法透傳給真正的 writer
type timeoutWriter struct {
	hypcontext.ResponseWriter

	mu       sync.Mutex
	h        http.Header
	buf      bytes.Buffer
	code     int
	wrote    bool
	timedOut bool
}

func (w *timeoutWriter) Header() http.Header { return w.h }

func (w *timeoutWriter) WriteHeader(code int) {
	w.mu.Lock()
	if code > 0 && !w.wrote {
		w.code = code
	}
	w.mu.Unlock()
}

func (w *timeoutWriter) WriteHeaderNow() {
	w.mu.Lock()
	w.wrote = true
	w.mu.Unlock()
}

func (w *timeoutWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	w.wrote = true
	return w.buf.Write(p)
}

func (w *timeoutWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func (w *timeoutWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.code
}

func (w *timeoutWriter) Size() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Len()
}

func (w *timeoutWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.wrote
}

// Flush 不透傳：輸出要等 handler 完成才決定送不送
func (w *timeoutWriter) Flush() {}

// copyTo 把 handler 完成後的標頭、狀態碼與 body 寫到真正的 writer（handler goroutine 已結束）
func (w *timeoutWriter) copyTo(real hypcontext.ResponseWriter) {
	dst := real.Header()
	for k, v := range w.h {
		dst[k] = v
	}
	real.WriteHeader(w.code)
	if w.buf.Len() > 0 {
		real.Write(w.buf.Bytes())
	} else if w.wrote {
		real.WriteHeaderNow()
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

// MethodOverride 已移至 router：中間件在路由比對之後才執行，改寫 c.Request.Method
// 無法讓 POST 對到 PUT / DELETE 路由。請改用 r.EnableMethodOverride()

// RejectEarlyData 對非安全方法的 0-RTT 早期資料回 425 Too Early（RFC 8470），
// 客戶端會在握手完成後重送。早期資料可被攻擊者重放，單次使用 session ticket
// 也擋不住「搶先重放 + 原請求改走 1-RTT 重送」造成的重複執行。
// 安全方法（GET / HEAD / OPTIONS / TRACE）不改變狀態，照常放行。
func RejectEarlyData() hypcontext.HandlerFunc {
	return func(c *hypcontext.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		default:
			if c.IsEarlyData() {
				c.AbortWithStatus(http.StatusTooEarly)
				return
			}
		}
		c.Next()
	}
}

// ===== 預設中間件配置 =====

// DefaultMiddleware 創建預設中間件組合（日誌與 panic 記錄寫到 os.Stdout）
func DefaultMiddleware() []hypcontext.HandlerFunc {
	return DefaultMiddlewareWithOutput(nil, nil)
}

// DefaultMiddlewareWithOutput 同 DefaultMiddleware，但請求日誌寫到 access、panic 記錄寫到 errs
// （nil 時為 os.Stdout）。server 以此把兩者接到自己的 logger，遵守日誌等級與 JSON 格式
func DefaultMiddlewareWithOutput(access, errs io.Writer) []hypcontext.HandlerFunc {
	return []hypcontext.HandlerFunc{
		// 錯誤恢復
		Recovery(RecoveryConfig{
			StackSize: 4 << 10,
			Output:    errs,
		}),
		// 日誌
		Logger(LoggerConfig{
			TimeFormat: time.RFC3339,
			Output:     access,
		}),
		// 0-RTT 重放保護（auto 模式同時服務 HTTP/3；經代理的早期資料在任何協議都可能出現）
		RejectEarlyData(),
		// 安全頭（X-XSS-Protection: 0——舊版瀏覽器的 XSS 過濾器本身可被利用，OWASP 建議關閉）
		Security(SecurityConfig{
			XSSProtection:      "0",
			ContentTypeNosniff: "nosniff",
			XFrameOptions:      "DENY",
		}),
		// CORS
		CORS(CORSConfig{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			// Authorization：內建 JWT 需要；先前未列入，瀏覽器帶 Bearer token 的跨來源請求 preflight 失敗
			AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With", "X-CSRF-Token"},
		}),
	}
}

// HTTP3Middleware 創建 HTTP/3 優化的中間件組合
func HTTP3Middleware() []hypcontext.HandlerFunc {
	return HTTP3MiddlewareWithOutput(nil, nil)
}

// HTTP3MiddlewareWithOutput 同 HTTP3Middleware，輸出規則同 DefaultMiddlewareWithOutput
func HTTP3MiddlewareWithOutput(access, errs io.Writer) []hypcontext.HandlerFunc {
	return []hypcontext.HandlerFunc{
		// 錯誤恢復
		Recovery(RecoveryConfig{
			StackSize: 4 << 10,
			Output:    errs,
		}),
		// 日誌
		Logger(LoggerConfig{
			TimeFormat:    time.RFC3339,
			EnableLatency: true,
			EnableSize:    true,
			Output:        access,
		}),
		// 0-RTT 重放保護
		RejectEarlyData(),
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
