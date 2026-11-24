package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// 自訂標頭放原始 token；Authorization 仍然必須帶方案名稱。
// 歷史 bug：所有標頭都要求 "Bearer " 前綴，TokenLookup: "header:X-Auth-Token" 的請求一律 401
func TestJWTCustomHeaderTakesRawToken(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	token, _ := SignHS256(JWTClaims{Subject: "1"}, secret, time.Hour)
	r := router.New()
	r.GET("/custom", JWT(JWTConfig{Validator: HS256Validator(secret), TokenLookup: "header:X-Auth-Token"}), func(c *hypcontext.Context) { c.Status(204) })
	r.GET("/auth", JWT(JWTConfig{Validator: HS256Validator(secret)}), func(c *hypcontext.Context) { c.Status(204) })

	for _, c := range []struct {
		path, header, value string
		want                int
	}{
		{"/custom", "X-Auth-Token", token, 204},
		{"/custom", "X-Auth-Token", "Bearer " + token, 204}, // 帶前綴的客戶端照常
		{"/custom", "X-Auth-Token", "", 401},
		{"/custom", "X-Auth-Token", "not-a-token", 401},
		{"/auth", "Authorization", token, 401}, // Authorization 沒有方案名稱：拒絕
		{"/auth", "Authorization", "Bearer " + token, 204},
	} {
		if w := get(r, "GET", c.path, c.header, c.value); w.Code != c.want {
			t.Errorf("%s %s: %q... = %d, want %d", c.path, c.header, c.value[:min(8, len(c.value))], w.Code, c.want)
		}
	}
}

// Compression / Timeout 包住 writer 之後 c.DisableWriteTimeout 仍然有效。
// 歷史 bug：兩個 writer 沒有 Unwrap，回 ErrNotSupported，SSE 與串流照樣被 write_timeout 切斷
func TestDisableWriteTimeoutThroughWrappedWriters(t *testing.T) {
	r := router.New()
	h := func(c *hypcontext.Context) {
		if err := c.DisableWriteTimeout(); err != nil {
			c.String(500, err.Error())
			return
		}
		c.String(200, "ok")
	}
	r.GET("/plain", h)
	r.GET("/gzip", Compression(CompressionConfig{}), h)
	r.GET("/timeout", Timeout(TimeoutConfig{Timeout: 5 * time.Second}), h)
	srv := httptest.NewServer(r)
	defer srv.Close()

	for _, path := range []string{"/plain", "/gzip", "/timeout"} {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s: DisableWriteTimeout failed (status %d)", path, resp.StatusCode)
		}
	}
}

// IsCORS 認得 CORS / NewCORS 建立的中間件（server 據此決定要不要掛預設的 CORS）
func TestIsCORS(t *testing.T) {
	own, err := NewCORS(CORSConfig{AllowOrigins: []string{"https://app.example.com"}, AllowCredentials: true})
	if err != nil {
		t.Fatal(err)
	}
	if !IsCORS(own) || !IsCORS(CORS(DefaultCORSConfig())) {
		t.Error("a CORS middleware is not recognised")
	}
	if IsCORS(Recovery(RecoveryConfig{})) || IsCORS(nil) || IsCORS(func(c *hypcontext.Context) {}) {
		t.Error("something else is taken for a CORS middleware")
	}
}
