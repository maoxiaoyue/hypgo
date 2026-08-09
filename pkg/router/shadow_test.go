package router

import (
	"net/http/httptest"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// get 輔助：發 GET 並回傳 (status, body)
func get(r *Router, path string) (int, string) {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w.Code, w.Body.String()
}

// TestStaticParamCoexist 回歸測試：靜態路由與參數路由必須共存。
// 歷史 bug（雙向都壞）：
//   - 先靜態後參數：insertChild 直接覆寫 children，靜態路由被靜默丟棄
//     （GET /users/list 回 PARAM:list）
//   - 先參數後靜態：addRoute 對任何註冊一律走通配符相容性檢查 → panic
//
// 正確語意（Gin 同款）：兩者共存，比對時靜態優先、參數作 fallback。
func TestStaticParamCoexist(t *testing.T) {
	for _, order := range []string{"static-first", "param-first"} {
		r := New()
		staticH := func(c *hypcontext.Context) { c.String(200, "STATIC") }
		paramH := func(c *hypcontext.Context) { c.String(200, "PARAM:"+c.Param("id")) }

		if order == "static-first" {
			r.GET("/users/list", staticH)
			r.GET("/users/:id", paramH)
		} else {
			r.GET("/users/:id", paramH)
			r.GET("/users/list", staticH)
		}

		if code, body := get(r, "/users/list"); code != 200 || body != "STATIC" {
			t.Errorf("[%s] GET /users/list = (%d, %q), want (200, STATIC)", order, code, body)
		}
		if code, body := get(r, "/users/42"); code != 200 || body != "PARAM:42" {
			t.Errorf("[%s] GET /users/42 = (%d, %q), want (200, PARAM:42)", order, code, body)
		}
	}
}

// TestStaticDeadEndBacktracksToParam 靜態子樹前綴命中但無完整匹配時，
// 必須回溯（backtrack）到參數路由，而非直接 404。
func TestStaticDeadEndBacktracksToParam(t *testing.T) {
	r := New()
	r.GET("/a/list/x", func(c *hypcontext.Context) { c.String(200, "DEEP-STATIC") })
	r.GET("/a/:p", func(c *hypcontext.Context) { c.String(200, "P:"+c.Param("p")) })

	// 完整靜態命中
	if code, body := get(r, "/a/list/x"); code != 200 || body != "DEEP-STATIC" {
		t.Errorf("GET /a/list/x = (%d, %q), want (200, DEEP-STATIC)", code, body)
	}
	// "list" 走靜態子樹會死路（子樹只有 /a/list/x）→ 應回溯給 :p
	if code, body := get(r, "/a/list"); code != 200 || body != "P:list" {
		t.Errorf("GET /a/list = (%d, %q), want (200, P:list) via backtrack", code, body)
	}
	// 一般參數
	if code, body := get(r, "/a/zzz"); code != 200 || body != "P:zzz" {
		t.Errorf("GET /a/zzz = (%d, %q), want (200, P:zzz)", code, body)
	}
}

// TestStaticCatchAllCoexist 靜態路由與 catch-all 亦須共存（雙向註冊順序）。
func TestStaticCatchAllCoexist(t *testing.T) {
	for _, order := range []string{"static-first", "catchall-first"} {
		r := New()
		staticH := func(c *hypcontext.Context) { c.String(200, "ICO") }
		catchH := func(c *hypcontext.Context) { c.String(200, "FILE:"+c.Param("filepath")) }

		if order == "static-first" {
			r.GET("/static/favicon.ico", staticH)
			r.GET("/static/*filepath", catchH)
		} else {
			r.GET("/static/*filepath", catchH)
			r.GET("/static/favicon.ico", staticH)
		}

		if code, body := get(r, "/static/favicon.ico"); code != 200 || body != "ICO" {
			t.Errorf("[%s] GET /static/favicon.ico = (%d, %q), want (200, ICO)", order, code, body)
		}
		if code, body := get(r, "/static/js/app.js"); code != 200 || body != "FILE:/js/app.js" {
			t.Errorf("[%s] GET /static/js/app.js = (%d, %q), want (200, FILE:/js/app.js)", order, code, body)
		}
	}
}

// TestDifferentWildcardNamesStillConflict 不同名的通配符仍屬衝突，必須 panic（保留原防呆）。
func TestDifferentWildcardNamesStillConflict(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for conflicting wildcard names (:id vs :uid)")
		}
	}()
	r := New()
	r.GET("/users/:id", func(c *hypcontext.Context) {})
	r.GET("/users/:uid/posts", func(c *hypcontext.Context) {})
}
