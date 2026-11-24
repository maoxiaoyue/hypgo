// @chris
package middleware

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	hyperrors "github.com/maoxiaoyue/hypgo/pkg/errors"
	"golang.org/x/time/rate"
)

// abortError 以 Error Catalog 的 JSON 中止請求（與 router 的 404 / 405、c.BindInput 同一種格式）。
// 先前各中間件的預設錯誤回應都沒有 body。回應已開始寫出時只中止
func abortError(c *hypcontext.Context, status int, e *hyperrors.AppError) {
	c.Abort()
	if !c.Writer.Written() {
		c.JSON(status, e.JSON())
	}
}

// ===== 核心工具函數 =====

// secureRandHex 使用 crypto/rand 生成安全隨機 hex 字串
// （Go 1.24 起 rand.Read 保證不回傳錯誤，先前的時間戳備援走不到）
func secureRandHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// valueSource 解析好的取值來源（"header:X-Token"、"query:token"、"form:csrf"、"cookie:token"）。
// 建立中間件時解析一次：先前 CSRF / JWT / APIKey 各有一份解析函式，每個請求重新切字串，
// 格式寫錯時靜默取不到值、一律拒絕
type valueSource struct{ kind, name string }

// parseValueSource 解析 lookup；來源不在 allowed 內或格式錯誤時 panic（設定錯誤應在啟動時發現）
func parseValueSource(lookup string, allowed ...string) valueSource {
	kind, name, ok := strings.Cut(lookup, ":")
	if ok && name != "" {
		for _, a := range allowed {
			if kind == a {
				return valueSource{kind: kind, name: name}
			}
		}
	}
	panic(fmt.Sprintf("middleware: invalid lookup %q (want <%s>:<name>)", lookup, strings.Join(allowed, "|")))
}

func (s valueSource) get(c *hypcontext.Context) string {
	switch s.kind {
	case "header":
		return c.GetHeader(s.name)
	case "query":
		return c.Query(s.name)
	case "form":
		return c.PostForm(s.name)
	case "cookie":
		v, _ := c.Cookie(s.name)
		return v
	}
	return ""
}

// ===== 日誌中間件 =====

// LoggerConfig 日誌配置
type LoggerConfig struct {
	TimeFormat string
	SkipPaths  []string
	Output     io.Writer // nil 時寫到 os.Stdout
	// Log 非 nil 時改以結構化欄位記錄（訊息 "request"，欄位 method / path / status / ms /
	// bytes / ip / proto / ua），不寫 Output。server 以此接到自己的 logger：
	// 先前整行塞在 msg 裡，JSON 格式下狀態碼、耗時都不是獨立欄位
	Log func(msg string, keysAndValues ...interface{})
}

// logSafe 把控制字元跳脫成 \n、\x1b 這類寫法。路徑是解碼後的，/x%0A... 會在日誌裡變成
// 另起一行、看起來像另一筆紀錄
func logSafe(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			q := strconv.Quote(s)
			return q[1 : len(q)-1]
		}
	}
	return s
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
		k, _, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		// 鍵先解碼再比對：c.Query("token") 讀得到 %74oken=，先前這種寫法不會被遮蔽
		name, err := url.QueryUnescape(k)
		if err != nil {
			name = k
		}
		if sensitiveQueryKeys[strings.ToLower(name)] {
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
			if config.Log != nil {
				config.Log("request",
					"method", c.Request.Method,
					"path", path,
					"status", status,
					"ms", float64(time.Since(start).Microseconds())/1000,
					"bytes", c.Response.Size(),
					"ip", c.ClientIP(),
					"proto", c.Request.Proto,
					"ua", c.GetHeader("User-Agent"),
				)
			} else {
				fmt.Fprintf(out, "[%s] %s | %s | %d | %v | %d bytes | %s %s | %s\n",
					c.Request.Proto,
					time.Now().Format(config.TimeFormat),
					c.ClientIP(),
					status,
					time.Since(start),
					c.Response.Size(),
					logSafe(c.Request.Method),
					logSafe(path),
					logSafe(c.GetHeader("User-Agent")),
				)
			}
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
	Rate       int     // 每秒請求數（必填，須大於 0）
	Burst      int     // 突發請求數；0 時等於 Rate
	KeyFunc    KeyFunc // 獲取限制鍵的函數；nil 時為客戶端 IP（IPv6 以 /64 為單位）
	ErrorMsg   string  // 設定時以純文字回應；空時回 Error Catalog 的 JSON（E0007）
	StatusCode int     // 狀態碼，預設 429
}

// rateLimitKey 預設的限流鍵：IPv4 用位址本身，IPv6 用 /64。
// 一個 IPv6 用戶通常拿到整個 /64，先前以完整位址為鍵，換位址就能繞過限流
func rateLimitKey(ip string) string {
	if a, err := netip.ParseAddr(ip); err == nil && a.Is6() && !a.Is4In6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return ip
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

// RateLimiter 創建速率限制中間件。Rate 必須大於 0（否則 panic）；Burst 沒給時等於 Rate。
// 先前 RateLimiterConfig{Rate: 100} 的 Burst 是 0，第一個請求起全部 429
func RateLimiter(config RateLimiterConfig) hypcontext.HandlerFunc {
	if config.Rate <= 0 {
		panic(fmt.Sprintf("middleware: RateLimiterConfig.Rate must be greater than 0 (got %d)", config.Rate))
	}
	if config.Burst <= 0 {
		config.Burst = config.Rate
	}
	limiters := &sync.Map{}

	if config.KeyFunc == nil {
		config.KeyFunc = func(c *hypcontext.Context) string {
			return rateLimitKey(c.ClientIP())
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
		if !entry.limiter.Allow() {
			c.Header("Retry-After", "1")
			c.Header("X-RateLimit-Limit", limitHeader)
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))

			// 一律 Abort：先前帶 ErrorMsg 時只寫了 429 沒中止，後面的 handler 照樣執行，
			// 回應變成「錯誤訊息 + handler 輸出」，限流形同虛設
			if config.ErrorMsg != "" {
				c.Abort()
				c.String(config.StatusCode, config.ErrorMsg)
			} else {
				abortError(c, config.StatusCode, hyperrors.ErrTooManyRequests)
			}
			return
		}

		c.Next()
	}
}

// ===== 超時中間件 =====

// TimeoutConfig 超時配置
type TimeoutConfig struct {
	Timeout      time.Duration // 必填，須大於 0
	ErrorMessage string        // 設定時以純文字回應；空時回 Error Catalog 的 JSON（E0008）
	ErrorCode    int           // 預設 503（逾時是伺服器端處理太久；408 是指客戶端送請求太慢）
}

// PanicError 是 Timeout 把 handler 的 panic 轉交給外層時用的包裝：handler 在另一個 goroutine
// 執行，panic 在主 goroutine 重拋後堆疊裡已沒有出事的函式，所以在原處先擷取。
// Recovery 會解開它、印出 Stack
type PanicError struct {
	Value interface{} // 原本的 panic 值
	Stack []byte      // panic 當下 handler goroutine 的堆疊
}

func (p *PanicError) Error() string { return fmt.Sprintf("%v\n%s", p.Value, p.Stack) }

// Unwrap 回傳原本的 panic 值（它是 error 時）
func (p *PanicError) Unwrap() error {
	err, _ := p.Value.(error)
	return err
}

// repanic 把 handler goroutine 的 panic 交給外層；http.ErrAbortHandler 維持原值（net/http 以它辨認）
func repanic(v interface{}, stack []byte) {
	if v == http.ErrAbortHandler {
		panic(v)
	}
	panic(&PanicError{Value: v, Stack: stack})
}

// Timeout 創建超時中間件。
//
// handler 在另一個 goroutine 執行，輸出寫進緩衝；逾時前完成才整份寫到真正的回應，
// 逾時則立即回 ErrorCode，handler 之後的寫入一律丟棄。逾時後仍會等 handler 結束才返回：
// router 在本中間件返回後就把 Context 歸還物件池，handler 若還在用它，會改到下一個請求的
// Context。先前逾時即返回，handler 與主 goroutine 同時寫 Context / ResponseWriter（data race），
// 並在 Context 被下一個請求取用後繼續寫入。
//
// 因此 handler 應觀察 c.Request.Context()（逾時即 Done）盡快結束；串流回應會被緩衝到結束才送出。
//
// Timeout 必須大於 0（否則 panic）：先前 TimeoutConfig{} 讓每個請求立刻逾時、全部 503
func Timeout(config TimeoutConfig) hypcontext.HandlerFunc {
	if config.Timeout <= 0 {
		panic(fmt.Sprintf("middleware: TimeoutConfig.Timeout must be greater than 0 (got %v)", config.Timeout))
	}
	if config.ErrorCode == 0 {
		config.ErrorCode = http.StatusServiceUnavailable
	}
	timeoutJSON, _ := json.Marshal(hyperrors.ErrTimeout.JSON())

	return func(c *hypcontext.Context) {
		orig := c.Request
		ctx, cancel := context.WithTimeout(orig.Context(), config.Timeout)
		// 返回時換回原本的請求：外層（Recovery）以請求的 context 判斷客戶端是否已斷線，
		// 留著這個已被 cancel 的 context 會讓每個 panic 都被當成斷線
		defer func() { c.Request = orig }()
		defer cancel()
		c.Request = orig.WithContext(ctx)

		real := c.Writer
		tw := &timeoutWriter{ResponseWriter: real, h: real.Header().Clone(), code: http.StatusOK}
		c.Writer, c.Response = tw, tw

		done := make(chan struct{})
		var panicVal interface{}
		var panicStack []byte
		go func() {
			defer close(done)
			defer func() {
				if panicVal = recover(); panicVal != nil {
					panicStack = debug.Stack() // 在這裡才看得到出事的函式（見 PanicError）
				}
			}()
			c.Next()
		}()

		select {
		case <-done:
			c.Writer, c.Response = real, real
			if panicVal != nil {
				repanic(panicVal, panicStack) // 交給外層 Recovery
			}
			tw.copyTo(real)
		case <-ctx.Done():
			tw.mu.Lock()
			tw.timedOut = true
			tw.mu.Unlock()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) { // 客戶端斷線時不必回應
				// 直接寫 real：handler 可能還在用 Context，不能經過 c.JSON
				if config.ErrorMessage != "" {
					real.WriteHeader(config.ErrorCode)
					real.WriteString(config.ErrorMessage)
				} else {
					real.Header().Set("Content-Type", "application/json; charset=utf-8")
					real.WriteHeader(config.ErrorCode)
					real.Write(timeoutJSON)
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
				repanic(panicVal, panicStack)
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

// Unwrap 讓 http.ResponseController 找得到底層的 writer（c.DisableWriteTimeout 需要）
func (w *timeoutWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

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
	ErrorMsg string // 設定時以純文字回應；空時回 Error Catalog 的 JSON（E0006，與 c.BindInput 相同）
}

// BodyLimit 限制請求 body 大小，防止大 payload 攻擊
func BodyLimit(config BodyLimitConfig) hypcontext.HandlerFunc {
	if config.MaxBytes <= 0 {
		config.MaxBytes = 1 << 20 // 1MB
	}

	return func(c *hypcontext.Context) {
		if c.Request.ContentLength > config.MaxBytes {
			if config.ErrorMsg != "" {
				c.Abort()
				c.String(http.StatusRequestEntityTooLarge, config.ErrorMsg)
			} else {
				abortError(c, http.StatusRequestEntityTooLarge, hyperrors.ErrBodyTooLarge)
			}
			return
		}
		// 調整（或建立）這個請求的 body 上限：server 已依 server.max_body_size 套用一層，
		// 這裡的值取代它——掛在上傳路由上可以放寬，掛在全域可以收緊
		c.SetBodyLimit(config.MaxBytes)
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
// （nil 時為 os.Stdout）
func DefaultMiddlewareWithOutput(access, errs io.Writer) []hypcontext.HandlerFunc {
	return DefaultMiddlewareWith(LoggerConfig{Output: access}, RecoveryConfig{Output: errs}, CORS(DefaultCORSConfig()))
}

// DefaultCORSConfig 預設組合用的 CORS 設定：任何來源、不帶憑證
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		// Authorization：內建 JWT 需要；先前未列入，瀏覽器帶 Bearer token 的跨來源請求 preflight 失敗
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With", "X-CSRF-Token"},
	}
}

// DefaultMiddlewareWith 以指定的日誌、Recovery 設定與 CORS 中間件組出預設組合；cors 為 nil 時不含 CORS。
// server 以此接上自己的 logger 與 server.cors 設定
func DefaultMiddlewareWith(log LoggerConfig, rec RecoveryConfig, cors hypcontext.HandlerFunc) []hypcontext.HandlerFunc {
	mws := []hypcontext.HandlerFunc{
		// 錯誤恢復
		Recovery(rec),
		// 日誌
		Logger(log),
		// 0-RTT 重放保護（auto 模式同時服務 HTTP/3；經代理的早期資料在任何協議都可能出現）
		RejectEarlyData(),
		// 安全頭（X-XSS-Protection: 0——舊版瀏覽器的 XSS 過濾器本身可被利用，OWASP 建議關閉）
		Security(SecurityConfig{
			XSSProtection:      "0",
			ContentTypeNosniff: "nosniff",
			XFrameOptions:      "DENY",
		}),
	}
	if cors != nil {
		mws = append(mws, cors)
	}
	return mws
}
