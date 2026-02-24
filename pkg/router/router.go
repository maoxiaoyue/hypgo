package router

import (
	"net/http"
	"sync"
	"unsafe"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// Router 主結構
type Router struct {
	Group                              // 嵌入根路由組
	trees     map[string]*radixNode    // 每個 HTTP 方法一棵 Radix Tree
	cache     *routeCache              // LRU 路由快取
	paramPool *sync.Pool               // 參數對象池
	globalMW  []hypcontext.HandlerFunc // 全域中間件（獨立於 Group 的中間件）

	// 配置
	maxParams              int
	enableCache            bool
	cacheSize              int
	caseSensitive          bool
	strictSlash            bool
	handleMethodNotAllowed bool

	// HTTP/3 配置
	http3Config *HTTP3Config

	// 404/405 處理器
	notFound         hypcontext.HandlerFunc
	methodNotAllowed hypcontext.HandlerFunc
}

// HTTP3Config HTTP/3 配置
type HTTP3Config struct {
	Enabled            bool
	MaxHeaderBytes     int
	EnableDatagrams    bool
	EnableWebtransport bool
	MaxBidiStreams     int64
	MaxUniStreams      int64
}

// Param 路由參數
type Param struct {
	Key   string
	Value string
}

// RouterOption 路由器選項
type RouterOption func(*Router)

// WithCache 設置快取大小
func WithCache(size int) RouterOption {
	return func(r *Router) {
		r.enableCache = true
		r.cacheSize = size
		r.cache = newRouteCache(size)
	}
}

// WithMaxParams 設置最大參數數
func WithMaxParams(n int) RouterOption {
	return func(r *Router) {
		r.maxParams = n
	}
}

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
		cache:                  newRouteCache(1000),
		globalMW:               make([]hypcontext.HandlerFunc, 0),
		maxParams:              10,
		enableCache:            true,
		cacheSize:              1000,
		caseSensitive:          false,
		strictSlash:            false,
		handleMethodNotAllowed: true,
		http3Config:            nil,
		paramPool: &sync.Pool{
			New: func() interface{} {
				return make([]Param, 0, 10)
			},
		},
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
// 這是所有路由的最終入口，由 Group.handle() 調用
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

	root := r.trees[method]
	root.addRoute(absolutePath, handlers)

	// 更新最大參數數
	if pc := countParams(absolutePath); pc > r.maxParams {
		r.maxParams = pc
	}
}

// ServeHTTP 實現 http.Handler 介面
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c := hypcontext.New(w, req)
	defer c.Release()

	urlPath := req.URL.Path
	method := req.Method

	// HTTP/3 Alt-Svc 標頭
	if r.http3Config != nil && r.http3Config.Enabled {
		w.Header().Set("Alt-Svc", `h3=":443"; ma=2592000`)
	}

	// 快取查找
	if r.enableCache {
		cacheKey := method + urlPath
		if entry := r.cache.get(cacheKey); entry != nil {
			c.Params = r.makeContextParams(entry.params)
			r.executeHandlers(c, entry.handlers)
			return
		}
	}

	// Radix Tree 查找
	if root := r.trees[method]; root != nil {
		handlers, params := root.search(urlPath, r.getParams())
		if handlers != nil {
			c.Params = r.makeContextParams(params)

			// 只快取靜態路由（無參數）
			if r.enableCache && len(params) == 0 {
				r.cache.put(method+urlPath, handlers, params)
			}

			r.executeHandlers(c, handlers)
			r.putParams(params)
			return
		}
		r.putParams(params)
	}

	// 405 Method Not Allowed
	if r.handleMethodNotAllowed {
		for m, tree := range r.trees {
			if m == method {
				continue
			}
			if handlers, _ := tree.search(urlPath, nil); handlers != nil {
				if r.methodNotAllowed != nil {
					r.methodNotAllowed(c)
				} else {
					c.Status(http.StatusMethodNotAllowed)
				}
				return
			}
		}
	}

	// 404 Not Found
	if r.notFound != nil {
		r.notFound(c)
	} else {
		c.Status(http.StatusNotFound)
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

// executeHandlers 執行處理器鏈
// 順序：全域中間件 → (Group 中間件 + 路由 Handler)，其中 Group 中間件已在 Group.handle() 中與 Handler 合併
func (r *Router) executeHandlers(c *hypcontext.Context, handlers []hypcontext.HandlerFunc) {
	// 1. 全域中間件
	for _, h := range r.globalMW {
		h(c)
		if c.Response.Written() {
			return
		}
	}

	// 2. Group 中間件 + 路由 Handler（已合併為一個 slice）
	for _, h := range handlers {
		h(c)
		if c.Response.Written() {
			return
		}
	}
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

// EnableHTTP3 啟用 HTTP/3 支援
func (r *Router) EnableHTTP3(config *HTTP3Config) {
	if config == nil {
		config = &HTTP3Config{
			Enabled:            true,
			MaxHeaderBytes:     1 << 20, // 1MB
			EnableDatagrams:    false,
			EnableWebtransport: false,
			MaxBidiStreams:     100,
			MaxUniStreams:      100,
		}
	}
	config.Enabled = true
	r.http3Config = config
}

// IsHTTP3Enabled 檢查是否啟用了 HTTP/3
func (r *Router) IsHTTP3Enabled() bool {
	return r.http3Config != nil && r.http3Config.Enabled
}

// GetHTTP3Config 獲取 HTTP/3 配置
func (r *Router) GetHTTP3Config() *HTTP3Config {
	return r.http3Config
}

// getParams 從池中獲取參數切片，參數池 & 轉換
func (r *Router) getParams() []Param {
	ps := r.paramPool.Get().([]Param)
	return ps[:0]
}

// putParams 返回參數切片到池
func (r *Router) putParams(params []Param) {
	if params != nil {
		r.paramPool.Put(params[:0])
	}
}

// makeContextParams 轉換路由參數為 Context 參數格式
func (r *Router) makeContextParams(params []Param) hypcontext.Params {
	if len(params) == 0 {
		return nil
	}
	cp := make(hypcontext.Params, len(params))
	for i, p := range params {
		cp[i] = hypcontext.Param{
			Key:   p.Key,
			Value: p.Value,
		}
	}
	return cp
}

// RouteInfo 路由信息
type RouteInfo struct {
	Method   string
	Path     string
	Handlers int // handler 數量
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
		routes = append(routes, RouteInfo{
			Method:   method,
			Path:     fullPath,
			Handlers: len(n.handlers),
		})
	}
	for _, child := range n.children {
		routes = collectRoutes(fullPath, method, routes, child)
	}
	return routes
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

// longestCommonPrefix 最長公共前綴（使用 unsafe 加速）
func longestCommonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	if max > 0 {
		ap := *(*[]byte)(unsafe.Pointer(&a))
		bp := *(*[]byte)(unsafe.Pointer(&b))
		for i := 0; i < max; i++ {
			if ap[i] != bp[i] {
				return i
			}
		}
	}
	return max
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

// countParams 計算路徑中的參數數量
func countParams(path string) int {
	n := 0
	for i := 0; i < len(path); i++ {
		if path[i] == ':' || path[i] == '*' {
			n++
		}
	}
	return n
}
