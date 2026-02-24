package router

import (
	"net/http"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// IRoutes 定義路由註冊介面
// 所有路由方法都支援鏈式調用
type IRoutes interface {
	Handle(string, string, ...hypcontext.HandlerFunc) IRoutes
	Any(string, ...hypcontext.HandlerFunc) IRoutes
	GET(string, ...hypcontext.HandlerFunc) IRoutes
	POST(string, ...hypcontext.HandlerFunc) IRoutes
	DELETE(string, ...hypcontext.HandlerFunc) IRoutes
	PATCH(string, ...hypcontext.HandlerFunc) IRoutes
	PUT(string, ...hypcontext.HandlerFunc) IRoutes
	OPTIONS(string, ...hypcontext.HandlerFunc) IRoutes
	HEAD(string, ...hypcontext.HandlerFunc) IRoutes
	Match([]string, string, ...hypcontext.HandlerFunc) IRoutes
	Static(string, string) IRoutes
	StaticFile(string, string) IRoutes
	StaticFS(string, http.FileSystem) IRoutes
}

// IRouter 定義完整路由介面（包含路由分組）
type IRouter interface {
	IRoutes
	NewGroup(string, ...hypcontext.HandlerFunc) *Group
}

// 編譯期介面合規檢查
var _ IRouter = (*Group)(nil)
var _ IRouter = (*Router)(nil)

// Group 路由分組結構
// 用於將路由按前綴和中間件分組管理
// Router 嵌入 Group 作為根路由組
//
// 路由分組支援嵌套，子組會繼承父組的中間件鏈
//
// EX：
//
//	r := router.New()
//
//	// 創建 API v1 分組
//	api := r.NewGroup("/api/v1")
//
//	// 帶中間件的分組
//	auth := api.NewGroup("/auth", authMiddleware)
//	auth.POST("/login", loginHandler)
//
//	// 鏈式調用
//	api.GET("/users", listUsers).
//	    GET("/posts", listPosts).
//	    POST("/posts", createPost)
type Group struct {
	basePath   string                   // 路徑前綴
	middleware []hypcontext.HandlerFunc // 本組的中間件
	router     *Router                  // 所屬的主路由器
	isRoot     bool                     // 是否為根路由組
}

// returnObj 返回鏈式調用對象
// 若為根路由組，返回 Router（保持與 Router 方法的一致性）
// 否則返回 Group 本身
func (g *Group) returnObj() IRoutes {
	if g.isRoot {
		return g.router
	}
	return g
}

// NewGroup 創建新的子路由分組
// 子組會繼承父組的中間件，並可添加自己的中間件
//
// EX：
//
//	api := r.NewGroup("/api/v1")
//	api.GroupUse(authMiddleware)
//	{
//	    api.GET("/users", listUsers)
//	    api.POST("/users", createUser)
//	}
//
//	// 也可以在創建時直接指定中間件
//	admin := r.NewGroup("/admin", authMiddleware, adminOnlyMiddleware)
func (g *Group) NewGroup(relativePath string, handlers ...hypcontext.HandlerFunc) *Group {
	return &Group{
		basePath:   joinPaths(g.basePath, relativePath),
		middleware: g.combineHandlers(handlers),
		router:     g.router,
		isRoot:     false,
	}
}

// BasePath 返回路由組的基礎路徑
func (g *Group) BasePath() string {
	return g.basePath
}

// GroupUse 為路由組添加中間件
// 返回 *Group 以支援鏈式調用
//
// EX：
//
//	admin := r.NewGroup("/admin")
//	admin.GroupUse(authMiddleware, adminOnlyMiddleware)
//	admin.GET("/dashboard", dashboardHandler)
func (g *Group) GroupUse(middleware ...hypcontext.HandlerFunc) *Group {
	g.middleware = append(g.middleware, middleware...)
	return g
}

// handle 核心路由註冊（內部方法）
// 計算絕對路徑，合併中間件鏈，委託給 Router.addRoute
func (g *Group) handle(method, relativePath string, handlers []hypcontext.HandlerFunc) IRoutes {
	absolutePath := joinPaths(g.basePath, relativePath)
	finalHandlers := g.combineHandlers(handlers)
	g.router.addRoute(method, absolutePath, finalHandlers)
	return g.returnObj()
}

// Handle 註冊指定 HTTP 方法的路由
func (g *Group) Handle(method, relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	if !isValidHTTPMethod(method) {
		panic("router: invalid HTTP method: " + method)
	}
	return g.handle(method, relativePath, handlers)
}

// GET 註冊 GET 路由
func (g *Group) GET(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	return g.handle(http.MethodGet, relativePath, handlers)
}

// POST 註冊 POST 路由
func (g *Group) POST(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	return g.handle(http.MethodPost, relativePath, handlers)
}

// PUT 註冊 PUT 路由
func (g *Group) PUT(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	return g.handle(http.MethodPut, relativePath, handlers)
}

// DELETE 註冊 DELETE 路由
func (g *Group) DELETE(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	return g.handle(http.MethodDelete, relativePath, handlers)
}

// PATCH 註冊 PATCH 路由
func (g *Group) PATCH(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	return g.handle(http.MethodPatch, relativePath, handlers)
}

// OPTIONS 註冊 OPTIONS 路由
func (g *Group) OPTIONS(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	return g.handle(http.MethodOptions, relativePath, handlers)
}

// HEAD 註冊 HEAD 路由
func (g *Group) HEAD(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	return g.handle(http.MethodHead, relativePath, handlers)
}

// Any 註冊所有 HTTP 方法
func (g *Group) Any(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodDelete,
		http.MethodConnect, http.MethodTrace,
	} {
		g.handle(method, relativePath, handlers)
	}
	return g.returnObj()
}

// Match 註冊指定多個 HTTP 方法的路由
func (g *Group) Match(methods []string, relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	for _, method := range methods {
		g.handle(method, relativePath, handlers)
	}
	return g.returnObj()
}

// Static 服務靜態文件目錄
func (g *Group) Static(relativePath, dir string) IRoutes {
	absolutePath := joinPaths(g.basePath, relativePath)
	fs := http.FileServer(http.Dir(dir))
	handler := func(c *hypcontext.Context) {
		http.StripPrefix(absolutePath, fs).ServeHTTP(c.Writer, c.Request)
	}
	g.handle(http.MethodGet, relativePath+"/*filepath", []hypcontext.HandlerFunc{handler})
	return g.returnObj()
}

// StaticFile 服務單個靜態文件
func (g *Group) StaticFile(relativePath, filepath string) IRoutes {
	handler := func(c *hypcontext.Context) {
		c.File(filepath)
	}
	g.handle(http.MethodGet, relativePath, []hypcontext.HandlerFunc{handler})
	g.handle(http.MethodHead, relativePath, []hypcontext.HandlerFunc{handler})
	return g.returnObj()
}

// StaticFS 使用 http.FileSystem 服務靜態文件
func (g *Group) StaticFS(relativePath string, fs http.FileSystem) IRoutes {
	absolutePath := joinPaths(g.basePath, relativePath)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(fs))
	handler := func(c *hypcontext.Context) {
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
	urlPattern := relativePath
	if urlPattern[len(urlPattern)-1] != '/' {
		urlPattern += "/"
	}
	urlPattern += "*filepath"
	g.handle(http.MethodGet, urlPattern, []hypcontext.HandlerFunc{handler})
	g.handle(http.MethodHead, urlPattern, []hypcontext.HandlerFunc{handler})
	return g.returnObj()
}

// combineHandlers 合併中間件鏈
// 將本組的中間件與傳入的 handlers 合併為一個新的切片
func (g *Group) combineHandlers(handlers []hypcontext.HandlerFunc) []hypcontext.HandlerFunc {
	finalSize := len(g.middleware) + len(handlers)
	merged := make([]hypcontext.HandlerFunc, finalSize)
	copy(merged, g.middleware)
	copy(merged[len(g.middleware):], handlers)
	return merged
}

// isValidHTTPMethod 驗證 HTTP 方法
func isValidHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodDelete,
		http.MethodConnect, http.MethodTrace:
		return true
	}
	return false
}
