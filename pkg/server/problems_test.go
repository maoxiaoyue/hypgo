package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
)

// 使用者以 Use() 掛了 CORS 時，預設的 CORS 不掛。
// 歷史 bug：預設中間件排在使用者的之前，預設的 "*" 先回掉 preflight（沒有 Allow-Credentials，
// 帶憑證的請求在瀏覽器失敗），不允許的來源也拿到 Access-Control-Allow-Origin: *
func TestUserCORSReplacesDefault(t *testing.T) {
	cfg := regressCfg("127.0.0.1:0")
	log, buf := bufLogger()
	s := New(cfg, log)
	s.router.Use(middleware.CORS(middleware.CORSConfig{
		AllowOrigins:     []string{"https://app.example.com"},
		AllowMethods:     []string{"GET"},
		AllowCredentials: true,
	}))
	s.router.GET("/x", func(c *hypcontext.Context) { c.String(200, "secret") })
	if err := s.applyDefaultMiddlewares(); err != nil {
		t.Fatal(err)
	}
	do := func(method, origin string, extra ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/x", nil)
		req.Header.Set("Origin", origin)
		for i := 0; i+1 < len(extra); i += 2 {
			req.Header.Set(extra[i], extra[i+1])
		}
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		return w
	}

	h := do("OPTIONS", "https://app.example.com", "Access-Control-Request-Method", "GET").Header()
	if h.Get("Access-Control-Allow-Origin") != "https://app.example.com" || h.Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("preflight from the allowed origin: %v", h)
	}
	if h := do("GET", "https://evil.example").Header(); h.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("disallowed origin got Access-Control-Allow-Origin %q", h.Get("Access-Control-Allow-Origin"))
	}
	// 其餘的預設中間件照常
	if h := do("GET", "https://app.example.com").Header(); h.Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers missing: %v", h)
	}
	if strings.Contains(buf.String(), "server.cors is ignored") {
		t.Error("warned although server.cors is not set")
	}

	// 兩邊都設了：以 Use() 的為準並警告
	cfg = regressCfg("127.0.0.1:0")
	cfg.Server.CORS.AllowOrigins = []string{"https://other.example"}
	log, buf = bufLogger()
	s = New(cfg, log)
	s.router.Use(middleware.CORS(middleware.CORSConfig{AllowOrigins: []string{"https://app.example.com"}}))
	if err := s.applyDefaultMiddlewares(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "server.cors is ignored") {
		t.Errorf("no warning when both are set: %q", buf.String())
	}
}

// max_handlers 的 503 與其他錯誤同一個格式。歷史 bug：純文字 "server busy"
func TestRejectBusyIsCatalogJSON(t *testing.T) {
	log, _ := bufLogger()
	s := New(regressCfg("127.0.0.1:0"), log)
	w := httptest.NewRecorder()
	s.rejectBusy(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != 503 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Header().Get("Content-Type"), "application/json") ||
		!strings.Contains(w.Body.String(), `"code":"E0009"`) {
		t.Errorf("%d %v %q", w.Code, w.Header(), w.Body.String())
	}
}
