// @chris
package router

import (
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"sort"
	"strings"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// Router 主結構
type Router struct {
	Group                             // 嵌入根路由組
	trees    map[string]*radixNode    // 每個 HTTP 方法一棵 Radix Tree
	globalMW []hypcontext.HandlerFunc // 全域中間件（獨立於 Group 的中間件）

	// 配置
	strictSlash            bool
	handleMethodNotAllowed bool
	methodOverride         bool

	// 404/405 處理器
	notFound         hypcontext.HandlerFunc
	methodNotAllowed hypcontext.HandlerFunc
}

// Param 路由參數。與 hypcontext.Param 為同一型別：radix tree 直接把參數
// search 進 Context 自帶的緩衝，免去「router 池 → 逐一複製成 Context 參數」
// 這一層（先前每個帶參數請求多一次配置與一次複製）
type Param = hypcontext.Param

// RouterOption 路由器選項
type RouterOption func(*Router)

// WithStrictSlash 設置嚴格斜線模式
func WithStrictSlash(enabled bool) RouterOption {
	return func(r *Router) {
		r.strictSlash = enabled
	}
}

// WithMethodNotAllowed 設置是否處理 405
func WithMethodNotAllowed(enabled bool) RouterOption {
	return func(r *Router) {
		r.handleMethodNotAllowed = enabled
	}
}

// New 創建新的路由器
// EX:
//
//	r := router.New()
//	r.GET("/ping", pingHandler)
//
//	api := r.NewGroup("/api/v1")
//	api.GET("/users", listUsers)
func New(opts ...RouterOption) *Router {
	r := &Router{
		trees:                  make(map[string]*radixNode),
		globalMW:               make([]hypcontext.HandlerFunc, 0),
		strictSlash:            false,
		handleMethodNotAllowed: true,
	}

	// 初始化嵌入的根 Group
	r.Group = Group{
		basePath:   "/",
		middleware: nil,
		router:     r,
		isRoot:     true,
	}

	// 套用選項
	for _, opt := range opts {
		opt(r)
	}

	return r
}

// addRoute 註冊路由到 Radix Tree
// 這是所有路由的最終入口，由 Group.handle() 調用。
// 路由須在開始服務前註冊完成：radix tree 不加鎖，服務中途註冊會與查找產生 data race
func (r *Router) addRoute(method, absolutePath string, handlers []hypcontext.HandlerFunc) {
	if absolutePath == "" {
		panic("router: path cannot be empty")
	}
	if absolutePath[0] != '/' {
		panic("router: path must begin with '/'")
	}
	if len(handlers) == 0 {
		panic("router: must provide at least one handler")
	}

	// 獲取或創建方法樹
	if r.trees[method] == nil {
		r.trees[method] = &radixNode{nType: root}
	}

	r.trees[method].addRoute(absolutePath, handlers)
}

// ServeHTTP 實現 http.Handler 介面
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c := hypcontext.New(w, req)
	defer c.Release()
	// 請求結束前強制 flush 已記錄的狀態碼（冪等；hijack 後為 no-op）。
	// responseWriter.WriteHeader 是惰性的，只有寫 body 才會真正送出；
	// c.Redirect 在非 GET（http.Redirect 不寫 body）時若無此行，
	// 底層從未收到 WriteHeader，Go 會自動補 200 → 瀏覽器拿到 200 + Location 不跳轉。
	// （defer LIFO：此行先於 c.Release() 執行，writer 尚未歸還池）
	defer c.Response.WriteHeaderNow()
	// 清理 multipart 暫存檔並回報未消費的請求錯誤（見各自函式說明）
	defer c.Finish()

	if r.methodOverride && req.Method == http.MethodPost {
		if m := overrideMethod(req); m != "" {
			req.Method = m
		}
	}
	urlPath := req.URL.Path
	method := req.Method

	// Alt-Svc 由 server.wrapHandler 依實際監聽位址設定（router 不寫入）。
	//
	// 不再有路由快取：radix tree 的靜態查找本身零分配，快取只多出 method+path 字串拼接
	// 與 sync.Map 成本（500 條路由實測：有快取 71ns / 1 alloc，無快取 56ns / 0 alloc）

	// Radix Tree 查找：參數直接 search 進 Context 自帶的緩衝（c.Params 經
	// reset 後為長度 0 的自帶緩衝），帶參數路由零分配
	if root := r.trees[method]; root != nil {
		if handlers, params := root.search(urlPath, c.Params[:0]); handlers != nil {
			c.Params = params
			r.executeHandlers(c, handlers)
			return
		}
	}

	// HEAD 自動回應：若無 HEAD handler，使用 GET handler
	if method == "HEAD" {
		if root := r.trees["GET"]; root != nil {
			if handlers, params := root.search(urlPath, c.Params[:0]); handlers != nil {
				c.Params = params
				r.executeHandlers(c, handlers)
				return
			}
		}
	}

	// 尾部斜線重導向（URL.Path 可能為空，例如 HTTP/2 CONNECT）
	if r.strictSlash && urlPath != "" {
		var tryPath string
		if urlPath[len(urlPath)-1] == '/' {
			tryPath = urlPath[:len(urlPath)-1]
		} else {
			tryPath = urlPath + "/"
		}
		if root := r.trees[method]; root != nil {
			if handlers, _ := root.search(tryPath, nil); handlers != nil {
				r.executeHandlers(c, []hypcontext.HandlerFunc{trailingSlashRedirect(method, tryPath, req.URL.RawQuery)})
				return
			}
		}
	}

	// 405 Method Not Allowed
	if r.handleMethodNotAllowed {
		var allow []string
		for m, tree := range r.trees {
			if m == method {
				continue
			}
			if handlers, _ := tree.search(urlPath, nil); handlers != nil {
				allow = append(allow, m)
			}
		}
		if len(allow) > 0 {
			// RFC 9110 §15.5.6：405 必須帶 Allow。有 GET 即可 HEAD（見上方 HEAD 自動回應）
			if containsString(allow, http.MethodGet) && !containsString(allow, http.MethodHead) {
				allow = append(allow, http.MethodHead)
			}
			sort.Strings(allow)
			c.Header("Allow", strings.Join(allow, ", "))
			// 走 executeHandlers 讓 405 也經過全域中間件：
			// 直接呼叫會讓 Recovery / Logger / CORS / Security 全部跳過，
			// 自訂 handler 內的 panic 直接穿透到 net/http（連線被斷而非 500）
			r.executeHandlers(c, []hypcontext.HandlerFunc{r.methodNotAllowedHandler()})
			return
		}
	}

	// 404 Not Found（同樣經過全域中間件）
	r.executeHandlers(c, []hypcontext.HandlerFunc{r.notFoundHandler()})
}

// EnableMethodOverride 讓 POST 請求以 X-HTTP-Method-Override 標頭或查詢參數 _method
// 改用 PUT / PATCH / DELETE 路由（HTML 表單只能送 GET / POST，方法寫在 action：
// <form method="post" action="/users/1?_method=DELETE">）。
// 在路由比對前改寫方法，所以必須在 router 層做：先前的 middleware.MethodOverride 在比對之後
// 才執行，POST 永遠只對到 POST 路由。只允許改成這三種方法，避免改成 GET / CONNECT 等繞過檢查
func (r *Router) EnableMethodOverride() {
	r.methodOverride = true
}

// overrideMethod 回傳覆寫後的方法；不合法或未指定時回空字串。
// 不讀 body：先前讀表單欄位 _method，在路由比對前（也就在 BodyLimit 之前）把整個 body
// 讀完並解析，chunked 傳送的 multipart 請求可完全繞過 BodyLimit，超出記憶體上限的部分落地成暫存檔
func overrideMethod(req *http.Request) string {
	m := req.Header.Get("X-HTTP-Method-Override")
	if m == "" {
		m = req.URL.Query().Get("_method")
	}
	switch m = strings.ToUpper(strings.TrimSpace(m)); m {
	case http.MethodPut, http.MethodPatch, http.MethodDelete:
		return m
	}
	return ""
}

// trailingSlashRedirect 回傳尾斜線重導向的 handler。經 executeHandlers 執行，全域中間件
// （CORS / Security…）照常套用；以 c.Redirect 寫入 c.Writer，不會與 router 收尾的
// WriteHeaderNow 重複寫標頭。先前直接 http.Redirect 到底層 writer：中間件全被跳過、
// 每次重導向 server log 多兩行 superfluous WriteHeader；且一律 301 又丟掉 query string
func trailingSlashRedirect(method, path, rawQuery string) hypcontext.HandlerFunc {
	code := http.StatusMovedPermanently
	if method != http.MethodGet && method != http.MethodHead {
		code = http.StatusPermanentRedirect // 308：保留方法與 body；301 會讓瀏覽器把 POST 改成 GET
	}
	location := (&url.URL{Path: path, RawQuery: rawQuery}).RequestURI()
	return func(c *hypcontext.Context) { c.Redirect(code, location) }
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// notFoundHandler 回傳 404 處理器（自訂或預設）
func (r *Router) notFoundHandler() hypcontext.HandlerFunc {
	if r.notFound != nil {
		return r.notFound
	}
	return func(c *hypcontext.Context) {
		c.Status(http.StatusNotFound)
		c.Writer.WriteHeaderNow()
	}
}

// methodNotAllowedHandler 回傳 405 處理器（自訂或預設）
func (r *Router) methodNotAllowedHandler() hypcontext.HandlerFunc {
	if r.methodNotAllowed != nil {
		return r.methodNotAllowed
	}
	return func(c *hypcontext.Context) {
		c.Status(http.StatusMethodNotAllowed)
		c.Writer.WriteHeaderNow()
	}
}

// Use 添加全域中間件，全域中間件在 executeHandlers 中優先於 Group 中間件執行
//
// EX：
//
//	r := router.New()
//	r.Use(loggerMiddleware, recoveryMiddleware)
func (r *Router) Use(middleware ...hypcontext.HandlerFunc) {
	r.globalMW = append(r.globalMW, middleware...)
}

// executeHandlers 執行處理器鏈（gin 式洋蔥模型）
// 順序：全域中間件 → (Group 中間件 + 路由 Handler)，合併進 c.handlers 後由 c.Next() 驅動。
//
// v0.8.11 前為攤平循序執行、c.handlers 從未填入——中間件內的 c.Next() 是 no-op，
// 導致「包裹式」中間件全數失效：Recovery 的 defer/recover 接不到 handler 的 panic、
// Logger 在 handler 執行前就記完 log（耗時恆 0、狀態碼恆 200）、Compression 在
// handler 寫入前就 defer Close 掉 gzip writer。改為 c.Next() 驅動後，中間件的
// c.Next() 會先執行鏈上其餘 handler 再返回，真正包住下游；c.Abort* 中止鏈。
func (r *Router) executeHandlers(c *hypcontext.Context, handlers []hypcontext.HandlerFunc) {
	c.SetHandlers(r.globalMW, handlers)
	c.Next()
}

// NotFound 設置 404 處理器
func (r *Router) NotFound(handler hypcontext.HandlerFunc) {
	r.notFound = handler
}

// MethodNotAllowed 設置 405 處理器
func (r *Router) MethodNotAllowed(handler hypcontext.HandlerFunc) {
	r.methodNotAllowed = handler
	r.handleMethodNotAllowed = true
}

// RouteInfo 路由信息
type RouteInfo struct {
	Method       string
	Path         string
	Handlers     int      // handler 數量
	HandlerNames []string // handler 函式名稱
}

// Routes 返回已註冊的所有路由信息
func (r *Router) Routes() []RouteInfo {
	routes := make([]RouteInfo, 0)
	for method, root := range r.trees {
		routes = collectRoutes("", method, routes, root)
	}
	return routes
}

// collectRoutes 遞歸收集路由信息
func collectRoutes(prefix, method string, routes []RouteInfo, n *radixNode) []RouteInfo {
	if n == nil {
		return routes
	}
	fullPath := prefix + n.path
	if len(n.handlers) > 0 {
		names := make([]string, len(n.handlers))
		for i, h := range n.handlers {
			names[i] = nameOfFunction(h)
		}
		routes = append(routes, RouteInfo{
			Method:       method,
			Path:         fullPath,
			Handlers:     len(n.handlers),
			HandlerNames: names,
		})
	}
	for _, child := range n.children {
		routes = collectRoutes(fullPath, method, routes, child)
	}
	return routes
}

// Schema 開始 schema-first 路由註冊
//
// 使用範例：
//
//	r.Schema(schema.Route{
//	    Method:  "POST",
//	    Path:    "/api/users",
//	    Summary: "建立使用者",
//	    Input:   CreateUserRequest{},
//	    Output:  UserResponse{},
//	}).Handle(createUserHandler)
func (r *Router) Schema(route schema.Route) *schema.SchemaRoute {
	return schema.NewSchemaRoute(route, r)
}

// LoadHTMLGlob 以 glob 模式載入 HTML 樣板集，供 c.HTML 以樣板名渲染
// 範例：r.LoadHTMLGlob("app/views/*.html")
func (r *Router) LoadHTMLGlob(pattern string) error {
	t, err := template.ParseGlob(pattern)
	if err != nil {
		return err
	}
	hypcontext.SetHTMLTemplate(t)
	return nil
}

// SetHTMLTemplate 直接設定 c.HTML 使用的已解析樣板集
func (r *Router) SetHTMLTemplate(t *template.Template) {
	hypcontext.SetHTMLTemplate(t)
}

// RegisterSchema 實作 schema.SchemaRegistrar 介面（見 schemaHandlers）
func (r *Router) RegisterSchema(route schema.Route, handlers ...hypcontext.HandlerFunc) {
	r.Group.handle(route.Method, route.Path, schemaHandlers(&route, handlers))
	schema.Global().Register(route)
}

// schemaHandlers 為 schema 路由準備 handler 鏈，Router 與 Group 共用：
//   - 以原始 handlers 填入 route.HandlerNames，供 contract.Observe() 過濾
//   - 宣告了 Input 型別時前置一個標記，於每次請求將該型別寫入 Context，
//     供 c.BindInput() 在執行期對齊「實作綁定型別」與「Schema 宣告型別」
//
// 先前 Group 版本是另一份實作、漏了標記：group 底下的路由綁錯 struct 完全不會被回報
func schemaHandlers(route *schema.Route, handlers []hypcontext.HandlerFunc) []hypcontext.HandlerFunc {
	if len(route.HandlerNames) == 0 && len(handlers) > 0 {
		names := make([]string, len(handlers))
		for i, h := range handlers {
			names[i] = nameOfFunction(h)
		}
		route.HandlerNames = names
	}
	if route.Input == nil {
		return handlers
	}
	key, input := route.RouteKey(), route.Input
	marker := func(c *hypcontext.Context) { c.SetSchemaInput(key, input) }
	return append([]hypcontext.HandlerFunc{marker}, handlers...)
}

// nameOfFunction 透過反射取得函式名稱（短名稱）
func nameOfFunction(f interface{}) string {
	full := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	// 取最後一段（移除 package path）
	if idx := strings.LastIndex(full, "/"); idx >= 0 {
		full = full[idx+1:]
	}
	// 移除 -fm 後綴（method value）
	full = strings.TrimSuffix(full, "-fm")
	return full
}

// 共用工具函數

// joinPaths 連接路徑
func joinPaths(base, relative string) string {
	if relative == "" {
		return base
	}

	finalPath := base
	if base[len(base)-1] != '/' && relative[0] != '/' {
		finalPath += "/"
	} else if base[len(base)-1] == '/' && relative[0] == '/' {
		relative = relative[1:]
	}
	finalPath += relative

	return finalPath
}

// longestCommonPrefix 最長公共前綴。
// 直接對 string 取索引：先前以 unsafe 把 string header 當 []byte header 讀，cap 讀到
// header 之外的記憶體（未定義行為），而且並沒有比取索引快
func longestCommonPrefix(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// findWildcard 查找路徑中的通配符
func findWildcard(path string) (string, int, bool) {
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c != ':' && c != '*' {
			continue
		}
		valid := true
		for j := i + 1; j < len(path); j++ {
			switch path[j] {
			case '/':
				return path[i:j], i, valid
			case ':', '*':
				valid = false
			}
		}
		return path[i:], i, valid
	}
	return "", -1, false
}
