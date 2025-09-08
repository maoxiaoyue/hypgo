package router

import (
	"net/http"
	"sync"
	"unsafe"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// OptimizedRouter 高效能路由器
type OptimizedRouter struct {
	trees      map[string]*radixNode // 每個 HTTP 方法一棵樹
	cache      *routeCache           // 路由快取
	pool       *sync.Pool            // 參數池
	middleware []hypcontext.HandlerFunc

	// 配置
	maxParams              int
	enableCache            bool
	cacheSize              int
	caseSensitive          bool
	strictSlash            bool
	handleMethodNotAllowed bool

	// 404/405 處理器
	notFound         hypcontext.HandlerFunc
	methodNotAllowed hypcontext.HandlerFunc
}

/* //更完整的功能
type HybridRouter struct {
	core *OptimizedRouter

	websocket WebSocketSupport
	static    StaticFileSupport
	http3     HTTP3Config
}*/

// radixNode Radix Tree 節點
type radixNode struct {
	path      string
	indices   string       // 子節點的第一個字元索引
	wildChild bool         // 是否有通配符子節點
	nType     nodeType     // 節點類型
	priority  uint32       // 優先級（命中次數）
	children  []*radixNode // 子節點
	handlers  []hypcontext.HandlerFunc
	fullPath  string
}

type nodeType uint8

const (
	static   nodeType = iota // 靜態節點
	root                     // 根節點
	param                    // 參數節點 :id
	catchAll                 // 捕獲所有 *filepath
)

// routeCache 路由快取（LRU）
type routeCache struct {
	mu       sync.RWMutex
	cache    map[string]*cacheEntry
	head     *cacheEntry
	tail     *cacheEntry
	capacity int
	size     int
}

type cacheEntry struct {
	key      string
	handlers []hypcontext.HandlerFunc
	params   []Param
	prev     *cacheEntry
	next     *cacheEntry
}

// Param 路由參數
type Param struct {
	Key   string
	Value string
}

// NewOptimized 創建優化的路由器
func NewOptimized(opts ...RouterOption) *OptimizedRouter {
	r := &OptimizedRouter{
		trees:                  make(map[string]*radixNode),
		maxParams:              16,
		enableCache:            true,
		cacheSize:              1000,
		caseSensitive:          true,
		strictSlash:            false,
		handleMethodNotAllowed: false,
	}

	// 應用選項
	for _, opt := range opts {
		opt(r)
	}

	// 初始化快取
	if r.enableCache {
		r.cache = newRouteCache(r.cacheSize)
	}

	// 初始化參數池
	r.pool = &sync.Pool{
		New: func() interface{} {
			return make([]Param, 0, r.maxParams)
		},
	}

	return r
}

// RouterOption 路由器選項
type RouterOption func(*OptimizedRouter)

// WithCache 設置快取
func WithCache(size int) RouterOption {
	return func(r *OptimizedRouter) {
		r.enableCache = true
		r.cacheSize = size
	}
}

// WithMaxParams 設置最大參數數
func WithMaxParams(n int) RouterOption {
	return func(r *OptimizedRouter) {
		r.maxParams = n
	}
}

// ===== 路由註冊 =====

// Handle 註冊路由
func (r *OptimizedRouter) Handle(method, path string, handlers ...hypcontext.HandlerFunc) {
	if path == "" {
		panic("path cannot be empty")
	}
	if path[0] != '/' {
		panic("path must begin with '/'")
	}
	if len(handlers) == 0 {
		panic("must provide at least one handler")
	}

	// 獲取或創建方法樹
	if r.trees[method] == nil {
		r.trees[method] = &radixNode{nType: root}
	}

	root := r.trees[method]
	root.addRoute(path, handlers)

	// 更新最大參數數
	if pc := countParams(path); pc > r.maxParams {
		r.maxParams = pc
	}
}

// GET 註冊 GET 路由
func (r *OptimizedRouter) GET(path string, handlers ...hypcontext.HandlerFunc) {
	r.Handle(http.MethodGet, path, handlers...)
}

// POST 註冊 POST 路由
func (r *OptimizedRouter) POST(path string, handlers ...hypcontext.HandlerFunc) {
	r.Handle(http.MethodPost, path, handlers...)
}

// PUT 註冊 PUT 路由
func (r *OptimizedRouter) PUT(path string, handlers ...hypcontext.HandlerFunc) {
	r.Handle(http.MethodPut, path, handlers...)
}

// DELETE 註冊 DELETE 路由
func (r *OptimizedRouter) DELETE(path string, handlers ...hypcontext.HandlerFunc) {
	r.Handle(http.MethodDelete, path, handlers...)
}

// ===== 路由匹配（核心） =====

// ServeHTTP 實現 http.Handler
func (r *OptimizedRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c := hypcontext.New(w, req)
	defer c.Release()

	path := req.URL.Path
	method := req.Method

	// 快取查找（如果啟用）
	if r.enableCache {
		cacheKey := method + path
		if entry := r.cache.get(cacheKey); entry != nil {
			c.Params = r.makeContextParams(entry.params)
			r.executeHandlers(c, entry.handlers)
			return
		}
	}

	// Radix tree 查找
	if root := r.trees[method]; root != nil {
		handlers, params := root.search(path, r.getParams())
		if handlers != nil {
			c.Params = r.makeContextParams(params)

			// 加入快取
			if r.enableCache && len(params) == 0 { // 只快取靜態路由
				r.cache.put(method+path, handlers, params)
			}

			r.executeHandlers(c, handlers)
			r.putParams(params)
			return
		}
		r.putParams(params)
	}

	// 檢查其他方法（405）
	if r.handleMethodNotAllowed {
		for m := range r.trees {
			if m == method {
				continue
			}
			if handlers, _ := r.trees[m].search(path, nil); handlers != nil {
				if r.methodNotAllowed != nil {
					r.methodNotAllowed(c)
				} else {
					c.Status(http.StatusMethodNotAllowed)
				}
				return
			}
		}
	}

	// 404
	if r.notFound != nil {
		r.notFound(c)
	} else {
		c.Status(http.StatusNotFound)
	}
}

// search 在 Radix Tree 中搜索
func (n *radixNode) search(path string, params []Param) ([]hypcontext.HandlerFunc, []Param) {
	var (
		//handlers []hypcontext.HandlerFunc
		p = params
	)

walk:
	for {
		if len(path) > len(n.path) {
			if path[:len(n.path)] == n.path {
				path = path[len(n.path):]

				// 尋找子節點
				if !n.wildChild {
					// 使用索引快速查找
					c := path[0]
					for i, index := range []byte(n.indices) {
						if c == index {
							n = n.children[i]
							continue walk
						}
					}

					// 沒找到
					return nil, p
				}

				// 處理通配符
				n = n.children[0]
				switch n.nType {
				case param:
					// 提取參數值
					end := 0
					for end < len(path) && path[end] != '/' {
						end++
					}

					if p == nil {
						p = make([]Param, 0, 4)
					}
					p = append(p, Param{
						Key:   n.path[1:],
						Value: path[:end],
					})

					if end < len(path) {
						if len(n.children) > 0 {
							path = path[end:]
							n = n.children[0]
							continue walk
						}
						return nil, p
					}

					return n.handlers, p

				case catchAll:
					// 捕獲所有剩餘路徑
					if p == nil {
						p = make([]Param, 0, 4)
					}
					p = append(p, Param{
						Key:   n.path[2:],
						Value: path,
					})
					return n.handlers, p
				}
			}
		} else if path == n.path {
			// 完全匹配
			return n.handlers, p
		}

		// 不匹配
		return nil, p
	}
}

// addRoute 添加路由到樹
func (n *radixNode) addRoute(path string, handlers []hypcontext.HandlerFunc) {
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

	// 需要分割當前節點
	if i < len(n.path) {
		child := &radixNode{
			path:      n.path[i:],
			wildChild: n.wildChild,
			nType:     static,
			indices:   n.indices,
			children:  n.children,
			handlers:  n.handlers,
			priority:  n.priority - 1,
		}

		n.children = []*radixNode{child}
		n.indices = string(n.path[i])
		n.path = path[:i]
		n.handlers = nil
		n.wildChild = false
	}

	// 插入新節點
	if i < len(path) {
		path = path[i:]

		if n.wildChild {
			n = n.children[0]
			n.priority++

			// 檢查通配符
			if len(path) >= len(n.path) && n.path == path[:len(n.path)] &&
				n.nType != catchAll &&
				(len(n.path) >= len(path) || path[len(n.path)] == '/') {
				n.addRoute(path, handlers)
			} else {
				panic("path conflict")
			}
			return
		}

		// 尋找子節點插入點
		c := path[0]

		// 處理參數
		if n.nType == param && c == '/' && len(n.children) == 1 {
			n = n.children[0]
			n.priority++
			n.addRoute(path, handlers)
			return
		}

		// 檢查子節點
		for i, index := range []byte(n.indices) {
			if c == index {
				n = n.children[i]
				n.priority++
				n.addRoute(path, handlers)
				return
			}
		}

		// 插入新子節點
		if c != ':' && c != '*' {
			n.indices += string(c)
			child := &radixNode{}
			n.children = append(n.children, child)
			n = child
		}

		n.insertChild(path, fullPath, handlers)
		return
	}

	// 設置處理器
	if n.handlers != nil {
		panic("handlers already registered")
	}
	n.handlers = handlers
}

// insertChild 插入子節點
func (n *radixNode) insertChild(path, fullPath string, handlers []hypcontext.HandlerFunc) {
	for {
		// 查找通配符
		wildcard, i, valid := findWildcard(path)
		if i < 0 {
			break
		}

		if !valid {
			panic("invalid wildcard")
		}

		if wildcard[0] == ':' { // 參數
			if i > 0 {
				n.path = path[:i]
				path = path[i:]
			}

			child := &radixNode{
				nType:    param,
				path:     wildcard,
				fullPath: fullPath,
			}
			n.children = []*radixNode{child}
			n.wildChild = true
			n = child

			if len(wildcard) < len(path) {
				path = path[len(wildcard):]
				child := &radixNode{
					priority: 1,
					fullPath: fullPath,
				}
				n.children = []*radixNode{child}
				n = child
				continue
			}

			n.handlers = handlers
			return
		} else { // catchAll
			if i+len(wildcard) != len(path) {
				panic("catch-all must be at end")
			}

			if i > 0 {
				n.path = path[:i]
			}

			child := &radixNode{
				wildChild: true,
				nType:     catchAll,
				fullPath:  fullPath,
			}
			n.children = []*radixNode{child}
			n.indices = string('/')
			n = child

			child = &radixNode{
				path:     path[i:],
				nType:    catchAll,
				handlers: handlers,
				fullPath: fullPath,
			}
			n.children = []*radixNode{child}
			return
		}
	}

	// 靜態路徑
	n.path = path
	n.handlers = handlers
	n.fullPath = fullPath
}

// ===== 輔助函數 =====

// executeHandlers 執行處理器鏈
func (r *OptimizedRouter) executeHandlers(c *hypcontext.Context, handlers []hypcontext.HandlerFunc) {
	// 執行全域中間件
	for _, h := range r.middleware {
		h(c)
		if c.Response.Written() {
			return
		}
	}

	// 執行路由處理器
	for _, h := range handlers {
		h(c)
		if c.Response.Written() {
			return
		}
	}
}

// getParams 從池中獲取參數切片
func (r *OptimizedRouter) getParams() []Param {
	return r.pool.Get().([]Param)
}

// putParams 返回參數切片到池
func (r *OptimizedRouter) putParams(params []Param) {
	if params != nil {
		params = params[:0]
		r.pool.Put(params)
	}
}

// makeContextParams 轉換參數格式
func (r *OptimizedRouter) makeContextParams(params []Param) hypcontext.Params {
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

// longestCommonPrefix 最長公共前綴
func longestCommonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}

	// 使用 unsafe 加速比較
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

// findWildcard 查找通配符
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

// countParams 計算參數數量
func countParams(path string) int {
	n := 0
	for i := 0; i < len(path); i++ {
		if path[i] == ':' || path[i] == '*' {
			n++
		}
	}
	return n
}

// ===== LRU 快取實現 =====

func newRouteCache(capacity int) *routeCache {
	return &routeCache{
		cache:    make(map[string]*cacheEntry),
		capacity: capacity,
	}
}

func (c *routeCache) get(key string) *cacheEntry {
	c.mu.RLock()
	entry, exists := c.cache[key]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// 移到頭部（LRU）
	c.mu.Lock()
	c.moveToHead(entry)
	c.mu.Unlock()

	return entry
}

func (c *routeCache) put(key string, handlers []hypcontext.HandlerFunc, params []Param) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.cache[key]; exists {
		entry.handlers = handlers
		entry.params = params
		c.moveToHead(entry)
		return
	}

	// 新增項目
	entry := &cacheEntry{
		key:      key,
		handlers: handlers,
		params:   params,
	}

	c.cache[key] = entry
	c.addToHead(entry)
	c.size++

	// 超過容量，移除最舊的
	if c.size > c.capacity {
		c.removeTail()
	}
}

func (c *routeCache) moveToHead(entry *cacheEntry) {
	c.removeEntry(entry)
	c.addToHead(entry)
}

func (c *routeCache) addToHead(entry *cacheEntry) {
	entry.prev = nil
	entry.next = c.head

	if c.head != nil {
		c.head.prev = entry
	}
	c.head = entry

	if c.tail == nil {
		c.tail = entry
	}
}

func (c *routeCache) removeEntry(entry *cacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		c.head = entry.next
	}

	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		c.tail = entry.prev
	}
}

func (c *routeCache) removeTail() {
	if c.tail == nil {
		return
	}

	delete(c.cache, c.tail.key)
	c.removeEntry(c.tail)
	c.size--
}

// ===== 公開方法 =====

// Use 添加全域中間件
func (r *OptimizedRouter) Use(middleware ...hypcontext.HandlerFunc) {
	r.middleware = append(r.middleware, middleware...)
}

// NotFound 設置 404 處理器
func (r *OptimizedRouter) NotFound(handler hypcontext.HandlerFunc) {
	r.notFound = handler
}

// MethodNotAllowed 設置 405 處理器
func (r *OptimizedRouter) MethodNotAllowed(handler hypcontext.HandlerFunc) {
	r.methodNotAllowed = handler
	r.handleMethodNotAllowed = true
}
