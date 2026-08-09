package router

import (
	"net/http/httptest"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// TestRedirectStatusActuallySent 回歸測試：c.Redirect 必須真正送出 3xx 狀態碼。
// 歷史 bug：responseWriter.WriteHeader 是惰性的，而 stdlib http.Redirect 只有
// GET 會寫 HTML body（觸發 flush），POST/PUT/DELETE 不寫 body → 底層從未收到
// WriteHeader → Go 自動補 200 → 瀏覽器收到 200 + Location header 而不跳轉。
func TestRedirectStatusActuallySent(t *testing.T) {
	r := New()
	r.POST("/submit", func(c *hypcontext.Context) { c.Redirect(302, "/done") })
	r.GET("/go", func(c *hypcontext.Context) { c.Redirect(302, "/done") })

	// POST 表單轉址（歷史上壞掉的情境：無 body 寫入）
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/submit", nil))
	if w.Code != 302 {
		t.Errorf("POST redirect: got status %d, want 302 (Location=%q)", w.Code, w.Header().Get("Location"))
	}
	if got := w.Header().Get("Location"); got != "/done" {
		t.Errorf("POST redirect: Location = %q, want %q", got, "/done")
	}

	// GET 轉址（一直正常，防止回歸）
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest("GET", "/go", nil))
	if w2.Code != 302 {
		t.Errorf("GET redirect: got status %d, want 302", w2.Code)
	}
}
