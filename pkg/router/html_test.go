package router

import (
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// TestHTMLTemplateRendering 回歸測試：c.HTML 必須能以樣板名渲染資料。
// 歷史 bug：樣板路徑依賴 c.routerGroup.engine，但 SetRouterGroup 無任何 caller，
// 該路徑永久死路——c.HTML 傳資料物件時靜默輸出空白頁。
func TestHTMLTemplateRendering(t *testing.T) {
	r := New()
	r.SetHTMLTemplate(template.Must(template.New("hello.html").Parse(`<h1>{{.Title}}</h1>`)))
	defer hypcontext.SetHTMLTemplate(nil)

	r.GET("/page", func(c *hypcontext.Context) {
		c.HTML(200, "hello.html", map[string]string{"Title": "Hi"})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/page", nil))

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "<h1>Hi</h1>") {
		t.Errorf("body = %q, want to contain <h1>Hi</h1>", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// TestHTMLRawStringFallback 無樣板集時，字串 obj 仍直接輸出原始 HTML（既有行為）
func TestHTMLRawStringFallback(t *testing.T) {
	hypcontext.SetHTMLTemplate(nil)

	r := New()
	r.GET("/raw", func(c *hypcontext.Context) {
		c.HTML(200, "", "<p>raw</p>")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/raw", nil))

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); body != "<p>raw</p>" {
		t.Errorf("body = %q, want %q", body, "<p>raw</p>")
	}
}

// TestHTMLNoTemplateNonStringPanics 無樣板集且 obj 非字串：大聲失敗而非靜默空白頁
func TestHTMLNoTemplateNonStringPanics(t *testing.T) {
	hypcontext.SetHTMLTemplate(nil)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when no template loaded and obj is not a string")
		}
	}()

	r := New()
	r.GET("/boom", func(c *hypcontext.Context) {
		c.HTML(200, "missing.html", map[string]string{"X": "y"})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/boom", nil))
}
