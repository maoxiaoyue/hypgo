package middleware

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

func serve(r *router.Router, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestTimeoutIsolatesSlowHandler 迴歸測試（A1）：逾時立即回 503，handler 晚到的寫入被丟棄，
// 且中間件等 handler 結束才返回（Context 不會在 handler 還在用時被歸還物件池）。
// 歷史 bug：handler 與主 goroutine 同時寫 Context / ResponseWriter（-race 偵測到 4 個 data race），
// 中間件返回後 Context 被下一個請求取用，handler 仍繼續寫入。以 go test -race 執行最有效
func TestTimeoutIsolatesSlowHandler(t *testing.T) {
	var finished atomic.Bool
	r := router.New()
	r.Use(Timeout(TimeoutConfig{Timeout: 20 * time.Millisecond, ErrorMessage: "too slow"}))
	r.GET("/slow", func(c *hypcontext.Context) {
		defer finished.Store(true)
		<-c.Request.Context().Done()
		time.Sleep(10 * time.Millisecond)
		c.Set("k", 1)
		c.String(200, "late") // 逾時後的寫入被丟棄（Render 因 ErrHandlerTimeout panic，由中間件吞掉）
	})
	r.GET("/fast", func(c *hypcontext.Context) {
		c.Header("X-From-Handler", "1")
		c.String(201, "fast")
	})

	w := serve(r, httptest.NewRequest("GET", "/slow", nil))
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != "too slow" {
		t.Errorf("slow = %d %q, want 503 \"too slow\"", w.Code, w.Body.String())
	}
	if !finished.Load() {
		t.Error("middleware returned before the handler finished (Context would be released while in use)")
	}

	w = serve(r, httptest.NewRequest("GET", "/fast", nil))
	if w.Code != 201 || w.Body.String() != "fast" || w.Header().Get("X-From-Handler") != "1" {
		t.Errorf("fast = %d %q headers=%v, want 201 fast with handler header", w.Code, w.Body.String(), w.Header())
	}
}

// TestRateLimiterErrorMsgStopsChain 迴歸測試（A2）：設了 ErrorMsg 時被限流的請求不得執行 handler。
// 歷史 bug：只寫了 429 沒 Abort，回應變成 "slow downhandler"，handler 照樣執行
func TestRateLimiterErrorMsgStopsChain(t *testing.T) {
	var ran atomic.Int32
	r := router.New()
	r.Use(RateLimiter(RateLimiterConfig{Rate: 1, Burst: 1, ErrorMsg: "slow down"}))
	r.GET("/x", func(c *hypcontext.Context) { ran.Add(1); c.String(200, "handler") })

	serve(r, httptest.NewRequest("GET", "/x", nil))
	w := serve(r, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusTooManyRequests || w.Body.String() != "slow down" || ran.Load() != 1 {
		t.Errorf("limited = %d %q, handler ran %d times; want 429 \"slow down\", 1 run", w.Code, w.Body.String(), ran.Load())
	}
}

// TestRateLimiterConcurrentUse 迴歸測試（A4）：並行請求不得有 data race（以 go test -race 執行）。
// 歷史 bug：lastSeen 為 time.Time 欄位，多個請求同時寫入、清理 goroutine 同時讀取
func TestRateLimiterConcurrentUse(t *testing.T) {
	r := router.New()
	r.Use(RateLimiter(RateLimiterConfig{Rate: 1e6, Burst: 1e6}))
	r.GET("/x", func(c *hypcontext.Context) {})
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				serve(r, httptest.NewRequest("GET", "/x", nil))
			}
		}()
	}
	wg.Wait()
}

// BenchmarkRateLimiterHit 已存在的 key 不再配置新的 limiter（先前 3 allocs / 128 B 每請求）
func BenchmarkRateLimiterHit(b *testing.B) {
	h := RateLimiter(RateLimiterConfig{Rate: 1e9, Burst: 1e9})
	c := hypcontext.New(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		h(c)
	}
}

// TestLoggerRecordsPanicsProtocolAndRedactsSecrets 迴歸測試（A5）。
// 歷史 bug：Recovery 在外時 panic 的請求沒有日誌；協議恆為 HTTP/1.1（讀了從沒人設的 key）；
// ?token= 等憑證原樣寫進 log
func TestLoggerRecordsPanicsProtocolAndRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	r := router.New()
	r.Use(Recovery(RecoveryConfig{DisablePrintStack: true}), Logger(LoggerConfig{Output: &buf}))
	r.GET("/boom", func(c *hypcontext.Context) { panic("x") })
	r.GET("/ok", func(c *hypcontext.Context) {})

	w := serve(r, httptest.NewRequest("GET", "/boom", nil))
	if w.Code != 500 || !strings.Contains(buf.String(), "| 500 |") {
		t.Errorf("panic request: code=%d log=%q, want 500 logged", w.Code, buf.String())
	}

	buf.Reset()
	req := httptest.NewRequest("GET", "/ok?page=2&token=SECRET&API_KEY=K", nil)
	req.Proto, req.ProtoMajor = "HTTP/2.0", 2
	serve(r, req)
	line := buf.String()
	if !strings.HasPrefix(line, "[HTTP/2.0]") {
		t.Errorf("protocol not taken from the request: %q", line)
	}
	if strings.Contains(line, "SECRET") || strings.Contains(line, "=K ") || !strings.Contains(line, "page=2&token=REDACTED&API_KEY=REDACTED") {
		t.Errorf("query not redacted: %q", line)
	}
}

// TestCORSBehavior 迴歸測試（A6）。
// 歷史 bug：沒帶 Origin 的 OPTIONS 也被吞掉；反射 Origin 卻沒有 Vary；"*" 時回傳請求的 Origin；
// 預設 AllowHeaders 不含 Authorization（內建 JWT 的跨來源請求 preflight 失敗）
func TestCORSBehavior(t *testing.T) {
	r := router.New()
	r.Use(DefaultMiddlewareWithOutput(io.Discard, io.Discard)...)
	r.OPTIONS("/opt", func(c *hypcontext.Context) { c.String(200, "mine") })
	r.GET("/c", func(c *hypcontext.Context) {})

	if w := serve(r, httptest.NewRequest("OPTIONS", "/opt", nil)); w.Code != 200 || w.Body.String() != "mine" {
		t.Errorf("plain OPTIONS = %d %q, want the route's own response", w.Code, w.Body.String())
	}

	req := httptest.NewRequest("GET", "/c", nil)
	req.Header.Set("Origin", "https://a.example")
	w := serve(r, req)
	if w.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Origin") {
		t.Errorf("ACAO=%q Vary=%v, want literal * and Vary: Origin", w.Header().Get("Access-Control-Allow-Origin"), w.Header().Values("Vary"))
	}

	pf := httptest.NewRequest("OPTIONS", "/c", nil)
	pf.Header.Set("Origin", "https://a.example")
	pf.Header.Set("Access-Control-Request-Method", "PATCH")
	pf.Header.Set("Access-Control-Request-Headers", "Authorization")
	w = serve(r, pf)
	if w.Code != 204 || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Errorf("preflight = %d ACAH=%q ACAM=%q", w.Code, w.Header().Get("Access-Control-Allow-Headers"), w.Header().Get("Access-Control-Allow-Methods"))
	}

	// 帶憑證時不得回 "*"
	r2 := router.New()
	r2.Use(CORS(CORSConfig{AllowOrigins: []string{"*"}, AllowCredentials: true}))
	r2.GET("/c", func(c *hypcontext.Context) {})
	if w := serve(r2, req); w.Header().Get("Access-Control-Allow-Origin") != "https://a.example" {
		t.Errorf("with credentials ACAO=%q, want the request origin", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

// TestCompressionHonorsConfig 迴歸測試（A7）。
// 歷史 bug：MinLength 未生效（2 bytes 壓成 26 bytes）、204 也帶 Content-Encoding、
// "gzip;q=0" 被當成接受、不支援 Flush
func TestCompressionHonorsConfig(t *testing.T) {
	big := strings.Repeat("hypgo ", 400)
	r := router.New()
	r.Use(Compression(CompressionConfig{MinLength: 1024, ExcludedTypes: []string{"image/"}}))
	r.GET("/tiny", func(c *hypcontext.Context) { c.String(200, "ok") })
	r.GET("/big", func(c *hypcontext.Context) { c.String(200, big) })
	r.GET("/img", func(c *hypcontext.Context) { c.Data(200, "image/png", []byte(big)) })
	r.GET("/none", func(c *hypcontext.Context) { c.Status(204) })
	r.GET("/stream", func(c *hypcontext.Context) {
		c.Writer.Write([]byte("chunk"))
		c.Writer.Flush()
	})

	get := func(path, accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept-Encoding", accept)
		return serve(r, req)
	}

	if w := get("/tiny", "gzip"); w.Header().Get("Content-Encoding") != "" || w.Body.String() != "ok" {
		t.Errorf("tiny: CE=%q body=%q, want uncompressed", w.Header().Get("Content-Encoding"), w.Body.String())
	}
	w := get("/big", "gzip")
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("big not compressed")
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != big {
		t.Error("big body did not round-trip")
	}
	if w := get("/img", "gzip"); w.Header().Get("Content-Encoding") != "" {
		t.Error("excluded content type was compressed")
	}
	if w := get("/none", "gzip"); w.Code != 204 || w.Header().Get("Content-Encoding") != "" {
		t.Errorf("204: CE=%q, want none", w.Header().Get("Content-Encoding"))
	}
	if w := get("/big", "gzip;q=0, identity"); w.Header().Get("Content-Encoding") != "" {
		t.Error("gzip;q=0 was treated as accepted")
	}
	w = get("/stream", "gzip")
	if !w.Flushed || w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("stream: flushed=%v CE=%q, want flushed gzip", w.Flushed, w.Header().Get("Content-Encoding"))
	}
}

// TestCSRFTokenStableAcrossRequests 迴歸測試（A8）：cookie 存在期間 token 不變。
// 歷史 bug：每個 GET 都換新 token，多分頁時先前頁面表單的 token 失效
func TestCSRFTokenStableAcrossRequests(t *testing.T) {
	r := router.New()
	r.Use(CSRF(CSRFConfig{}))
	r.GET("/f", func(c *hypcontext.Context) {})
	r.POST("/f", func(c *hypcontext.Context) { c.Status(204) })

	first := serve(r, httptest.NewRequest("GET", "/f", nil)).Result().Cookies()[0]

	again := httptest.NewRequest("GET", "/f", nil)
	again.AddCookie(first)
	if w := serve(r, again); w.Header().Get("Set-Cookie") != "" {
		t.Error("second GET rotated the token")
	}

	post := httptest.NewRequest("POST", "/f", nil)
	post.AddCookie(first)
	post.Header.Set("X-CSRF-Token", first.Value)
	if w := serve(r, post); w.Code != 204 || w.Header().Get("Set-Cookie") != "" {
		t.Errorf("POST = %d Set-Cookie=%q, want 204 without rotation", w.Code, w.Header().Get("Set-Cookie"))
	}
}

// TestIPWhitelist 迴歸測試（A9）：TrustProxy 生效、CIDR 於建立時解析、格式錯誤即 panic。
func TestIPWhitelist(t *testing.T) {
	if err := hypcontext.SetTrustedProxies("192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	defer hypcontext.SetTrustedProxies()

	for _, tc := range []struct {
		trustProxy bool
		want       int
	}{{false, 403}, {true, 204}} {
		r := router.New()
		r.Use(IPWhitelist(IPWhitelistConfig{AllowedCIDRs: []string{"10.0.0.0/8"}, TrustProxy: tc.trustProxy}))
		r.GET("/x", func(c *hypcontext.Context) { c.Status(204) })
		req := httptest.NewRequest("GET", "/x", nil) // RemoteAddr 192.0.2.1（可信代理）
		req.Header.Set("X-Forwarded-For", "10.1.2.3")
		if w := serve(r, req); w.Code != tc.want {
			t.Errorf("TrustProxy=%v: %d, want %d", tc.trustProxy, w.Code, tc.want)
		}
	}

	defer func() {
		if recover() == nil {
			t.Error("invalid CIDR did not panic at construction")
		}
	}()
	IPWhitelist(IPWhitelistConfig{AllowedCIDRs: []string{"10.0.0.0/99"}})
}

// TestRecoveryAbortAndErrAbortHandler 迴歸測試（A10）。
// 歷史 bug：自訂 ErrorHandler 未 Abort 時，panic 之後的 handler 繼續執行；http.ErrAbortHandler 被吞掉
func TestRecoveryAbortAndErrAbortHandler(t *testing.T) {
	var after atomic.Bool
	r := router.New()
	r.Use(Recovery(RecoveryConfig{Output: io.Discard, ErrorHandler: func(c *hypcontext.Context, err interface{}) {
		c.String(500, "custom")
	}}))
	r.GET("/p", func(c *hypcontext.Context) { panic("x") }, func(c *hypcontext.Context) { after.Store(true) })
	if w := serve(r, httptest.NewRequest("GET", "/p", nil)); w.Body.String() != "custom" || after.Load() {
		t.Errorf("body=%q, later handler ran=%v; want custom and no further handlers", w.Body.String(), after.Load())
	}

	r2 := router.New()
	r2.Use(Recovery(RecoveryConfig{Output: io.Discard}))
	r2.GET("/a", func(c *hypcontext.Context) { panic(http.ErrAbortHandler) })
	defer func() {
		if p := recover(); p != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler to propagate", p)
		}
	}()
	serve(r2, httptest.NewRequest("GET", "/a", nil))
}

// TestSmallSecurityFixes 迴歸測試（A11–A13）。
func TestSmallSecurityFixes(t *testing.T) {
	// A11：X-XSS-Protection 預設 0
	r := router.New()
	r.Use(Security(SecurityConfig{}))
	r.GET("/s", func(c *hypcontext.Context) {})
	if got := serve(r, httptest.NewRequest("GET", "/s", nil)).Header().Get("X-XSS-Protection"); got != "0" {
		t.Errorf("X-XSS-Protection = %q, want 0", got)
	}

	// A12：不合法的 X-Request-ID 被換掉
	r = router.New()
	r.Use(RequestID(RequestIDConfig{}))
	r.GET("/id", func(c *hypcontext.Context) {})
	req := httptest.NewRequest("GET", "/id", nil)
	req.Header.Set("X-Request-ID", "bad id\twith tab"+strings.Repeat("x", 200))
	if got := serve(r, req).Header().Get("X-Request-ID"); !validRequestID(got) || strings.Contains(got, "bad") {
		t.Errorf("invalid request id echoed back: %q", got)
	}
	req.Header.Set("X-Request-ID", "trace-123.abc")
	if got := serve(r, req).Header().Get("X-Request-ID"); got != "trace-123.abc" {
		t.Errorf("valid request id replaced: %q", got)
	}

	// A13：BasicAuth 帳密錯誤也帶 WWW-Authenticate
	r = router.New()
	r.Use(BasicAuth(AuthConfig{Authorized: map[string]string{"u": "p"}}))
	r.GET("/b", func(c *hypcontext.Context) {})
	req = httptest.NewRequest("GET", "/b", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("u:wrong")))
	if w := serve(r, req); w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("bad credentials = %d WWW-Authenticate=%q", w.Code, w.Header().Get("WWW-Authenticate"))
	}

	// A13：JWT 方案名稱不分大小寫
	secret := []byte("s")
	tok, _ := SignHS256(JWTClaims{Subject: "1"}, secret, time.Hour)
	r = router.New()
	r.Use(JWT(JWTConfig{Validator: HS256Validator(secret)}))
	r.GET("/j", func(c *hypcontext.Context) { c.Status(204) })
	req = httptest.NewRequest("GET", "/j", nil)
	req.Header.Set("Authorization", "bearer "+tok)
	if w := serve(r, req); w.Code != 204 {
		t.Errorf("lowercase bearer = %d, want 204", w.Code)
	}

	// A13：APIKey 常數時間比對仍正確
	r = router.New()
	r.Use(APIKey(APIKeyConfig{Keys: map[string]bool{"k1": true, "k2": false}}))
	r.GET("/k", func(c *hypcontext.Context) { c.Status(204) })
	for key, want := range map[string]int{"k1": 204, "k2": 401, "nope": 401} {
		req := httptest.NewRequest("GET", "/k", nil)
		req.Header.Set("X-API-Key", key)
		if w := serve(r, req); w.Code != want {
			t.Errorf("api key %q = %d, want %d", key, w.Code, want)
		}
	}

	// A13：JWT 未設 Validator 於建立時 panic
	defer func() {
		if recover() == nil {
			t.Error("JWT without Validator did not panic at construction")
		}
	}()
	JWT(JWTConfig{})
}
