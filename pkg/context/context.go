package context

// Package context 提供 HypGo 框架的核心上下文功能
import (
	stdcontext "context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// ===== 標準 context.Context 橋接 =====

// hypContextKey 用於在標準 context.Context 中存放 *Context 的 key
// 使用未匯出的 struct 確保不會與其他套件的 key 衝突
type hypContextKey struct{}

// NewContext 將 HypGo *Context 嵌入標準 context.Context 中
// 讓下游接受 context.Context 的 API 可透過 FromContext 取回 HypGo Context
func NewContext(parent stdcontext.Context, c *Context) stdcontext.Context {
	return stdcontext.WithValue(parent, hypContextKey{}, c)
}

// FromContext 從標準 context.Context 中提取 HypGo *Context
// 若 ctx 本身就是 *Context（因其實現 context.Context 介面），也能正確提取
func FromContext(ctx stdcontext.Context) (*Context, bool) {
	c, ok := ctx.Value(hypContextKey{}).(*Context)
	return c, ok
}

// MustFromContext 同 FromContext，但不存在時 panic
// 適用於框架內部保證 HypGo Context 存在的場景
func MustFromContext(ctx stdcontext.Context) *Context {
	c, ok := FromContext(ctx)
	if !ok {
		panic("hypgo: context.Context does not contain a *Context")
	}
	return c
}

// Context 是 HypGo 框架的核心上下文結構
// 同時支援 HTTP/1.1, 2.0, 3.0
type Context struct {
	// 請求和回應
	Request  *http.Request
	Response ResponseWriter
	Writer   ResponseWriter // Gin 兼容別名

	// HTTP/3 QUIC 特定支援
	quicConn   *QuicConnection
	streamInfo *StreamInfo

	// 路由參數
	Params      Params
	queryCache  url.Values
	formCache   url.Values
	rawData     []byte
	routerGroup *RouterGroup

	// 中間件和處理器
	handlers []HandlerFunc
	index    int8
	fullPath string

	// 資料存儲
	mu   sync.RWMutex
	Keys map[string]interface{}

	// 錯誤處理
	Errors errorMsgs

	// 協議資訊
	protocol Protocol

	// 效能監控
	startTime time.Time
	metrics   *RequestMetrics

	// 內容協商
	Accepted []string

	// SameSite cookie 設置
	sameSite http.SameSite
}

// QuicConnection 封裝 QUIC 連接資訊
type QuicConnection struct {
	conn      http3.HTTPStreamer
	streamID  uint64
	priority  uint8
	rtt       time.Duration
	congWin   uint32
	bytesRead uint64
}

// StreamInfo 包含 HTTP/3 流資訊
type StreamInfo struct {
	StreamID     uint64
	Priority     uint8
	Dependencies []uint64
	Weight       uint8
	Exclusive    bool
}

// HandlerFunc 定義處理函數類型
type HandlerFunc func(*Context)

// HandlersChain 定義處理器鏈
type HandlersChain []HandlerFunc

// RouterGroup 路由組（簡化版）
type RouterGroup struct {
	Handlers HandlersChain
	basePath string
	root     bool
	engine   *Engine
}

// Engine 引擎（簡化版）
type Engine struct {
	HTMLRender HTMLRender
}

// HTMLRender HTML 渲染器介面
type HTMLRender interface {
	Instance(string, interface{}) Render
}

// RequestMetrics 請求指標
type RequestMetrics struct {
	Duration      time.Duration
	BytesIn       int64
	BytesOut      int64
	StreamsOpened int
	RTT           time.Duration
}

// ===== 核心方法 =====

// New 創建新的 Context（使用物件池）
func New(w http.ResponseWriter, r *http.Request) *Context {
	// 優先使用物件池
	if contextPool != nil {
		return AcquireContext(w, r)
	}

	c := &Context{
		Request:   r,
		Response:  newResponseWriter(w),
		Params:    make(Params, 0, 8),
		handlers:  make([]HandlerFunc, 0, 8),
		index:     -1,
		Keys:      make(map[string]interface{}),
		startTime: time.Now(),
		metrics:   &RequestMetrics{},
		sameSite:  http.SameSiteDefaultMode,
	}

	// Writer 是 Response 的別名（Gin 兼容）
	c.Writer = c.Response

	// 檢測並設置協議
	c.detectProtocol()

	// 如果是 HTTP/3，初始化 QUIC 相關資訊
	if c.protocol == HTTP3 {
		c.initQuicConnection()
	}

	return c
}

// Reset 重置 Context 到初始狀態（用於物件池）
func (c *Context) Reset(w http.ResponseWriter, r *http.Request) {
	c.Request = r
	if w != nil {
		c.Response = newResponseWriter(w)
		c.Writer = c.Response
	} else {
		c.Response = nil
		c.Writer = nil
	}
	c.Params = c.Params[:0]
	c.handlers = nil
	c.index = -1
	c.Keys = nil
	c.Errors = c.Errors[:0]
	c.Accepted = nil
	c.queryCache = nil
	c.formCache = nil
	c.rawData = nil
	c.fullPath = ""
	c.startTime = time.Now()
	c.metrics = &RequestMetrics{}
	if r != nil {
		c.detectProtocol()
		if c.protocol == HTTP3 {
			c.initQuicConnection()
		}
	}
}

// Copy 返回當前 Context 的副本（用於 goroutine）
func (c *Context) Copy() *Context {
	cp := &Context{
		Request:  c.Request.Clone(c.Request.Context()),
		Response: nil,
		Writer:   nil,
		Params:   make(Params, len(c.Params)),
		handlers: nil,
		index:    c.index,
		fullPath: c.fullPath,
		protocol: c.protocol,
	}

	copy(cp.Params, c.Params)

	// 深拷貝 Keys
	cp.Keys = make(map[string]interface{}, len(c.Keys))
	c.mu.RLock()
	for k, v := range c.Keys {
		cp.Keys[k] = v
	}
	c.mu.RUnlock()

	// 拷貝錯誤
	cp.Errors = make(errorMsgs, len(c.Errors))
	copy(cp.Errors, c.Errors)

	return cp
}

// Release 釋放 Context 回物件池
func (c *Context) Release() {
	if contextPool != nil {
		ReleaseContext(c)
	}
}

// ===== 中間件執行 =====

// Next 執行下一個中間件
func (c *Context) Next() {
	c.index++
	for c.index < int8(len(c.handlers)) {
		c.handlers[c.index](c)
		c.index++
	}
}

// IsAborted 檢查是否已中止
func (c *Context) IsAborted() bool {
	return c.index >= abortIndex
}

// Abort 中止請求處理
func (c *Context) Abort() {
	c.index = abortIndex
}

// AbortWithStatus 中止並設置狀態碼
func (c *Context) AbortWithStatus(code int) {
	c.Status(code)
	c.Writer.WriteHeaderNow()
	c.Abort()
}

// AbortWithStatusJSON 中止並返回 JSON 錯誤
func (c *Context) AbortWithStatusJSON(code int, jsonObj interface{}) {
	c.Abort()
	c.JSON(code, jsonObj)
}

// AbortWithError 中止並返回錯誤
func (c *Context) AbortWithError(code int, err error) *Error {
	c.AbortWithStatus(code)
	return c.Error(err)
}

// HandlerName 返回當前處理器的名稱
func (c *Context) HandlerName() string {
	return nameOfFunction(c.handlers[c.index])
}

// HandlerNames 返回所有處理器的名稱
func (c *Context) HandlerNames() []string {
	names := make([]string, 0, len(c.handlers))
	for _, handler := range c.handlers {
		names = append(names, nameOfFunction(handler))
	}
	return names
}

// ===== 路徑和路由 =====

// FullPath 返回匹配的路由完整路徑
func (c *Context) FullPath() string {
	return c.fullPath
}

// SetFullPath 設置完整路徑
func (c *Context) SetFullPath(fullPath string) {
	c.fullPath = fullPath
}

// BasePath 獲取基礎路徑
func (c *Context) BasePath() string {
	if c.routerGroup != nil {
		return c.routerGroup.basePath
	}
	return ""
}

// GetRouterGroup 獲取路由組
func (c *Context) GetRouterGroup() *RouterGroup {
	return c.routerGroup
}

// SetRouterGroup 設置路由組
func (c *Context) SetRouterGroup(group *RouterGroup) {
	c.routerGroup = group
}

// ===== Context 介面實現 =====

// Deadline 返回請求的截止時間
func (c *Context) Deadline() (deadline time.Time, ok bool) {
	if c.Request == nil || c.Request.Context() == nil {
		return
	}
	return c.Request.Context().Deadline()
}

// Done 返回一個通道，當請求被取消時關閉
func (c *Context) Done() <-chan struct{} {
	if c.Request == nil || c.Request.Context() == nil {
		return nil
	}
	return c.Request.Context().Done()
}

// Err 返回請求的錯誤（如果有）
func (c *Context) Err() error {
	if c.Request == nil || c.Request.Context() == nil {
		return nil
	}
	return c.Request.Context().Err()
}

// Value 實現 context.Context 介面
// 查詢順序：hypContextKey → Request(key=0) → Keys map → Request.Context()
func (c *Context) Value(key interface{}) interface{} {
	// 允許 FromContext 直接從 *Context 提取（因 *Context 本身實現 context.Context）
	if _, ok := key.(hypContextKey); ok {
		return c
	}
	if key == 0 {
		return c.Request
	}
	if keyAsString, ok := key.(string); ok {
		if val, exists := c.Get(keyAsString); exists {
			return val
		}
	}
	if c.Request == nil || c.Request.Context() == nil {
		return nil
	}
	return c.Request.Context().Value(key)
}

// StdContext 返回攜帶 HypGo Context 的標準 context.Context
// 繼承 Request.Context() 的 deadline、cancellation 和 values
// 這是將 HypGo Context 傳入接受 context.Context 的 API 的推薦方式
func (c *Context) StdContext() stdcontext.Context {
	var parent stdcontext.Context
	if c.Request != nil {
		parent = c.Request.Context()
	} else {
		parent = stdcontext.Background()
	}
	return NewContext(parent, c)
}

// ===== 協議檢測 =====

// detectProtocol 檢測當前使用的協議
func (c *Context) detectProtocol() {
	if c.Request.ProtoMajor == 3 {
		c.protocol = HTTP3
	} else if c.Request.ProtoMajor == 2 {
		c.protocol = HTTP2
	} else {
		c.protocol = HTTP1
	}
}

// Protocol 返回協議字符串
func (c *Context) Protocol() string {
	switch c.protocol {
	case HTTP3:
		return "HTTP/3"
	case HTTP2:
		return "HTTP/2"
	default:
		return "HTTP/1.1"
	}
}

// ===== 效能監控 =====

// GetMetrics 獲取請求指標
func (c *Context) GetMetrics() *RequestMetrics {
	c.metrics.Duration = time.Since(c.startTime)
	return c.metrics
}

// RecordBytesIn 記錄輸入位元組
func (c *Context) RecordBytesIn(bytes int64) {
	c.metrics.BytesIn += bytes
}

// RecordBytesOut 記錄輸出位元組
func (c *Context) RecordBytesOut(bytes int64) {
	c.metrics.BytesOut += bytes
}
