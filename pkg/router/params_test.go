package router

import (
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// discardWriter 最小 ResponseWriter，避免 httptest.Recorder 自身的配置干擾量測
type discardWriter struct{ h http.Header }

func (w *discardWriter) Header() http.Header         { return w.h }
func (w *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *discardWriter) WriteHeader(int)             {}

// TestParamRouteAllocsMatchStatic 帶參數路由不得比靜態路由多配置。
// 歷史：參數先 search 進 router 自己的池，再逐一複製到 hypcontext 的 Params 池，
// 而後者每次歸還都把 slice header 逃逸到堆上（SA6002）——每個帶參數請求多一次配置。
// 現在 search 直接寫進 Context 自帶的參數緩衝。
func TestParamRouteAllocsMatchStatic(t *testing.T) {
	if raceEnabled() {
		t.Skip("race 模式下 sync.Pool 會隨機丟棄 Put，配置次數不穩定")
	}
	r := New()
	r.GET("/static", func(c *hypcontext.Context) { c.Status(http.StatusNoContent) })
	r.GET("/users/:id/posts/:pid", func(c *hypcontext.Context) {
		if c.Param("id") != "42" || c.Param("pid") != "7" {
			t.Errorf("params = %v", c.Params)
		}
		c.Status(http.StatusNoContent)
	})

	w := &discardWriter{h: http.Header{}}
	static := httptest.NewRequest("GET", "/static", nil)
	param := httptest.NewRequest("GET", "/users/42/posts/7", nil)

	staticAllocs := testing.AllocsPerRun(200, func() { r.ServeHTTP(w, static) })
	paramAllocs := testing.AllocsPerRun(200, func() { r.ServeHTTP(w, param) })
	if paramAllocs > staticAllocs {
		t.Errorf("param route allocs = %.0f, static route allocs = %.0f; params should add none",
			paramAllocs, staticAllocs)
	}
}

// raceEnabled 回報測試是否以 -race 建置（build info 的 -race 設定）
func raceEnabled() bool {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "-race" {
				return s.Value == "true"
			}
		}
	}
	return false
}

// TestParamsDoNotLeakAcrossRequests 參數緩衝隨 Context 重用，下一個請求
// （無參數路由）不得看到上一個請求的參數。
func TestParamsDoNotLeakAcrossRequests(t *testing.T) {
	r := New()
	r.GET("/users/:id", func(c *hypcontext.Context) { c.Status(http.StatusNoContent) })
	r.GET("/health", func(c *hypcontext.Context) {
		if len(c.Params) != 0 || c.Param("id") != "" {
			t.Errorf("params leaked into next request: %v", c.Params)
		}
		c.Status(http.StatusNoContent)
	})

	w := &discardWriter{h: http.Header{}}
	for i := 0; i < 50; i++ {
		r.ServeHTTP(w, httptest.NewRequest("GET", "/users/42", nil))
		r.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	}
}
