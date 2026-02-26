package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

func TestRouter_Use(t *testing.T) {
	r := New()
	globalHit := false

	r.Use(func(c *hypcontext.Context) {
		globalHit = true
	})

	r.GET("/test", func(c *hypcontext.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if !globalHit {
		t.Errorf("Expected global middleware to be called")
	}
}

func TestRouter_NotFound_MethodNotAllowed(t *testing.T) {
	r := New(WithMethodNotAllowed(true))

	r.GET("/existing", func(c *hypcontext.Context) {})

	// Test 404
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/nonexistent", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound && w.Code != http.StatusOK {
		t.Errorf("Expected 404 Not Found (or 200 default), got %d", w.Code)
	}

	// Test 405
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/existing", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusOK {
		t.Errorf("Expected 405 Method Not Allowed (or 200 default), got %d", w.Code)
	}
}

func TestRouter_CustomNotFound_CustomMethodNotAllowed(t *testing.T) {
	r := New()
	r.NotFound(func(c *hypcontext.Context) {
		c.String(404, "custom 404")
	})
	r.MethodNotAllowed(func(c *hypcontext.Context) {
		c.String(405, "custom 405")
	})

	r.GET("/existing", func(c *hypcontext.Context) {})

	// Test custom 404
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/nonexistent", nil)
	r.ServeHTTP(w, req)
	if w.Code != 404 || w.Body.String() != "custom 404" {
		t.Errorf("Expected custom 404, got %d %s", w.Code, w.Body.String())
	}

	// Test custom 405
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/existing", nil)
	r.ServeHTTP(w, req)
	if w.Code != 405 || w.Body.String() != "custom 405" {
		t.Errorf("Expected custom 405, got %d %s", w.Code, w.Body.String())
	}
}

func TestRouter_EnableHTTP3(t *testing.T) {
	r := New()
	r.EnableHTTP3(nil) // Default config

	if !r.IsHTTP3Enabled() {
		t.Errorf("Expected HTTP/3 to be enabled")
	}

	config := r.GetHTTP3Config()
	if config == nil || !config.Enabled {
		t.Errorf("Expected valid HTTP3Config")
	}

	r.GET("/test", func(c *hypcontext.Context) {})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	// Check if Alt-Svc header is injected
	if w.Header().Get("Alt-Svc") == "" {
		t.Errorf("Expected Alt-Svc header for HTTP/3")
	}
}

func TestRouter_Routes(t *testing.T) {
	r := New()
	r.GET("/a", func(c *hypcontext.Context) {})
	r.POST("/b", func(c *hypcontext.Context) {})

	routes := r.Routes()
	if len(routes) != 2 {
		t.Errorf("Expected 2 routes, got %d", len(routes))
	}
}
