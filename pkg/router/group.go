// @chris
package router

import (
	"net/http"
	"os"
	"strings"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
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

// GroupUse 為路由組添加中間件，只影響之後註冊的路由（中間件鏈在註冊時合併）
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

// Handle 註冊指定 HTTP 方法的路由，可以是擴充方法（PROPFIND 等）。
// 方法名稱轉成大寫再註冊，與 r.Schema(...) 一致（schema 一向接受 "get"；先前這裡遇到小寫直接 panic，
// 兩條註冊路徑對同一個寫法的反應不同）
func (g *Group) Handle(method, relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	method = strings.ToUpper(strings.TrimSpace(method))
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

// Any 註冊 GET / POST / PUT / PATCH / HEAD / OPTIONS / DELETE。
// 不含 TRACE 與 CONNECT（先前一併註冊）：TRACE 會把請求原樣回給對方，CONNECT 是代理用的，
// 一般 handler 不該順便回應它們；需要時以 Handle 個別註冊
func (g *Group) Any(relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodDelete,
	} {
		g.handle(method, relativePath, handlers)
	}
	return g.returnObj()
}

// Match 註冊指定多個 HTTP 方法的路由（方法名稱同 Handle 驗證，"get" 等無效名稱會 panic）
func (g *Group) Match(methods []string, relativePath string, handlers ...hypcontext.HandlerFunc) IRoutes {
	for _, method := range methods {
		g.Handle(method, relativePath, handlers...)
	}
	return g.returnObj()
}

// StaticOptions 調整 StaticWith 的檔案服務行為；零值即 Static / StaticFS 的預設（兩者皆關）
type StaticOptions struct {
	// ListDirectories 目錄沒有 index.html 時回傳檔案列表；關閉時回 404
	ListDirectories bool
	// AllowDotFiles 提供任何一段以 "." 開頭的路徑（.env、.git/config）；關閉時回 404
	AllowDotFiles bool
}

// Static 服務靜態文件目錄。不列目錄、不提供點開頭的檔案（見 StaticWith）。
// 委派給 StaticFS：先前自己拼 relativePath+"/*filepath"，Static("/assets/", …) 會產生
// "/assets//*filepath" 而全部 404；且每個請求都重建 http.StripPrefix
func (g *Group) Static(relativePath, dir string) IRoutes {
	return g.StaticFS(relativePath, http.Dir(dir))
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

// StaticFS 使用 http.FileSystem 服務靜態文件。不列目錄、不提供點開頭的檔案（見 StaticWith）
func (g *Group) StaticFS(relativePath string, fs http.FileSystem) IRoutes {
	return g.StaticWith(relativePath, fs, StaticOptions{})
}

// StaticWith 同 StaticFS，可用 opts 開啟目錄列表或點開頭的檔案。
// 先前 Static / StaticFS 直接交給 http.FileServer：沒有 index.html 的目錄回傳檔案列表，
// 誤放進靜態目錄的 .env / .git 也照樣送出
func (g *Group) StaticWith(relativePath string, fs http.FileSystem, opts StaticOptions) IRoutes {
	absolutePath := joinPaths(g.basePath, relativePath)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(staticFS{fs, opts}))
	handler := func(c *hypcontext.Context) {
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
	urlPattern := relativePath
	if !strings.HasSuffix(urlPattern, "/") { // 空字串時先前 urlPattern[len-1] 會 panic
		urlPattern += "/"
	}
	urlPattern += "*filepath"
	g.handle(http.MethodGet, urlPattern, []hypcontext.HandlerFunc{handler})
	g.handle(http.MethodHead, urlPattern, []hypcontext.HandlerFunc{handler})
	return g.returnObj()
}

// staticFS 依 StaticOptions 把不該提供的路徑變成「不存在」，其餘交給底層檔案系統
type staticFS struct {
	fs   http.FileSystem
	opts StaticOptions
}

func (s staticFS) Open(name string) (http.File, error) {
	if !s.opts.AllowDotFiles {
		for _, seg := range strings.Split(name, "/") {
			if strings.HasPrefix(seg, ".") {
				return nil, os.ErrNotExist
			}
		}
	}
	f, err := s.fs.Open(name)
	if err != nil || s.opts.ListDirectories {
		return f, err
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		// 目錄只有在有 index.html 時才提供（http.FileServer 會接著開它）
		index, err := s.fs.Open(strings.TrimSuffix(name, "/") + "/index.html")
		if err != nil {
			f.Close()
			return nil, os.ErrNotExist
		}
		index.Close()
	}
	return f, nil
}

// Schema 開始 schema-first 路由註冊（Group 版本）
// 自動將 Group 的 basePath 前綴加到 route.Path
func (g *Group) Schema(route schema.Route) *schema.SchemaRoute {
	route.Path = joinPaths(g.basePath, route.Path)
	return schema.NewSchemaRoute(route, g)
}

// RegisterSchema 實作 schema.SchemaRegistrar 介面（Group 版本，見 schemaHandlers）
func (g *Group) RegisterSchema(route schema.Route, handlers ...hypcontext.HandlerFunc) {
	// route.Path 已由 Schema() 計算為絕對路徑
	g.router.addRoute(route.Method, route.Path, g.combineHandlers(schemaHandlers(&route, handlers)))
	schema.Global().Register(route)
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

// isValidHTTPMethod 驗證方法名稱：大寫的 HTTP token（RFC 9110 §5.6.2），標準方法與
// PROPFIND、PURGE 這類擴充方法皆可。先前只收 9 個標準方法，WebDAV 等方法無法註冊。
// 只收大寫（net/http 不會轉大寫，"get" 註冊了也永遠對不到）；Handle 已先轉成大寫
func isValidHTTPMethod(method string) bool {
	if method == "" {
		return false
	}
	for i := 0; i < len(method); i++ {
		c := method[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}
