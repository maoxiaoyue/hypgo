package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// TestNotFoundGoesThroughMiddleware 回歸測試：404 / 405 也必須經過全域中間件。
//
// 歷史 bug：r.notFound(c) / r.methodNotAllowed(c) 是直接呼叫，
// 完全繞過 executeHandlers —— Recovery / Logger / CORS / Security
// 對所有 404/405 回應都沒跑；自訂 404 handler 內的 panic 會直接
// 穿透到 net/http（連線被斷，而非 500）。
func TestNotFoundGoesThroughMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		wantStatus         int
	}{
		{"404", "GET", "/nope", http.StatusNotFound},
		{"405", "POST", "/only-get", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mwRan := false

			r := New()
			r.Use(func(c *hypcontext.Context) {
				mwRan = true
				c.Header("X-Global-MW", "1")
				c.Next()
			})
			r.GET("/only-get", func(c *hypcontext.Context) { c.String(200, "ok") })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))

			if !mwRan {
				t.Errorf("global middleware did not run for %s", tc.name)
			}
			if got := w.Header().Get("X-Global-MW"); got != "1" {
				t.Errorf("middleware header missing on %s response", tc.name)
			}
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
		})
	}
}

// TestNotFoundPanicIsRecovered 自訂 404 handler 內的 panic 必須能被
// Recovery 風格的中間件接住（證明 404 確實跑在中間件鏈內）。
func TestNotFoundPanicIsRecovered(t *testing.T) {
	r := New()
	r.Use(func(c *hypcontext.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	})
	r.NotFound(func(c *hypcontext.Context) { panic("boom in 404 handler") })

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("panic escaped the middleware chain: %v", rec)
		}
	}()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (recovery should convert the panic)", w.Code)
	}
}
