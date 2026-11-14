package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

func get(r *router.Router, method, path string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return serve(r, req)
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected a panic", name)
		}
	}()
	f()
}

// 自訂 ErrorHandler 只寫回應、沒有 Abort 時，受保護的 handler 不得執行。
// 歷史 bug：JWT / CSRF / IPWhitelist / RequireRole 都照樣往下執行，回應是「401 + handler 的輸出」
func TestErrorHandlerWithoutAbortStopsChain(t *testing.T) {
	deny := func(code int) func(c *hypcontext.Context) {
		return func(c *hypcontext.Context) { c.String(code, "denied") }
	}
	cases := map[string]struct {
		mw     hypcontext.HandlerFunc
		method string
	}{
		"jwt": {JWT(JWTConfig{
			Validator:    HS256Validator([]byte("k")),
			ErrorHandler: func(c *hypcontext.Context, _ error) { c.String(401, "denied") },
		}), "GET"},
		"csrf": {CSRF(CSRFConfig{ErrorHandler: deny(403)}), "POST"},
		"ip":   {IPWhitelist(IPWhitelistConfig{AllowedIPs: []string{"10.9.9.9"}, ErrorHandler: deny(403)}), "GET"},
		"role": {RequireRoleWith(RequireRoleConfig{
			Roles:        []string{"admin"},
			ErrorHandler: func(c *hypcontext.Context, _ error) { c.String(403, "denied") },
		}), "GET"},
	}
	for name, tc := range cases {
		ran := false
		r := router.New()
		r.Handle(tc.method, "/x", tc.mw, func(c *hypcontext.Context) { ran = true; c.String(200, "SECRET") })
		w := get(r, tc.method, "/x")
		if ran || w.Body.String() != "denied" {
			t.Errorf("%s: handler ran = %v, body = %q", name, ran, w.Body.String())
		}
	}
}

// 零值設定不得讓所有請求失敗。
// 歷史 bug：RateLimiterConfig{Rate: 100} 第一個請求就 429、TimeoutConfig{} 全部 503、RequireRole() 全部 403
func TestZeroValueConfigs(t *testing.T) {
	r := router.New()
	r.GET("/x", RateLimiter(RateLimiterConfig{Rate: 100}), func(c *hypcontext.Context) { c.Status(204) })
	for i := 0; i < 5; i++ {
		if w := get(r, "GET", "/x"); w.Code != 204 {
			t.Fatalf("request %d with Rate only = %d", i, w.Code)
		}
	}
	mustPanic(t, "Rate 0", func() { RateLimiter(RateLimiterConfig{}) })
	mustPanic(t, "Timeout 0", func() { Timeout(TimeoutConfig{}) })
	mustPanic(t, "no roles", func() { RequireRole() })
}

// CORS 設定在建立時檢查。
// 歷史 bug："*" 加 AllowCredentials 反射任何來源並帶 Allow-Credentials；
// "https://Example.com/" 永遠比對不到而沒有提示
func TestCORSConfigValidation(t *testing.T) {
	if _, err := NewCORS(CORSConfig{AllowOrigins: []string{"*"}, AllowCredentials: true}); err == nil {
		t.Error(`"*" with credentials accepted`)
	}
	for _, bad := range []string{"example.com", "https://a.example/path", "https://a.example?x=1", ""} {
		if _, err := NewCORS(CORSConfig{AllowOrigins: []string{bad}}); err == nil {
			t.Errorf("origin %q accepted", bad)
		}
	}
	mustPanic(t, "CORS", func() { CORS(CORSConfig{AllowOrigins: []string{"nope"}}) })

	r := router.New()
	r.Use(CORS(CORSConfig{AllowOrigins: []string{"https://Example.com/"}, AllowCredentials: true}))
	r.GET("/x", func(c *hypcontext.Context) {})
	if w := get(r, "GET", "/x", "Origin", "https://example.com"); w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("normalized origin not matched: %v", w.Header())
	}
	if w := get(r, "GET", "/x", "Origin", "https://evil.example"); w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("other origin allowed: %v", w.Header())
	}
}

// DefaultMiddlewareWith 的 cors 為 nil 時不含 CORS（server.cors.allow_origins: [] 走這裡）
func TestDefaultMiddlewareWithoutCORS(t *testing.T) {
	var buf bytes.Buffer
	r := router.New()
	r.Use(DefaultMiddlewareWith(LoggerConfig{Output: &buf}, RecoveryConfig{Output: &buf}, nil)...)
	r.GET("/x", func(c *hypcontext.Context) {})
	w := get(r, "GET", "/x", "Origin", "https://a.example")
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("headers = %v", w.Header())
	}
}

// 帶 Range 的請求與已壓縮的格式不壓縮；壓縮時 ETag 轉弱、拿掉 Accept-Ranges。
// 歷史 bug：206 回應的那一段被 gzip（Content-Range 講原始位元組、body 是壓縮後的），image/png 也照壓
func TestCompressionRangeAndTypes(t *testing.T) {
	big := strings.Repeat("hypgo ", 1000)
	file := func(c *hypcontext.Context) {
		c.Writer.Header().Set("ETag", `"v1"`)
		http.ServeContent(c.Writer, c.Request, "a.txt", time.Time{}, strings.NewReader(big))
	}
	r := router.New()
	r.Use(Compression(CompressionConfig{}))
	r.GET("/file", file)
	r.GET("/png", func(c *hypcontext.Context) { c.Data(200, "image/png", []byte(big)) })
	r.GET("/svg", func(c *hypcontext.Context) { c.Data(200, "image/svg+xml", []byte(big)) })

	w := get(r, "GET", "/file", "Accept-Encoding", "gzip", "Range", "bytes=6-11")
	if w.Code != 206 || w.Header().Get("Content-Encoding") != "" || w.Body.String() != "hypgo " {
		t.Errorf("range = %d enc=%q body=%q", w.Code, w.Header().Get("Content-Encoding"), w.Body.String())
	}
	w = get(r, "GET", "/file", "Accept-Encoding", "gzip")
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("ETag") != `W/"v1"` || w.Header().Get("Accept-Ranges") != "" {
		t.Errorf("full: enc=%q etag=%q accept-ranges=%q", w.Header().Get("Content-Encoding"), w.Header().Get("ETag"), w.Header().Get("Accept-Ranges"))
	}
	if w := get(r, "GET", "/png", "Accept-Encoding", "gzip"); w.Header().Get("Content-Encoding") != "" {
		t.Error("image/png compressed by default")
	}
	if w := get(r, "GET", "/svg", "Accept-Encoding", "gzip"); w.Header().Get("Content-Encoding") != "gzip" {
		t.Error("image/svg+xml not compressed")
	}

	all := router.New()
	all.Use(Compression(CompressionConfig{ExcludedTypes: []string{}}))
	all.GET("/png", func(c *hypcontext.Context) { c.Data(200, "image/png", []byte(big)) })
	if w := get(all, "GET", "/png", "Accept-Encoding", "gzip"); w.Header().Get("Content-Encoding") != "gzip" {
		t.Error("explicit empty ExcludedTypes should compress everything")
	}
}

// 客戶端斷線造成的寫入 panic 只記一行；程式錯誤照舊印堆疊、回 500 JSON。
// 歷史 bug：每次斷線都印一份 4KB 的堆疊
func TestRecoveryClientDisconnect(t *testing.T) {
	var buf bytes.Buffer
	called := false
	r := router.New()
	r.Use(Recovery(RecoveryConfig{Output: &buf}))
	r.GET("/write", func(c *hypcontext.Context) { panic(errors.New("write tcp: broken pipe")) })
	r.GET("/nil", func(c *hypcontext.Context) { var p *int; _ = *p })
	r.GET("/after", func(c *hypcontext.Context) { panic(errors.New("x")) }, func(c *hypcontext.Context) { called = true })

	gone, cancel := context.WithCancel(context.Background())
	cancel()
	serve(r, httptest.NewRequest("GET", "/write", nil).WithContext(gone))
	if out := buf.String(); !strings.Contains(out, "client disconnected") || strings.Contains(out, "goroutine") || strings.Count(out, "\n") != 1 {
		t.Errorf("disconnect log = %q", out)
	}

	buf.Reset()
	serve(r, httptest.NewRequest("GET", "/nil", nil).WithContext(gone)) // 執行期錯誤不算斷線
	if !strings.Contains(buf.String(), "goroutine") {
		t.Errorf("runtime error lost its stack: %q", buf.String())
	}

	buf.Reset()
	w := get(r, "GET", "/write") // 連線還在：一般的 panic
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"E0003"`) || !strings.Contains(buf.String(), "goroutine") {
		t.Errorf("panic = %d %q, log has stack = %v", w.Code, w.Body.String(), strings.Contains(buf.String(), "goroutine"))
	}
	if get(r, "GET", "/after"); called {
		t.Error("handler after the panic ran")
	}
}

// 同上，走真正的連線：客戶端送出請求後重置連線，c.JSON 寫入失敗而 panic
func TestRecoveryRealDisconnect(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	out := writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return buf.Write(p) })
	done := make(chan struct{})
	r := router.New()
	r.Use(func(c *hypcontext.Context) { defer close(done); c.Next() }, Recovery(RecoveryConfig{Output: out}))
	r.GET("/big", func(c *hypcontext.Context) {
		time.Sleep(150 * time.Millisecond)
		c.JSON(200, map[string]string{"data": strings.Repeat("x", 4<<20)})
	})
	srv := httptest.NewServer(r)
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(conn, "GET /big HTTP/1.1\r\nHost: x\r\n\r\n")
	time.Sleep(30 * time.Millisecond)
	conn.(*net.TCPConn).SetLinger(0) // 關閉時送 RST
	conn.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if got := buf.String(); !strings.Contains(got, "client disconnected") || strings.Contains(got, "goroutine") {
		t.Errorf("log = %q", got)
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func panicSite() { panic(errors.New("boom in handler")) }

// Timeout 內的 panic：Recovery 印出的堆疊要有出事的函式，而且不被當成客戶端斷線。
// 歷史 bug：panic 在主 goroutine 重拋，堆疊裡只剩 Timeout 自己
func TestTimeoutPanicKeepsStack(t *testing.T) {
	var buf bytes.Buffer
	r := router.New()
	r.Use(Recovery(RecoveryConfig{Output: &buf}), Timeout(TimeoutConfig{Timeout: time.Second}))
	r.GET("/x", func(c *hypcontext.Context) { panicSite() })
	w := get(r, "GET", "/x")
	if w.Code != 500 || !strings.Contains(buf.String(), "panicSite") || !strings.Contains(buf.String(), "boom in handler") {
		t.Errorf("status = %d, log = %q", w.Code, buf.String())
	}
}

// 預設的錯誤回應是 Error Catalog 的 JSON；設了自訂訊息時維持純文字。
// 歷史 bug：JWT / APIKey / CSRF / 限流 / 逾時 / Recovery 都沒有 body，BodyLimit 是純文字
func TestDefaultErrorBodies(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	expired, _ := SignHS256(JWTClaims{Subject: "1", ExpiresAt: time.Now().Add(-time.Hour).Unix()}, secret, 0)
	ok := func(c *hypcontext.Context) { c.String(200, "ok") }
	slow := func(c *hypcontext.Context) { <-c.Request.Context().Done() }

	r := router.New()
	r.GET("/jwt", JWT(JWTConfig{Validator: HS256Validator(secret)}), ok)
	r.GET("/key", APIKey(APIKeyConfig{Keys: map[string]bool{"k": true}}), ok)
	r.POST("/csrf", CSRF(CSRFConfig{}), ok)
	r.GET("/ip", IPWhitelist(IPWhitelistConfig{AllowedIPs: []string{"10.9.9.9"}}), ok)
	r.GET("/role", RequireRole("admin"), ok)
	r.GET("/basic", BasicAuth(AuthConfig{Authorized: map[string]string{"u": "p"}}), ok)
	r.GET("/rate", RateLimiter(RateLimiterConfig{Rate: 1}), ok)
	r.GET("/rate-text", RateLimiter(RateLimiterConfig{Rate: 1, ErrorMsg: "slow down"}), ok)
	r.GET("/timeout", Timeout(TimeoutConfig{Timeout: 10 * time.Millisecond}), slow)
	r.POST("/body", BodyLimit(BodyLimitConfig{MaxBytes: 4}), ok)
	r.POST("/body-text", BodyLimit(BodyLimitConfig{MaxBytes: 4, ErrorMsg: "too big"}), ok)

	check := func(name string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || !strings.Contains(w.Body.String(), `"code":"`+code+`"`) ||
			!strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s = %d %q (%s), want %d %s JSON", name, w.Code, w.Body.String(), w.Header().Get("Content-Type"), status, code)
		}
	}
	w := get(r, "GET", "/jwt")
	check("jwt", w, 401, "E2001")
	if w.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("jwt WWW-Authenticate = %q", w.Header().Get("WWW-Authenticate"))
	}
	check("jwt expired", get(r, "GET", "/jwt", "Authorization", "Bearer "+expired), 401, "E2003")
	check("apikey", get(r, "GET", "/key"), 401, "E2001")
	check("apikey wrong", get(r, "GET", "/key", "X-API-Key", "nope"), 401, "E2001")
	check("csrf", get(r, "POST", "/csrf"), 403, "E2002")
	check("ip", get(r, "GET", "/ip"), 403, "E2002")
	check("role", get(r, "GET", "/role"), 401, "E2001")
	check("basic", get(r, "GET", "/basic"), 401, "E2001")
	get(r, "GET", "/rate")
	check("rate", get(r, "GET", "/rate"), 429, "E0007")
	get(r, "GET", "/rate-text")
	if w := get(r, "GET", "/rate-text"); w.Code != 429 || w.Body.String() != "slow down" {
		t.Errorf("rate text = %d %q", w.Code, w.Body.String())
	}
	check("timeout", get(r, "GET", "/timeout"), 503, "E0008")
	check("body", serve(r, httptest.NewRequest("POST", "/body", strings.NewReader("0123456789"))), 413, "E0006")
	if w := serve(r, httptest.NewRequest("POST", "/body-text", strings.NewReader("0123456789"))); w.Code != 413 || w.Body.String() != "too big" {
		t.Errorf("body text = %d %q", w.Code, w.Body.String())
	}
}

// 存取日誌：控制字元跳脫、編碼過的敏感鍵也遮蔽、Log 以結構化欄位輸出。
// 歷史 bug：/x%0A... 在日誌另起一行；?%74oken= 不被遮蔽；整行塞在 msg 裡
func TestLoggerEscapesRedactsAndStructures(t *testing.T) {
	var buf bytes.Buffer
	r := router.New()
	r.Use(Logger(LoggerConfig{Output: &buf}))
	r.GET("/x/:id", func(c *hypcontext.Context) {})
	get(r, "GET", "/x/a%0A[HTTP%2F1.1]%20FAKE?%74oken=abc&q=1", "User-Agent", "ua\r\nX")
	out := buf.String()
	if strings.Count(out, "\n") != 1 || strings.Contains(out, "abc") || !strings.Contains(out, "%74oken=REDACTED&q=1") || !strings.Contains(out, `\n`) {
		t.Errorf("log = %q", out)
	}

	var msg string
	var kv []interface{}
	s := router.New()
	s.Use(Logger(LoggerConfig{Log: func(m string, a ...interface{}) { msg, kv = m, a }}))
	s.GET("/y", func(c *hypcontext.Context) { c.String(201, "hi") })
	get(s, "GET", "/y?a=1")
	fields := map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		fields[kv[i].(string)] = kv[i+1]
	}
	if msg != "request" || fields["method"] != "GET" || fields["path"] != "/y?a=1" || fields["status"] != 201 || fields["bytes"] != 2 {
		t.Errorf("msg = %q, fields = %v", msg, fields)
	}
	for _, k := range []string{"ms", "ip", "proto", "ua"} {
		if _, ok := fields[k]; !ok {
			t.Errorf("field %q missing", k)
		}
	}
}

// 預設的限流鍵把 IPv6 歸到 /64。歷史 bug：同一個 /64 換位址就能繞過
func TestRateLimiterGroupsIPv6(t *testing.T) {
	r := router.New()
	r.GET("/x", RateLimiter(RateLimiterConfig{Rate: 1, Burst: 1}), func(c *hypcontext.Context) { c.Status(204) })
	from := func(addr string) int {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = addr
		return serve(r, req).Code
	}
	passed := 0
	for i := 1; i <= 5; i++ {
		if from(fmt.Sprintf("[2001:db8:1:2::%d]:1000", i)) == 204 {
			passed++
		}
	}
	if passed != 1 {
		t.Errorf("%d of 5 addresses in one /64 passed, want 1", passed)
	}
	if from("[2001:db8:1:3::1]:1000") != 204 || from("192.0.2.7:1000") != 204 || from("192.0.2.8:1000") != 204 {
		t.Error("another /64 or separate IPv4 addresses share a bucket")
	}
}

// JWT 的寬鬆之處。歷史 bug：負的 ttl 簽出不過期的 token；簽章尾端位元改掉、後面多一個換行、
// 不帶 Bearer 都能通過
func TestJWTStrictness(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	if _, err := SignHS256(JWTClaims{Subject: "1"}, secret, -time.Hour); !errors.Is(err, ErrJWTTTL) {
		t.Errorf("negative ttl: err = %v", err)
	}
	token, _ := SignHS256(JWTClaims{Subject: "1"}, secret, time.Hour)
	if _, err := VerifyHS256(token, secret); err != nil {
		t.Fatal(err)
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, token[len(token)-1])
	variant := token[:len(token)-1] + string(alphabet[last|1]) // 只動未使用的位元
	if last&1 == 1 {
		t.Fatal("canonical signature should end with unused bits = 0")
	}
	if _, err := VerifyHS256(variant, secret); err == nil {
		t.Error("non-canonical signature accepted")
	}
	if _, err := VerifyHS256(token+"\n", secret); err == nil {
		t.Error("token with trailing newline accepted")
	}

	r := router.New()
	r.GET("/x", JWT(JWTConfig{Validator: HS256Validator(secret)}), func(c *hypcontext.Context) { c.Status(204) })
	for header, want := range map[string]int{
		token:              401, // 沒有 Bearer
		"Basic " + token:   401,
		"Bearer " + token:  204,
		"bearer  " + token: 204, // 方案名稱不分大小寫，多個空白可接受
	} {
		if w := get(r, "GET", "/x", "Authorization", header); w.Code != want {
			t.Errorf("Authorization %q = %d, want %d", header[:8], w.Code, want)
		}
	}
}

// CSRF cookie 預設 SameSite=Lax，可設定。歷史 bug：沒有 SameSite 屬性
func TestCSRFCookieSameSite(t *testing.T) {
	for want, cfg := range map[string]CSRFConfig{
		"SameSite=Lax":  {},
		"SameSite=None": {CookieSameSite: http.SameSiteNoneMode, CookieSecure: true},
	} {
		r := router.New()
		r.GET("/x", CSRF(cfg), func(c *hypcontext.Context) {})
		if got := get(r, "GET", "/x").Header().Get("Set-Cookie"); !strings.Contains(got, want) {
			t.Errorf("Set-Cookie = %q, want %s", got, want)
		}
	}
}

// Security 的欄位寫 "-" 代表不送該標頭。歷史 bug：留空一定套預設值，X-Frame-Options 拿不掉
func TestSecurityHeaderCanBeDisabled(t *testing.T) {
	r := router.New()
	r.Use(Security(SecurityConfig{XFrameOptions: "-"}))
	r.GET("/x", func(c *hypcontext.Context) {})
	h := get(r, "GET", "/x").Header()
	if _, ok := h["X-Frame-Options"]; ok || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", h)
	}
}
