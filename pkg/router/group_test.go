package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// writeOK 測試用處理器，直接使用 Response 寫入避免 Writer 別名問題
func writeOK(c *hypcontext.Context) {
	c.Response.WriteHeader(http.StatusOK)
	c.Response.Write([]byte("ok"))
}

// writeBody 測試用處理器，寫入指定內容
func writeBody(body string) hypcontext.HandlerFunc {
	return func(c *hypcontext.Context) {
		c.Response.WriteHeader(http.StatusOK)
		c.Response.Write([]byte(body))
	}
}

// TestNewGroup 測試創建子路由分組
func TestNewGroup(t *testing.T) {
	r := New()

	api := r.NewGroup("/api")
	if api.BasePath() != "/api" {
		t.Errorf("expected basePath '/api', got '%s'", api.BasePath())
	}

	v1 := api.NewGroup("/v1")
	if v1.BasePath() != "/api/v1" {
		t.Errorf("expected basePath '/api/v1', got '%s'", v1.BasePath())
	}

	// 嵌套分組
	users := v1.NewGroup("/users")
	if users.BasePath() != "/api/v1/users" {
		t.Errorf("expected basePath '/api/v1/users', got '%s'", users.BasePath())
	}
}

// TestGroupWithMiddleware 測試分組中間件
func TestGroupWithMiddleware(t *testing.T) {
	r := New()

	called := false
	mw := func(c *hypcontext.Context) {
		called = true
	}

	api := r.NewGroup("/api", mw)
	api.GET("/test", writeOK)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)

	if !called {
		t.Error("middleware was not called")
	}
}

// TestGroupUse 測試 GroupUse 添加中間件
func TestGroupUse(t *testing.T) {
	r := New()

	order := make([]int, 0)
	mw1 := func(c *hypcontext.Context) { order = append(order, 1) }
	mw2 := func(c *hypcontext.Context) { order = append(order, 2) }

	api := r.NewGroup("/api")
	api.GroupUse(mw1, mw2)
	api.GET("/test", func(c *hypcontext.Context) {
		order = append(order, 3)
		c.Response.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("expected middleware order [1, 2, 3], got %v", order)
	}
}

// TestGroupMiddlewareInheritance 測試子組繼承父組中間件
func TestGroupMiddlewareInheritance(t *testing.T) {
	r := New()

	order := make([]int, 0)
	parentMW := func(c *hypcontext.Context) { order = append(order, 1) }
	childMW := func(c *hypcontext.Context) { order = append(order, 2) }

	api := r.NewGroup("/api", parentMW)
	v1 := api.NewGroup("/v1", childMW)
	v1.GET("/test", func(c *hypcontext.Context) {
		order = append(order, 3)
		c.Response.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	r.ServeHTTP(w, req)

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("expected middleware order [1, 2, 3], got %v", order)
	}
}

// TestGroupHTTPMethods 測試所有 HTTP 方法註冊
func TestGroupHTTPMethods(t *testing.T) {
	r := New()
	api := r.NewGroup("/api")

	api.GET("/get", writeOK)
	api.POST("/post", writeOK)
	api.PUT("/put", writeOK)
	api.DELETE("/delete", writeOK)
	api.PATCH("/patch", writeOK)
	api.OPTIONS("/options", writeOK)
	api.HEAD("/head", writeOK)

	methods := map[string]string{
		http.MethodGet:     "/api/get",
		http.MethodPost:    "/api/post",
		http.MethodPut:     "/api/put",
		http.MethodDelete:  "/api/delete",
		http.MethodPatch:   "/api/patch",
		http.MethodOptions: "/api/options",
		http.MethodHead:    "/api/head",
	}

	for method, path := range methods {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s %s: expected status 200, got %d", method, path, w.Code)
		}
	}
}

// TestGroupChaining 測試鏈式調用
func TestGroupChaining(t *testing.T) {
	r := New()

	api := r.NewGroup("/api")
	api.GET("/a", writeOK).
		GET("/b", writeOK).
		POST("/c", writeOK)

	routes := r.Routes()
	expected := map[string]bool{
		"GET /api/a":  false,
		"GET /api/b":  false,
		"POST /api/c": false,
	}

	for _, route := range routes {
		key := route.Method + " " + route.Path
		if _, ok := expected[key]; ok {
			expected[key] = true
		}
	}

	for key, found := range expected {
		if !found {
			t.Errorf("expected route '%s' not found", key)
		}
	}
}

// TestRootGroupChaining 測試根組鏈式調用
func TestRootGroupChaining(t *testing.T) {
	r := New()

	r.GET("/a", writeOK).
		GET("/b", writeOK).
		POST("/c", writeOK)

	routes := r.Routes()
	expected := map[string]bool{
		"GET /a":  false,
		"GET /b":  false,
		"POST /c": false,
	}

	for _, route := range routes {
		key := route.Method + " " + route.Path
		if _, ok := expected[key]; ok {
			expected[key] = true
		}
	}

	for key, found := range expected {
		if !found {
			t.Errorf("expected route '%s' not found", key)
		}
	}
}

// TestGroupAny 測試 Any 方法
func TestGroupAny(t *testing.T) {
	r := New()
	api := r.NewGroup("/api")
	api.Any("/any", writeOK)

	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodDelete,
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/any", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s /api/any: expected status 200, got %d", method, w.Code)
		}
	}
}

// TestGroupMatch 測試 Match 方法
func TestGroupMatch(t *testing.T) {
	r := New(WithMethodNotAllowed(false))
	api := r.NewGroup("/api")
	api.Match([]string{http.MethodGet, http.MethodPost}, "/match", writeOK)

	// GET 應該匹配
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/match", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /api/match: expected 200, got %d", w.Code)
	}

	// POST 應該匹配
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/match", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST /api/match: expected 200, got %d", w.Code)
	}
}

// TestGroupHandle 測試 Handle 自定義方法
func TestGroupHandle(t *testing.T) {
	r := New()
	api := r.NewGroup("/api")
	api.Handle(http.MethodGet, "/custom", writeOK)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/custom", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/custom: expected 200, got %d", w.Code)
	}
}

// TestGroupHandleInvalidMethod 測試非法 HTTP 方法 panic
func TestGroupHandleInvalidMethod(t *testing.T) {
	r := New()
	api := r.NewGroup("/api")

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid HTTP method")
		}
	}()
	api.Handle("INVALID", "/test", writeOK)
}

// TestGroupReturnObj 測試 returnObj 邏輯
func TestGroupReturnObj(t *testing.T) {
	r := New()

	// 根組的 returnObj 應該返回 *Router
	result := r.GET("/root", writeOK)
	if _, ok := result.(*Router); !ok {
		t.Error("root group returnObj should return *Router")
	}

	// 子組的 returnObj 應該返回 *Group
	api := r.NewGroup("/api")
	result = api.GET("/sub", writeOK)
	if _, ok := result.(*Group); !ok {
		t.Error("sub group returnObj should return *Group")
	}
}

// TestGroupWithParams 測試帶參數的分組路由
func TestGroupWithParams(t *testing.T) {
	r := New()

	api := r.NewGroup("/api/v1")
	api.GET("/users/:id", func(c *hypcontext.Context) {
		id, _ := c.Params.Get("id")
		c.Response.WriteHeader(http.StatusOK)
		c.Response.Write([]byte("user:" + id))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "user:42" {
		t.Errorf("expected 'user:42', got '%s'", w.Body.String())
	}
}

// TestGroupGlobalMiddleware 測試全域中間件與分組中間件的執行順序
func TestGroupGlobalMiddleware(t *testing.T) {
	r := New()

	order := make([]int, 0)
	r.Use(func(c *hypcontext.Context) { order = append(order, 1) })

	api := r.NewGroup("/api")
	api.GroupUse(func(c *hypcontext.Context) { order = append(order, 2) })
	api.GET("/test", func(c *hypcontext.Context) {
		order = append(order, 3)
		c.Response.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)

	// 順序：全域中間件(1) → 分組中間件(2) → Handler(3)
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("expected execution order [1, 2, 3], got %v", order)
	}
}

// TestInterfaceCompliance 測試介面合規性
func TestInterfaceCompliance(t *testing.T) {
	var _ IRoutes = (*Group)(nil)
	var _ IRouter = (*Group)(nil)
	var _ IRoutes = (*Router)(nil)
	var _ IRouter = (*Router)(nil)
}

// TestMultipleGroupsSamePrefix 測試相同前綴的多個分組
func TestMultipleGroupsSamePrefix(t *testing.T) {
	r := New()

	public := r.NewGroup("/api")
	public.GET("/info", writeBody("public"))

	private := r.NewGroup("/api")
	private.GroupUse(func(c *hypcontext.Context) {
		// 模擬認證中間件
	})
	private.GET("/secret", writeBody("private"))

	// 公開路由
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	r.ServeHTTP(w, req)
	if w.Body.String() != "public" {
		t.Errorf("expected 'public', got '%s'", w.Body.String())
	}

	// 私有路由
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/secret", nil)
	r.ServeHTTP(w, req)
	if w.Body.String() != "private" {
		t.Errorf("expected 'private', got '%s'", w.Body.String())
	}
}

// TestGroupUseChaining 測試 GroupUse 鏈式調用
func TestGroupUseChaining(t *testing.T) {
	r := New()
	order := make([]int, 0)

	api := r.NewGroup("/api")
	api.GroupUse(func(c *hypcontext.Context) {
		order = append(order, 1)
	}).GroupUse(func(c *hypcontext.Context) {
		order = append(order, 2)
	})

	api.GET("/test", func(c *hypcontext.Context) {
		order = append(order, 3)
		c.Response.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("expected [1, 2, 3], got %v", order)
	}
}

// TestNestedGroupMiddleware 測試多層嵌套分組中間件
func TestNestedGroupMiddleware(t *testing.T) {
	r := New()
	order := make([]int, 0)

	r.Use(func(c *hypcontext.Context) { order = append(order, 1) })

	api := r.NewGroup("/api")
	api.GroupUse(func(c *hypcontext.Context) { order = append(order, 2) })

	v1 := api.NewGroup("/v1")
	v1.GroupUse(func(c *hypcontext.Context) { order = append(order, 3) })

	admin := v1.NewGroup("/admin")
	admin.GroupUse(func(c *hypcontext.Context) { order = append(order, 4) })

	admin.GET("/dashboard", func(c *hypcontext.Context) {
		order = append(order, 5)
		c.Response.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	r.ServeHTTP(w, req)

	// 全域(1) → api(2) → v1(3) → admin(4) → handler(5)
	if len(order) != 5 {
		t.Fatalf("expected 5 calls, got %d: %v", len(order), order)
	}
	for i, expected := range []int{1, 2, 3, 4, 5} {
		if order[i] != expected {
			t.Errorf("order[%d] = %d, expected %d", i, order[i], expected)
		}
	}
}
