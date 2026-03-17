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

	// Test 404 — Bug4 修復：預設 handler 必須回傳 404 而非 200
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/nonexistent", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found, got %d", w.Code)
	}

	// Test 405 — Bug4 修復：預設 handler 必須回傳 405 而非 200
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/existing", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 Method Not Allowed, got %d", w.Code)
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

// TestRouter_CatchAll Bug1 修復驗證：*filepath 路由必須正確觸發
func TestRouter_CatchAll(t *testing.T) {
	r := New()

	var captured string
	r.GET("/static/*filepath", func(c *hypcontext.Context) {
		captured = c.Param("filepath")
		c.String(200, "ok")
	})

	tests := []struct {
		path     string
		expected string
	}{
		{"/static/css/main.css", "/css/main.css"},
		{"/static/js/app.js", "/js/app.js"},
		{"/static/img/logo.png", "/img/logo.png"},
		{"/static/deep/nested/path/file.txt", "/deep/nested/path/file.txt"},
	}

	for _, tt := range tests {
		captured = ""
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", tt.path, nil)
		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("CatchAll %s: expected 200, got %d", tt.path, w.Code)
		}
		if captured != tt.expected {
			t.Errorf("CatchAll %s: expected param %q, got %q", tt.path, tt.expected, captured)
		}
	}
}

// TestRouter_CatchAllWithParam Bug1 修復驗證：:param 和 *filepath 混合路由
func TestRouter_CatchAllWithParam(t *testing.T) {
	r := New()

	var paramHit, catchAllHit bool
	r.GET("/users/:id", func(c *hypcontext.Context) {
		paramHit = true
		c.String(200, c.Param("id"))
	})
	r.GET("/files/*filepath", func(c *hypcontext.Context) {
		catchAllHit = true
		c.String(200, c.Param("filepath"))
	})

	// Test :param route
	paramHit, catchAllHit = false, false
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/users/42", nil)
	r.ServeHTTP(w, req)
	if !paramHit || catchAllHit {
		t.Error("Expected only param route to hit")
	}
	if w.Body.String() != "42" {
		t.Errorf("Expected param value '42', got %q", w.Body.String())
	}

	// Test *filepath route
	paramHit, catchAllHit = false, false
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/files/docs/readme.md", nil)
	r.ServeHTTP(w, req)
	if paramHit || !catchAllHit {
		t.Error("Expected only catchAll route to hit")
	}
	if w.Body.String() != "/docs/readme.md" {
		t.Errorf("Expected filepath '/docs/readme.md', got %q", w.Body.String())
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
