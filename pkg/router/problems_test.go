package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// 根目錄的 .well-known 可以提供，其他點開頭的路徑照樣擋。
// 歷史 bug：/.well-known/security.txt 回 404（ACME HTTP-01、apple-app-site-association 都受影響），
// 唯一的開關 AllowDotFiles 會連 .env 一起開放
func TestStaticServesWellKnown(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		".well-known/security.txt":   "contact",
		".well-known/.hidden":        "no",
		"sub/.well-known/x.txt":      "no",
		".env":                       "SECRET",
		".well-known/acme/token.txt": "tok",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	r := New()
	r.Static("/", dir)
	for path, want := range map[string]int{
		"/.well-known/security.txt":   200,
		"/.well-known/acme/token.txt": 200,
		"/.well-known/.hidden":        404,
		"/sub/.well-known/x.txt":      404,
		"/.env":                       404,
	} {
		if w := serve(r, "GET", path); w.Code != want {
			t.Errorf("%s = %d, want %d", path, w.Code, want)
		}
	}
}

// 靜態檔案的 404 與其他路徑的 404 同一個格式，自訂的 NoRoute 也適用。
// 歷史 bug：http.FileServer 的純文字 "404 page not found"
func TestStaticNotFoundUsesRouterNotFound(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	r := New()
	r.Static("/static", dir)
	w := serve(r, "GET", "/static/missing.txt")
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"E0001"`) || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("missing file: %d %q (%s)", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
	}
	if w.Header().Get("X-Content-Type-Options") != "" {
		t.Errorf("leftover header from http.Error: %v", w.Header())
	}
	if w := serve(r, "GET", "/static/a.txt"); w.Code != 200 || w.Body.String() != "hello" {
		t.Errorf("existing file: %d %q", w.Code, w.Body.String())
	}
}

// Router.Schema 的路徑沒有前導斜線時補上，與 Group.Schema、r.GET 一致。
// 歷史 bug：路由先以 /users 註冊，接著 schema 登錄 panic，留下半註冊的路由
func TestRouterSchemaNormalizesPath(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := New()
	r.Schema(schema.Route{Method: "GET", Path: "plainusers", Summary: "list"}).Handle(ok)
	if w := serve(r, "GET", "/plainusers"); w.Code != 200 {
		t.Errorf("GET /plainusers = %d", w.Code)
	}
	if _, found := schema.Global().Get("GET", "/plainusers"); !found {
		t.Error("the schema is not registered under /plainusers")
	}
}
