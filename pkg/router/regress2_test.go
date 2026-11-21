package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

func serve(r *Router, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func ok(c *hypcontext.Context) { c.String(200, "ok") }

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: 應該在註冊時 panic", name)
		}
	}()
	f()
}

func staticTree(t *testing.T) string {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"index.html":      "home",
		"sub/a.txt":       "A",
		".env":            "SECRET=1",
		".git/config":     "[core]",
		"sub/.hidden.txt": "H",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// 先前 Static 把沒有 index.html 的目錄列出來，.env / .git 也照樣送出
func TestStaticHidesListingsAndDotFiles(t *testing.T) {
	r := New()
	r.Static("/assets", staticTree(t))
	for path, want := range map[string]int{
		"/assets/":                200, // 有 index.html
		"/assets/sub/a.txt":       200,
		"/assets/sub/":            404, // 沒有 index.html：先前回目錄列表
		"/assets/sub":             404, // 先前 301 → /assets/sub/ → 列表
		"/assets/.env":            404,
		"/assets/.git/config":     404,
		"/assets/sub/.hidden.txt": 404,
	} {
		w := serve(r, "GET", path)
		if w.Code != want {
			t.Errorf("GET %s = %d，應為 %d（body %q）", path, w.Code, want, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "a.txt") {
			t.Errorf("GET %s 洩漏了內容：%q", path, w.Body.String())
		}
	}
}

func TestStaticWithOptions(t *testing.T) {
	dir := staticTree(t)
	r := New()
	r.StaticWith("/list", http.Dir(dir), StaticOptions{ListDirectories: true})
	r.StaticWith("/dot", http.Dir(dir), StaticOptions{AllowDotFiles: true})
	if w := serve(r, "GET", "/list/sub/"); w.Code != 200 || !strings.Contains(w.Body.String(), "a.txt") {
		t.Errorf("ListDirectories: %d %q", w.Code, w.Body.String())
	}
	if w := serve(r, "GET", "/list/.env"); w.Code != 404 {
		t.Errorf("ListDirectories 不該連帶開放點開頭的檔案：%d", w.Code)
	}
	if w := serve(r, "GET", "/list/sub/"); strings.Contains(w.Body.String(), ".hidden") {
		// 列表由 http.FileServer 產生，會列出檔名；檔案本身仍是 404
		if serve(r, "GET", "/list/sub/.hidden.txt").Code != 404 {
			t.Error("列表中的點開頭檔案不該可讀")
		}
	}
	if w := serve(r, "GET", "/dot/.env"); w.Code != 200 || w.Body.String() != "SECRET=1" {
		t.Errorf("AllowDotFiles: %d %q", w.Code, w.Body.String())
	}
	if w := serve(r, "GET", "/dot/sub/"); w.Code != 404 {
		t.Errorf("AllowDotFiles 不該連帶開放目錄列表：%d", w.Code)
	}
}

// 先前 /assets 是 404，只有 /assets/ 有首頁
func TestCatchAllRootRedirectsToSlash(t *testing.T) {
	r := New()
	r.Static("/assets", staticTree(t))
	r.GET("/u/:id", ok)
	for _, m := range []string{"GET", "HEAD"} {
		w := serve(r, m, "/assets?v=1")
		if w.Code != 301 || w.Header().Get("Location") != "/assets/?v=1" {
			t.Errorf("%s /assets = %d Location=%q，應為 301 /assets/?v=1", m, w.Code, w.Header().Get("Location"))
		}
	}
	// 不是 catch-all 的路由不受影響（預設不做尾斜線重導向）
	if w := serve(r, "GET", "/u/1/"); w.Code != 404 {
		t.Errorf("GET /u/1/ = %d，應為 404", w.Code)
	}
	if w := serve(r, "GET", "/u"); w.Code != 404 {
		t.Errorf("GET /u = %d，應為 404", w.Code)
	}
	r.GET("/dir/", ok)
	if w := serve(r, "GET", "/dir"); w.Code != 404 {
		t.Errorf("GET /dir = %d，應為 404（/dir/ 不是 catch-all）", w.Code)
	}
}

// 先前預設的 404 / 405 沒有 body 也沒有 Content-Type
func TestDefaultNotFoundAndMethodNotAllowedBodies(t *testing.T) {
	r := New()
	r.GET("/users", ok)
	for _, tc := range []struct {
		method, path string
		code         int
		body         string
	}{
		{"GET", "/nope", 404, `{"code":"E0001","message":"Resource not found"}`},
		{"DELETE", "/users", 405, `{"code":"E0004","message":"Method not allowed"}`},
	} {
		w := serve(r, tc.method, tc.path)
		if w.Code != tc.code || strings.TrimSpace(w.Body.String()) != tc.body {
			t.Errorf("%s %s = %d %q，應為 %d %s", tc.method, tc.path, w.Code, w.Body.String(), tc.code, tc.body)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s %s Content-Type = %q", tc.method, tc.path, ct)
		}
	}
	if w := serve(r, "HEAD", "/nope"); w.Code != 404 {
		t.Errorf("HEAD /nope = %d", w.Code)
	}
}

func TestWithRedirectTrailingSlash(t *testing.T) {
	for name, opt := range map[string]RouterOption{
		"WithRedirectTrailingSlash": WithRedirectTrailingSlash(true),
		"WithStrictSlash（舊名）":       WithStrictSlash(true),
	} {
		r := New(opt)
		r.GET("/users", ok)
		if w := serve(r, "GET", "/users/?a=1"); w.Code != 301 || w.Header().Get("Location") != "/users?a=1" {
			t.Errorf("%s: %d Location=%q", name, w.Code, w.Header().Get("Location"))
		}
		// HEAD 沒有自己的路由時也跟著 GET 重導向（先前 405）
		if w := serve(r, "HEAD", "/users/"); w.Code != 301 {
			t.Errorf("%s: HEAD /users/ = %d，應為 301", name, w.Code)
		}
	}
	r := New()
	r.GET("/users", ok)
	if w := serve(r, "GET", "/users/"); w.Code != 404 {
		t.Errorf("預設不重導向：%d", w.Code)
	}
}

func TestRegistrationRejectsBrokenRoutes(t *testing.T) {
	// 先前註冊成功：/filesabc → rest="/abc"、/files/abc → rest="//abc"
	mustPanic(t, "catch-all 前面沒有斜線", func() { New().GET("/files*rest", ok) })
	// 先前註冊成功，請求進來才 nil pointer panic
	mustPanic(t, "nil handler", func() { New().GET("/nil", nil) })
	mustPanic(t, "nil 中間件", func() { New().NewGroup("/g", nil).GET("/x", ok) })
	mustPanic(t, "含空白的方法", func() { New().Handle("GE T", "/x", ok) })
	mustPanic(t, "空方法", func() { New().Handle("", "/x", ok) })

	r := New()
	r.GET("/files/*rest", func(c *hypcontext.Context) { c.String(200, c.Param("rest")) })
	if w := serve(r, "GET", "/files/a/b"); w.Body.String() != "/a/b" {
		t.Errorf("rest = %q", w.Body.String())
	}
}

// 先前 Routes() 的順序每次呼叫都不同
func TestRoutesAreSorted(t *testing.T) {
	r := New()
	for _, p := range []string{"/c", "/a", "/b/:id", "/b"} {
		r.GET(p, ok)
		r.POST(p, ok)
		r.DELETE(p, ok)
	}
	want := "DELETE /a,GET /a,POST /a,DELETE /b,GET /b,POST /b,DELETE /b/:id,GET /b/:id,POST /b/:id,DELETE /c,GET /c,POST /c"
	for i := 0; i < 20; i++ {
		var got []string
		for _, ri := range r.Routes() {
			got = append(got, ri.Method+" "+ri.Path)
		}
		if s := strings.Join(got, ","); s != want {
			t.Fatalf("Routes() = %s\n  應為   %s", s, want)
		}
	}
}

// 先前 Any 連 TRACE / CONNECT 都註冊；Handle 不收 PROPFIND 這類擴充方法
func TestAnyAndCustomMethods(t *testing.T) {
	r := New()
	r.Any("/any", ok)
	r.Handle("PROPFIND", "/dav", ok)
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		if w := serve(r, m, "/any"); w.Code != 200 {
			t.Errorf("%s /any = %d", m, w.Code)
		}
	}
	for _, m := range []string{"TRACE", "CONNECT"} {
		if w := serve(r, m, "/any"); w.Code != 405 {
			t.Errorf("%s /any = %d，應為 405", m, w.Code)
		}
	}
	if w := serve(r, "PROPFIND", "/dav"); w.Code != 200 {
		t.Errorf("PROPFIND /dav = %d", w.Code)
	}
}

// 先前沒註冊 OPTIONS 的路徑回 405
func TestAutomaticOptions(t *testing.T) {
	r := New()
	var sawMiddleware bool
	r.Use(func(c *hypcontext.Context) { sawMiddleware = true; c.Next() })
	r.GET("/users", ok)
	r.POST("/users", ok)
	r.OPTIONS("/own", func(c *hypcontext.Context) { c.String(200, "mine") })

	w := serve(r, "OPTIONS", "/users")
	if w.Code != 204 || w.Header().Get("Allow") != "GET, HEAD, OPTIONS, POST" || w.Body.Len() != 0 {
		t.Errorf("OPTIONS /users = %d Allow=%q body=%q", w.Code, w.Header().Get("Allow"), w.Body.String())
	}
	if !sawMiddleware {
		t.Error("自動 OPTIONS 應經過全域中間件")
	}
	if w := serve(r, "OPTIONS", "/own"); w.Body.String() != "mine" {
		t.Errorf("已註冊的 OPTIONS 應優先：%q", w.Body.String())
	}
	if w := serve(r, "OPTIONS", "/nope"); w.Code != 404 {
		t.Errorf("OPTIONS /nope = %d，應為 404", w.Code)
	}
	// 關掉 405 處理時也不自動回應
	r2 := New(WithMethodNotAllowed(false))
	r2.GET("/users", ok)
	if w := serve(r2, "OPTIONS", "/users"); w.Code != 404 {
		t.Errorf("WithMethodNotAllowed(false): %d", w.Code)
	}
}
