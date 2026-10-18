package router

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

func staticDir(t *testing.T) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestStaticServesDirectoryIndex 迴歸測試：Static 的目錄根（/static/）要能提供 index.html。
// 歷史 bug：catch-all 不匹配空的剩餘路徑，/static/ 一律 404；/static/index.html 又被
// http.FileServer 依標準行為 301 回 /static/ —— 首頁永遠打不開。
// 另含 Static("/assets/") 結尾斜線：先前產生 "/assets//*filepath" 而全部 404
func TestStaticServesDirectoryIndex(t *testing.T) {
	dir := staticDir(t)
	r := New()
	r.Static("/static", dir)
	r.Static("/assets/", dir)

	for _, p := range []string{"/static/", "/assets/", "/assets/index.html"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if p == "/assets/index.html" {
			if w.Code != http.StatusMovedPermanently { // FileServer 把 index.html 導回目錄
				t.Errorf("GET %s = %d, want 301 to the directory", p, w.Code)
			}
			continue
		}
		if w.Code != http.StatusOK || w.Body.String() != "home" {
			t.Errorf("GET %s = %d %q, want 200 home", p, w.Code, w.Body.String())
		}
	}

	// 空字串前綴先前會在 StaticFS 內 urlPattern[len-1] panic
	New().StaticFS("", http.Dir(dir))
}

// TestTrailingSlashRedirect 迴歸測試：尾斜線重導向保留方法與 query、經過全域中間件、不重複寫標頭。
// 歷史 bug：一律 301（POST 被瀏覽器改成 GET、body 遺失）、丟掉 query string、直接寫底層 writer
// 跳過 CORS / Security 等中間件，且每次重導向 server log 多兩行 superfluous WriteHeader。
func TestTrailingSlashRedirect(t *testing.T) {
	r := New(WithStrictSlash(true))
	r.Use(func(c *hypcontext.Context) { c.Header("X-MW", "1"); c.Next() })
	r.POST("/users", func(c *hypcontext.Context) { c.Status(http.StatusCreated) })
	r.GET("/search", func(c *hypcontext.Context) {})

	srv := httptest.NewServer(r)
	defer srv.Close()
	var errLog bytes.Buffer
	srv.Config.ErrorLog = log.New(&errLog, "", 0)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Post(srv.URL+"/users/", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPermanentRedirect || resp.Header.Get("Location") != "/users" {
		t.Errorf("POST /users/ = %d Location=%q, want 308 /users", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.Header.Get("X-MW") != "1" {
		t.Error("redirect skipped global middleware")
	}

	resp, err = client.Get(srv.URL + "/search/?q=go")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently || resp.Header.Get("Location") != "/search?q=go" {
		t.Errorf("GET /search/?q=go = %d Location=%q, want 301 /search?q=go", resp.StatusCode, resp.Header.Get("Location"))
	}

	if errLog.Len() > 0 {
		t.Errorf("server logged: %s", errLog.String())
	}

	// URL.Path 為空（如 HTTP/2 CONNECT）先前 index out of range
	req := httptest.NewRequest("GET", "/", nil)
	req.URL.Path = ""
	r.ServeHTTP(httptest.NewRecorder(), req)
}

// TestMethodNotAllowedSetsAllow 迴歸測試：405 必須帶 Allow（RFC 9110 §15.5.6）；有 GET 即含 HEAD。
func TestMethodNotAllowedSetsAllow(t *testing.T) {
	r := New()
	r.GET("/items", func(c *hypcontext.Context) {})
	r.POST("/items", func(c *hypcontext.Context) {})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/items", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD, POST" {
		t.Errorf("DELETE /items = %d Allow=%q, want 405 \"GET, HEAD, POST\"", w.Code, w.Header().Get("Allow"))
	}
}

// TestEmptyParamDoesNotMatch 迴歸測試：空的參數值不匹配。
// 歷史 bug：/users//posts 對到 /users/:id/posts，id 為空字串
func TestEmptyParamDoesNotMatch(t *testing.T) {
	r := New()
	r.GET("/users/:id/posts", func(c *hypcontext.Context) {})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/users//posts", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /users//posts = %d, want 404", w.Code)
	}
}

// TestMatchValidatesMethods 迴歸測試：Match 與 Handle 一樣驗證方法名稱。
// 歷史 bug：Match([]string{"get"}, …) 建出 "get" 路由樹，真正的 GET 請求 404
func TestMatchValidatesMethods(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Match with an invalid method name did not panic")
		}
	}()
	New().Match([]string{"get"}, "/x", func(c *hypcontext.Context) {})
}

// TestMethodOverrideRoutesBeforeMatching 迴歸測試：方法覆寫在路由比對之前生效，且只允許 PUT / PATCH / DELETE。
// 歷史 bug：middleware.MethodOverride 在比對之後才改寫 c.Request.Method，POST 永遠只對到 POST 路由
func TestMethodOverrideRoutesBeforeMatching(t *testing.T) {
	r := New()
	r.EnableMethodOverride()
	r.POST("/items/1", func(c *hypcontext.Context) { c.String(200, "post") })
	r.DELETE("/items/1", func(c *hypcontext.Context) { c.String(200, "delete") })
	r.PUT("/items/1", func(c *hypcontext.Context) { c.String(200, "put") })
	r.GET("/items/1", func(c *hypcontext.Context) { c.String(200, "get") })

	send := func(header, form string) string {
		req := httptest.NewRequest("POST", "/items/1", strings.NewReader(form))
		if header != "" {
			req.Header.Set("X-HTTP-Method-Override", header)
		}
		if form != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Body.String()
	}
	if got := send("DELETE", ""); got != "delete" {
		t.Errorf("header override DELETE -> %q", got)
	}
	if got := send("", "_method=put"); got != "put" {
		t.Errorf("form override put -> %q", got)
	}
	if got := send("GET", ""); got != "post" {
		t.Errorf("override to GET must be ignored, got %q", got)
	}
}
