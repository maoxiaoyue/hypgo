// Package router 提供高效能的 HTTP/3 優化路由系統
package router

import (
	//"fmt"
	"net/http"
	"path"
	"regexp"
	//"strings"
	"sync"

	"github.com/maoxiaoyue/hypgo/pkg/context"
)

// Router 是 HypGo 的核心路由器
type Router struct {
	// 路由樹（基於 Radix Tree）
	trees map[string]*node

	// 路由組
	RouterGroup

	// 全域中間件
	middleware []context.HandlerFunc

	// 404 處理器
	notFoundHandler context.HandlerFunc

	// 405 處理器
	methodNotAllowedHandler context.HandlerFunc

	// HTTP/3 特定配置
	http3Config *HTTP3Config

	// 路由快取（用於高頻路由）
	routeCache *sync.Map

	// 效能監控
	metrics *RouterMetrics

	// 最大參數數量
	maxParams int

	// 路由池（物件重用）
	contextPool sync.Pool

	// 正則路由快取
	regexCache map[string]*regexp.Regexp
	regexMu    sync.RWMutex
}

// RouterGroup 路由組
type RouterGroup struct {
	Handlers []context.HandlerFunc
	basePath string
	router   *Router
	root     bool
}

// HTTP3Config HTTP/3 特定配置
type HTTP3Config struct {
	// 啟用 0-RTT
	Enable0RTT bool

	// 最大並發流
	MaxConcurrentStreams int

	// 初始流控制窗口
	InitialStreamWindow uint32

	// 初始連接窗口
	InitialConnectionWindow uint32

	// Keep-Alive 間隔
	KeepAliveInterval int

	// 最大空閒超時
	MaxIdleTimeout int

	// 啟用資料報
	EnableDatagrams bool
}

// RouterMetrics 路由器指標
type RouterMetrics struct {
	TotalRequests  uint64
	TotalHits      uint64
	TotalMisses    uint64
	CacheHitRate   float64
	AvgRoutingTime float64
	HTTP3Requests  uint64
	HTTP2Requests  uint64
	HTTP1Requests  uint64
}

// node 路由樹節點
type node struct {
	path      string
	indices   string
	wildChild bool
	nType     nodeType
	priority  uint32
	children  []*node
	handlers  []context.HandlerFunc
	fullPath  string

	// HTTP/3 優化：流優先級
	streamPriority uint8

	// 路由參數名稱
	paramNames []string
}

type nodeType uint8

const (
	static nodeType = iota
	root
	param
	catchAll
)

// ===== Router 核心方法 =====

// New 創建新的路由器
func New() *Router {
	router := &Router{
		trees:      make(map[string]*node),
		routeCache: &sync.Map{},
		regexCache: make(map[string]*regexp.Regexp),
		http3Config: &HTTP3Config{
			Enable0RTT:              true,
			MaxConcurrentStreams:    100,
			InitialStreamWindow:     1 << 20, // 1MB
			InitialConnectionWindow: 1 << 21, // 2MB
			KeepAliveInterval:       30,
			MaxIdleTimeout:          120,
			EnableDatagrams:         false,
		},
		metrics: &RouterMetrics{},
	}

	router.RouterGroup = RouterGroup{
		basePath: "/",
		router:   router,
		root:     true,
	}

	// 初始化 context 池
	router.contextPool.New = func() interface{} {
		return &context.Context{
			Params: make(context.Params, 0, router.maxParams),
			Keys:   make(map[string]interface{}),
		}
	}

	return router
}

// ServeHTTP 實現 http.Handler 介面
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 從池中獲取 context
	c := r.contextPool.Get().(*context.Context)
	c.Reset(w, req)

	// 設置全域中間件
	c.SetHandlers(r.middleware)

	// 路由匹配
	r.handleRequest(c)

	// 記錄指標
	r.recordMetrics(c)

	// 歸還 context 到池
	r.contextPool.Put(c)
}

// handleRequest 處理請求路由
func (r *Router) handleRequest(c *context.Context) {
	httpMethod := c.Request.Method
	path := c.Request.URL.Path

	// 嘗試從快取獲取
	cacheKey := httpMethod + path
	if cached, ok := r.routeCache.Load(cacheKey); ok {
		if route, ok := cached.(*cachedRoute); ok {
			c.Params = route.params
			c.SetHandlers(append(r.middleware, route.handlers...))
			c.Next()
			return
		}
	}

	// 查找路由樹
	if root := r.trees[httpMethod]; root != nil {
		if handlers, params, _ := root.getValue(path, c.Params); handlers != nil {
			c.Params = params
			c.SetHandlers(append(r.middleware, handlers...))
			c.Next()

			// 快取熱門路由
			r.cacheRoute(cacheKey, handlers, params)
			return
		}
	}

	// 處理 404
	if r.notFoundHandler != nil {
		c.SetHandlers([]context.HandlerFunc{r.notFoundHandler})
		c.Next()
	} else {
		c.String(http.StatusNotFound, "404 Not Found")
	}
}

// ===== HTTP 方法路由 =====

// GET 註冊 GET 路由
func (group *RouterGroup) GET(path string, handlers ...context.HandlerFunc) {
	group.handle(http.MethodGet, path, handlers)
}

// POST 註冊 POST 路由
func (group *RouterGroup) POST(path string, handlers ...context.HandlerFunc) {
	group.handle(http.MethodPost, path, handlers)
}

// PUT 註冊 PUT 路由
func (group *RouterGroup) PUT(path string, handlers ...context.HandlerFunc) {
	group.handle(http.MethodPut, path, handlers)
}

// DELETE 註冊 DELETE 路由
func (group *RouterGroup) DELETE(path string, handlers ...context.HandlerFunc) {
	group.handle(http.MethodDelete, path, handlers)
}

// PATCH 註冊 PATCH 路由
func (group *RouterGroup) PATCH(path string, handlers ...context.HandlerFunc) {
	group.handle(http.MethodPatch, path, handlers)
}

// HEAD 註冊 HEAD 路由
func (group *RouterGroup) HEAD(path string, handlers ...context.HandlerFunc) {
	group.handle(http.MethodHead, path, handlers)
}

// OPTIONS 註冊 OPTIONS 路由
func (group *RouterGroup) OPTIONS(path string, handlers ...context.HandlerFunc) {
	group.handle(http.MethodOptions, path, handlers)
}

// Any 註冊所有 HTTP 方法的路由
func (group *RouterGroup) Any(path string, handlers ...context.HandlerFunc) {
	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodDelete, http.MethodPatch, http.MethodHead,
		http.MethodOptions,
	}
	for _, method := range methods {
		group.handle(method, path, handlers)
	}
}

// handle 處理路由註冊
func (group *RouterGroup) handle(httpMethod, relativePath string, handlers []context.HandlerFunc) {
	absolutePath := group.calculateAbsolutePath(relativePath)
	handlers = group.combineHandlers(handlers)
	group.router.addRoute(httpMethod, absolutePath, handlers)

	// 更新最大參數數量
	if paramsCount := countParams(absolutePath); paramsCount > group.router.maxParams {
		group.router.maxParams = paramsCount
	}
}

// ===== 路由組管理 =====

// Group 創建新的路由組
func (group *RouterGroup) Group(relativePath string, handlers ...context.HandlerFunc) *RouterGroup {
	return &RouterGroup{
		Handlers: group.combineHandlers(handlers),
		basePath: group.calculateAbsolutePath(relativePath),
		router:   group.router,
	}
}

// Use 添加中間件
func (group *RouterGroup) Use(middleware ...context.HandlerFunc) {
	group.Handlers = append(group.Handlers, middleware...)
}

// calculateAbsolutePath 計算絕對路徑
func (group *RouterGroup) calculateAbsolutePath(relativePath string) string {
	if relativePath == "" {
		return group.basePath
	}

	finalPath := path.Join(group.basePath, relativePath)
	appendSlash := lastChar(relativePath) == '/' && lastChar(finalPath) != '/'
	if appendSlash {
		return finalPath + "/"
	}
	return finalPath
}

// combineHandlers 合併處理器
func (group *RouterGroup) combineHandlers(handlers []context.HandlerFunc) []context.HandlerFunc {
	finalSize := len(group.Handlers) + len(handlers)
	mergedHandlers := make([]context.HandlerFunc, finalSize)
	copy(mergedHandlers, group.Handlers)
	copy(mergedHandlers[len(group.Handlers):], handlers)
	return mergedHandlers
}

// ===== 靜態檔案服務 =====

// Static 註冊靜態檔案路由
func (group *RouterGroup) Static(relativePath, root string) {
	handler := group.createStaticHandler(relativePath, http.Dir(root))
	urlPattern := path.Join(relativePath, "/*filepath")
	group.GET(urlPattern, handler)
	group.HEAD(urlPattern, handler)
}

// StaticFile 註冊單個檔案路由
func (group *RouterGroup) StaticFile(relativePath, filepath string) {
	handler := func(c *context.Context) {
		c.File(filepath)
	}
	group.GET(relativePath, handler)
	group.HEAD(relativePath, handler)
}

// StaticFS 註冊檔案系統路由
func (group *RouterGroup) StaticFS(relativePath string, fs http.FileSystem) {
	handler := group.createStaticHandler(relativePath, fs)
	urlPattern := path.Join(relativePath, "/*filepath")
	group.GET(urlPattern, handler)
	group.HEAD(urlPattern, handler)
}

// createStaticHandler 創建靜態檔案處理器
func (group *RouterGroup) createStaticHandler(relativePath string, fs http.FileSystem) context.HandlerFunc {
	absolutePath := group.calculateAbsolutePath(relativePath)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(fs))

	return func(c *context.Context) {
		file := c.Param("filepath")

		// 檢查檔案是否存在
		if _, err := fs.Open(file); err != nil {
			c.Status(http.StatusNotFound)
			return
		}

		fileServer.ServeHTTP(c.Response, c.Request)
	}
}

// ===== HTTP/3 特定功能 =====

// EnableHTTP3 啟用 HTTP/3 支援
func (r *Router) EnableHTTP3(config *HTTP3Config) {
	if config != nil {
		r.http3Config = config
	}
}

// SetStreamPriority 設置路由的流優先級
func (group *RouterGroup) SetStreamPriority(path string, priority uint8) {
	// 實現流優先級設置
	// 這會影響 HTTP/3 中的流調度
}

// EnableServerPush 為特定路由啟用 Server Push
func (group *RouterGroup) EnableServerPush(path string, resources []string) {
	handler := func(c *context.Context) {
		// 如果是 HTTP/2 或 HTTP/3，推送資源
		if c.Protocol >= context.HTTP2 {
			for _, resource := range resources {
				c.Push(resource, nil)
			}
		}
		c.Next()
	}

	group.Use(handler)
}

// ===== WebSocket 支援 =====

// WebSocket 註冊 WebSocket 路由
func (group *RouterGroup) WebSocket(path string, handler WebSocketHandler) {
	group.GET(path, func(c *context.Context) {
		if !c.IsWebsocket() {
			c.Status(http.StatusBadRequest)
			return
		}

		handler.Handle(c)
	})
}

// WebSocketHandler WebSocket 處理器介面
type WebSocketHandler interface {
	Handle(c *context.Context)
}

// ===== 路由樹操作 =====

// addRoute 添加路由到樹
func (r *Router) addRoute(method, path string, handlers []context.HandlerFunc) {
	if path[0] != '/' {
		panic("path must begin with '/'")
	}

	if method == "" {
		panic("HTTP method can not be empty")
	}

	if len(handlers) == 0 {
		panic("there must be at least one handler")
	}

	root := r.trees[method]
	if root == nil {
		root = new(node)
		r.trees[method] = root
	}

	root.addRoute(path, handlers)
}

// addRoute 向節點添加路由
func (n *node) addRoute(path string, handlers []context.HandlerFunc) {
	fullPath := path
	n.priority++

	// 空樹
	if len(n.path) == 0 && len(n.children) == 0 {
		n.insertChild(path, fullPath, handlers)
		n.nType = root
		return
	}

	// 查找最長公共前綴
	i := longestCommonPrefix(path, n.path)

	// 分割節點
	if i < len(n.path) {
		child := node{
			path:      n.path[i:],
			wildChild: n.wildChild,
			nType:     static,
			indices:   n.indices,
			children:  n.children,
			handlers:  n.handlers,
			priority:  n.priority - 1,
			fullPath:  n.fullPath,
		}

		n.children = []*node{&child}
		n.indices = string([]byte{n.path[i]})
		n.path = path[:i]
		n.handlers = nil
		n.wildChild = false
		n.fullPath = fullPath[:i]
	}

	// 為新路由創建子節點
	if i < len(path) {
		path = path[i:]

		if n.wildChild {
			n = n.children[0]
			n.priority++

			// 檢查通配符匹配
			if len(path) >= len(n.path) && n.path == path[:len(n.path)] &&
				n.nType != catchAll &&
				(len(n.path) >= len(path) || path[len(n.path)] == '/') {
				n.addRoute(path, handlers)
				return
			}

			panic("path segment '" + path +
				"' conflicts with existing wildcard '" + n.path +
				"' in path '" + fullPath + "'")
		}

		idxc := path[0]

		// 參數節點
		if n.nType == param && idxc == '/' && len(n.children) == 1 {
			n = n.children[0]
			n.priority++
			n.addRoute(path, handlers)
			return
		}

		// 檢查現有子節點
		for i, c := range n.indices {
			if c == idxc {
				i = n.incrementChildPrio(i)
				n = n.children[i]
				n.addRoute(path, handlers)
				return
			}
		}

		// 插入新節點
		if idxc != ':' && idxc != '*' {
			n.indices += string([]byte{idxc})
			child := &node{
				fullPath: fullPath,
			}
			n.children = append(n.children, child)
			n.incrementChildPrio(len(n.indices) - 1)
			n = child
		}

		n.insertChild(path, fullPath, handlers)
		return
	}

	// 設置處理器
	if n.handlers != nil {
		panic("handlers are already registered for path '" + fullPath + "'")
	}
	n.handlers = handlers
	n.fullPath = fullPath
}

// insertChild 插入子節點
func (n *node) insertChild(path, fullPath string, handlers []context.HandlerFunc) {
	for {
		// 查找參數或通配符
		wildcard, i, valid := findWildcard(path)
		if i < 0 {
			break
		}

		if !valid {
			panic("only one wildcard per path segment is allowed")
		}

		if len(wildcard) < 2 {
			panic("wildcards must be named with a non-empty name")
		}

		// 參數
		if wildcard[0] == ':' {
			if i > 0 {
				n.path = path[:i]
				path = path[i:]
			}

			child := &node{
				nType:    param,
				path:     wildcard,
				fullPath: fullPath,
			}
			n.addChild(child)
			n.wildChild = true
			n = child

			// 如果路徑還有剩餘部分
			if len(wildcard) < len(path) {
				path = path[len(wildcard):]

				child := &node{
					priority: 1,
					fullPath: fullPath,
				}
				n.addChild(child)
				n = child
				continue
			}

			// 設置處理器
			n.handlers = handlers
			return
		}

		// catchAll
		if i+len(wildcard) != len(path) {
			panic("catch-all routes are only allowed at the end of the path")
		}

		if len(n.path) > 0 && n.path[len(n.path)-1] == '/' {
			panic("catch-all conflicts with existing handle for the path segment")
		}

		// 創建 catchAll 節點
		i--
		if path[i] != '/' {
			panic("no / before catch-all")
		}

		n.path = path[:i]

		child := &node{
			wildChild: true,
			nType:     catchAll,
			fullPath:  fullPath,
		}

		n.addChild(child)
		n.indices = string('/')
		n = child

		child = &node{
			path:     path[i:],
			nType:    catchAll,
			handlers: handlers,
			fullPath: fullPath,
		}
		n.children = []*node{child}

		return
	}

	// 插入剩餘路徑
	n.path = path
	n.handlers = handlers
	n.fullPath = fullPath
}

// ===== 輔助函數 =====

// countParams 計算路徑中的參數數量
func countParams(path string) int {
	var n int
	for i := range path {
		switch path[i] {
		case ':', '*':
			n++
		}
	}
	return n
}

// lastChar 獲取字串最後一個字元
func lastChar(str string) uint8 {
	if str == "" {
		panic("empty string")
	}
	return str[len(str)-1]
}

// longestCommonPrefix 查找最長公共前綴
func longestCommonPrefix(a, b string) int {
	i := 0
	max := min(len(a), len(b))
	for i < max && a[i] == b[i] {
		i++
	}
	return i
}

// findWildcard 查找通配符
func findWildcard(path string) (string, int, bool) {
	for start, c := range []byte(path) {
		if c != ':' && c != '*' {
			continue
		}

		valid := true
		for end, c := range []byte(path[start+1:]) {
			switch c {
			case '/':
				return path[start : start+1+end], start, valid
			case ':', '*':
				valid = false
			}
		}
		return path[start:], start, valid
	}
	return "", -1, false
}

// min 返回較小值
func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

// ===== 路由快取 =====

type cachedRoute struct {
	handlers []context.HandlerFunc
	params   context.Params
}

// cacheRoute 快取路由
func (r *Router) cacheRoute(key string, handlers []context.HandlerFunc, params context.Params) {
	r.routeCache.Store(key, &cachedRoute{
		handlers: handlers,
		params:   params,
	})
}

// recordMetrics 記錄指標
func (r *Router) recordMetrics(c *context.Context) {
	r.metrics.TotalRequests++

	switch c.Protocol {
	case context.HTTP3:
		r.metrics.HTTP3Requests++
	case context.HTTP2:
		r.metrics.HTTP2Requests++
	default:
		r.metrics.HTTP1Requests++
	}
}
