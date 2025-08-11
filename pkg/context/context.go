// Package context 提供 HTTP/3 QUIC 優化的請求上下文處理
package context

import (
	//"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// Context 是 HypGo 框架的核心上下文結構
// 同時支援 HTTP/1.1, 2.0, 3.0
type Context struct {
	// 請求和回應
	Request  *http.Request
	Response ResponseWriter

	// HTTP/3 QUIC 特定支援
	quicConn   *QuicConnection
	streamInfo *StreamInfo

	// 路由參數
	Params     Params
	queryCache url.Values
	formCache  url.Values

	// 中間件和處理器
	handlers []HandlerFunc
	index    int8

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
}

// Protocol 定義支援的協議類型
type Protocol int

const (
	HTTP1 Protocol = iota
	HTTP2
	HTTP3
)

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

// ResponseWriter 擴展標準 ResponseWriter 支援 HTTP/3
type ResponseWriter interface {
	http.ResponseWriter
	http.Hijacker
	http.Flusher
	http.Pusher

	// HTTP/3 特定方法
	WriteHeader3(statusCode int, headers http.Header)
	PushPromise(target string, opts *http.PushOptions) error
	StreamID() uint64

	// 狀態方法
	Status() int
	Size() int
	Written() bool
	WriteString(string) (int, error)
}

// HandlerFunc 定義處理函數類型
type HandlerFunc func(*Context)

// Params 路由參數
type Params []Param

type Param struct {
	Key   string
	Value string
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

// New 創建新的 Context
func New(w http.ResponseWriter, r *http.Request) *Context {
	c := &Context{
		Request:   r,
		Response:  &responseWriter{ResponseWriter: w},
		Params:    make(Params, 0, 8),
		handlers:  make([]HandlerFunc, 0, 8),
		index:     -1,
		Keys:      make(map[string]interface{}),
		startTime: time.Now(),
		metrics:   &RequestMetrics{},
	}

	// 檢測並設置協議
	c.detectProtocol()

	// 如果是 HTTP/3，初始化 QUIC 相關資訊
	if c.protocol == HTTP3 {
		c.initQuicConnection()
	}

	return c
}

// Next 執行下一個中間件
func (c *Context) Next() {
	c.index++
	for c.index < int8(len(c.handlers)) {
		c.handlers[c.index](c)
		c.index++
	}
}

// Abort 中止請求處理
func (c *Context) Abort() {
	c.index = int8(len(c.handlers))
}

// AbortWithStatus 中止並設置狀態碼
func (c *Context) AbortWithStatus(code int) {
	c.Status(code)
	c.Abort()
}

// ===== HTTP/3 QUIC 優化方法 =====

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

// initQuicConnection 初始化 QUIC 連接資訊
func (c *Context) initQuicConnection() {
	// 從請求中提取 QUIC 連接資訊
	if conn, ok := c.Request.Context().Value("quic_conn").(*QuicConnection); ok {
		c.quicConn = conn
	}

	// 初始化流資訊
	c.streamInfo = &StreamInfo{
		StreamID: c.extractStreamID(),
		Priority: c.extractPriority(),
	}
}

// extractStreamID 提取流 ID
func (c *Context) extractStreamID() uint64 {
	// 實現從請求中提取流 ID 的邏輯
	return 0
}

// extractPriority 提取優先級
func (c *Context) extractPriority() uint8 {
	// 實現優先級提取邏輯
	return 0
}

// SetStreamPriority 設置流優先級（HTTP/3 特性）
func (c *Context) SetStreamPriority(priority uint8) error {
	if c.protocol != HTTP3 {
		return fmt.Errorf("stream priority only available in HTTP/3")
	}
	c.streamInfo.Priority = priority
	// 實際設置 QUIC 流優先級
	return nil
}

// GetRTT 獲取往返時間（對 HTTP/3 優化）
func (c *Context) GetRTT() time.Duration {
	if c.quicConn != nil {
		return c.quicConn.rtt
	}
	return 0
}

// GetCongestionWindow 獲取擁塞窗口大小
func (c *Context) GetCongestionWindow() uint32 {
	if c.quicConn != nil {
		return c.quicConn.congWin
	}
	return 0
}

// ===== 請求資料獲取方法 =====

// Param 獲取路由參數
func (c *Context) Param(key string) string {
	for _, p := range c.Params {
		if p.Key == key {
			return p.Value
		}
	}
	return ""
}

// Query 獲取查詢參數
func (c *Context) Query(key string) string {
	value, _ := c.GetQuery(key)
	return value
}

// GetQuery 獲取查詢參數，返回是否存在
func (c *Context) GetQuery(key string) (string, bool) {
	if c.queryCache == nil {
		c.queryCache = c.Request.URL.Query()
	}
	values := c.queryCache[key]
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// QueryArray 獲取查詢參數陣列
func (c *Context) QueryArray(key string) []string {
	if c.queryCache == nil {
		c.queryCache = c.Request.URL.Query()
	}
	return c.queryCache[key]
}

// PostForm 獲取表單資料
func (c *Context) PostForm(key string) string {
	value, _ := c.GetPostForm(key)
	return value
}

// GetPostForm 獲取表單資料，返回是否存在
func (c *Context) GetPostForm(key string) (string, bool) {
	if c.formCache == nil {
		c.Request.ParseForm()
		c.formCache = c.Request.PostForm
	}
	values := c.formCache[key]
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// ===== JSON 處理方法 =====

// BindJSON 綁定 JSON 資料到結構體
func (c *Context) BindJSON(obj interface{}) error {
	return c.bindWith(obj, bindingJSON{})
}

// ShouldBindJSON 嘗試綁定 JSON（不會中止請求）
func (c *Context) ShouldBindJSON(obj interface{}) error {
	return c.bindWith(obj, bindingJSON{})
}

// JSON 回應 JSON 資料
func (c *Context) JSON(code int, obj interface{}) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(code)

	encoder := json.NewEncoder(c.Response)
	if err := encoder.Encode(obj); err != nil {
		c.Error(err)
	}
}

// IndentedJSON 回應格式化的 JSON
func (c *Context) IndentedJSON(code int, obj interface{}) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(code)

	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		c.Error(err)
		return
	}
	c.Response.Write(data)
}

// ===== 回應方法 =====

// String 回應字串
func (c *Context) String(code int, format string, values ...interface{}) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(code)

	if len(values) > 0 {
		fmt.Fprintf(c.Response, format, values...)
	} else {
		io.WriteString(c.Response, format)
	}
}

// HTML 回應 HTML
func (c *Context) HTML(code int, html string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(code)
	io.WriteString(c.Response, html)
}

// File 回應檔案
func (c *Context) File(filepath string) {
	http.ServeFile(c.Response, c.Request, filepath)
}

// Stream 串流回應（支援 HTTP/3 優化）
func (c *Context) Stream(step func(w io.Writer) bool) bool {
	w := c.Response
	for {
		if !step(w) {
			return false
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// HTTP/3 優化：調整流控制
		if c.protocol == HTTP3 && c.quicConn != nil {
			// 根據 RTT 和擁塞窗口調整發送速率
			c.adaptiveStreamControl()
		}
	}
}

// adaptiveStreamControl HTTP/3 自適應流控制
func (c *Context) adaptiveStreamControl() {
	// 根據網路狀況動態調整
	if c.quicConn.rtt > 100*time.Millisecond {
		time.Sleep(10 * time.Millisecond)
	}
}

// ===== Header 操作 =====

// Header 設置回應頭
func (c *Context) Header(key, value string) {
	if c.Response.Written() {
		return
	}
	c.Response.Header().Set(key, value)
}

// GetHeader 獲取請求頭
func (c *Context) GetHeader(key string) string {
	return c.Request.Header.Get(key)
}

// Status 設置狀態碼
func (c *Context) Status(code int) {
	c.Response.WriteHeader(code)
}

// ===== Cookie 操作 =====

// SetCookie 設置 Cookie
func (c *Context) SetCookie(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   maxAge,
		Path:     path,
		Domain:   domain,
		Secure:   secure,
		HttpOnly: httpOnly,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(c.Response, cookie)
}

// Cookie 獲取 Cookie
func (c *Context) Cookie(name string) (string, error) {
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

// ===== 上下文資料存儲 =====

// Set 存儲資料到上下文
func (c *Context) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Keys[key] = value
}

// Get 從上下文獲取資料
func (c *Context) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, exists := c.Keys[key]
	return value, exists
}

// MustGet 必須獲取資料，不存在則 panic
func (c *Context) MustGet(key string) interface{} {
	value, exists := c.Get(key)
	if !exists {
		panic(fmt.Sprintf("Key %s does not exist", key))
	}
	return value
}

// GetString 獲取字串值
func (c *Context) GetString(key string) string {
	if val, exists := c.Get(key); exists {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// GetInt 獲取整數值
func (c *Context) GetInt(key string) int {
	if val, exists := c.Get(key); exists {
		if i, ok := val.(int); ok {
			return i
		}
	}
	return 0
}

// GetBool 獲取布林值
func (c *Context) GetBool(key string) bool {
	if val, exists := c.Get(key); exists {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// GetDuration 獲取時間間隔
func (c *Context) GetDuration(key string) time.Duration {
	if val, exists := c.Get(key); exists {
		if d, ok := val.(time.Duration); ok {
			return d
		}
	}
	return 0
}

// ===== 錯誤處理 =====

// Error 添加錯誤
func (c *Context) Error(err error) {
	c.Errors = append(c.Errors, &errorMsg{
		Err:  err,
		Type: ErrorTypePrivate,
	})
}

// AbortWithError 中止並返回錯誤
func (c *Context) AbortWithError(code int, err error) {
	c.Status(code)
	c.Error(err)
	c.Abort()
}

// ===== 檔案上傳 =====

// FormFile 獲取上傳的檔案
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	if c.Request.MultipartForm == nil {
		if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
			return nil, err
		}
	}
	file, header, err := c.Request.FormFile(name)
	if err != nil {
		return nil, err
	}
	file.Close()
	return header, nil
}

// MultipartForm 獲取多部分表單
func (c *Context) MultipartForm() (*multipart.Form, error) {
	err := c.Request.ParseMultipartForm(32 << 20)
	return c.Request.MultipartForm, err
}

// SaveUploadedFile 保存上傳的檔案
func (c *Context) SaveUploadedFile(file *multipart.FileHeader, dst string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	// 創建目標檔案
	// 實現檔案保存邏輯
	return nil
}

// ===== 客戶端資訊 =====

// ClientIP 獲取客戶端 IP
func (c *Context) ClientIP() string {
	// 檢查 X-Forwarded-For
	if ip := c.GetHeader("X-Forwarded-For"); ip != "" {
		return ip
	}
	// 檢查 X-Real-IP
	if ip := c.GetHeader("X-Real-IP"); ip != "" {
		return ip
	}
	// 從連接獲取
	if ip, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil {
		return ip
	}
	return ""
}

// ContentType 獲取內容類型
func (c *Context) ContentType() string {
	return c.GetHeader("Content-Type")
}

// IsWebsocket 檢查是否為 WebSocket 請求
func (c *Context) IsWebsocket() bool {
	return c.GetHeader("Upgrade") == "websocket"
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

// ===== HTTP/3 Server Push =====

// Push 使用 HTTP/3 Server Push
func (c *Context) Push(target string, opts *http.PushOptions) error {
	if c.protocol != HTTP3 && c.protocol != HTTP2 {
		return fmt.Errorf("server push only available in HTTP/2 and HTTP/3")
	}

	pusher, ok := c.Response.(http.Pusher)
	if !ok {
		return fmt.Errorf("server push not supported")
	}

	return pusher.Push(target, opts)
}

// ===== Deadline 和 Timeout =====

// Deadline 返回請求的截止時間
func (c *Context) Deadline() (time.Time, bool) {
	return c.Request.Context().Deadline()
}

// Done 返回一個通道，當請求被取消時關閉
func (c *Context) Done() <-chan struct{} {
	return c.Request.Context().Done()
}

// Err 返回請求的錯誤（如果有）
func (c *Context) Err() error {
	return c.Request.Context().Err()
}

// Value 實現 context.Context 介面
func (c *Context) Value(key interface{}) interface{} {
	if keyAsString, ok := key.(string); ok {
		if val, exists := c.Get(keyAsString); exists {
			return val
		}
	}
	return c.Request.Context().Value(key)
}

// ===== 輔助結構 =====

// responseWriter 實現 ResponseWriter 介面
type responseWriter struct {
	http.ResponseWriter
	status  int
	size    int
	written bool
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.written {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
		w.written = true
	}
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.size += n
	return n, err
}

func (w *responseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *responseWriter) Status() int {
	return w.status
}

func (w *responseWriter) Size() int {
	return w.size
}

func (w *responseWriter) Written() bool {
	return w.written
}

// errorMsg 錯誤訊息
type errorMsg struct {
	Err  error
	Type ErrorType
}

type errorMsgs []*errorMsg

type ErrorType uint64

const (
	ErrorTypePrivate ErrorType = 1 << 63
	ErrorTypePublic  ErrorType = 1 << 62
)

// bindingJSON JSON 綁定實現
type bindingJSON struct{}

func (bindingJSON) bindWith(c *Context, obj interface{}) error {
	decoder := json.NewDecoder(c.Request.Body)
	return decoder.Decode(obj)
}
