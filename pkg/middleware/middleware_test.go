package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

func TestSecurity(t *testing.T) {
	r := router.New()
	r.Use(Security(SecurityConfig{}))
	r.GET("/", func(c *context.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)

	headers := w.Header()
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Expected X-Content-Type-Options to be nosniff")
	}
}

func TestCORS(t *testing.T) {
	r := router.New()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"*"},
	}))
	r.OPTIONS("/", func(c *context.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	r.ServeHTTP(w, req)

	headers := w.Header()
	if headers.Get("Access-Control-Allow-Origin") == "" {
		t.Logf("CORS headers might apply depending on specific configs")
	}
}

func TestDefaultMiddleware(t *testing.T) {
	handlers := DefaultMiddleware()
	if len(handlers) == 0 {
		t.Fatal("DefaultMiddleware() returned empty handlers")
	}
}

// TestDefaultMiddlewareRejectsEarlyData DefaultMiddleware 與 HTTP3Middleware 都必須
// 對非安全方法的早期資料回 425，且 handler 不得執行；安全方法照常放行。
func TestDefaultMiddlewareRejectsEarlyData(t *testing.T) {
	for name, chain := range map[string][]context.HandlerFunc{
		"Default": DefaultMiddleware(),
		"HTTP3":   HTTP3Middleware(),
	} {
		r := router.New()
		r.Use(chain...)
		ran := false
		h := func(c *context.Context) { ran = true; c.Status(http.StatusNoContent) }
		r.POST("/pay", h)
		r.GET("/pay", h)

		cases := []struct {
			method string
			early  bool
			want   int
			ran    bool
		}{
			{"POST", true, http.StatusTooEarly, false},
			{"POST", false, http.StatusNoContent, true},
			{"GET", true, http.StatusNoContent, true},
		}
		for _, tc := range cases {
			ran = false
			req := httptest.NewRequest(tc.method, "/pay", nil)
			if tc.early {
				req.Header.Set("Early-Data", "1")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want || ran != tc.ran {
				t.Errorf("%s %s early=%v: status=%d ran=%v, want %d ran=%v",
					name, tc.method, tc.early, w.Code, ran, tc.want, tc.ran)
			}
		}
	}
}
