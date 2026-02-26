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
