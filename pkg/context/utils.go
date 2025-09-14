package context

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ===== Params 方法 =====

// Param 路由參數項
type Param struct {
	Key   string
	Value string
}

// Params 路由參數集合
type Params []Param

// Get 從 Params 獲取值
func (ps Params) Get(key string) (string, bool) {
	for _, p := range ps {
		if p.Key == key {
			return p.Value, true
		}
	}
	return "", false
}

// ByName 通過名稱獲取參數值
func (ps Params) ByName(key string) string {
	val, _ := ps.Get(key)
	return val
}

// ===== ResponseWriter 實現 =====

// ResponseWriter 擴展標準 ResponseWriter 支援 HTTP/3
type ResponseWriter interface {
	http.ResponseWriter
	http.Hijacker
	http.Flusher
	http.Pusher
	io.StringWriter

	// HTTP/3 特定方法
	WriteHeader3(statusCode int, headers http.Header)
	PushPromise(target string, opts *http.PushOptions) error
	StreamID() uint64

	// 狀態方法
	Status() int
	Size() int
	Written() bool
	WriteHeaderNow()
	Pusher() http.Pusher
}

// responseWriter 實現 ResponseWriter 介面
type responseWriter struct {
	http.ResponseWriter
	status   int
	size     int
	written  bool
	streamID uint64
	mu       sync.Mutex
}

// newResponseWriter 創建新的 responseWriter
func newResponseWriter(w http.ResponseWriter) ResponseWriter {
	return &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

// WriteHeader 寫入狀態碼
func (w *responseWriter) WriteHeader(code int) {
	if code > 0 && w.status != code {
		if w.written {
			debugPrint("[WARNING] Headers were already written. Wanted to override status code %d with %d", w.status, code)
		}
		w.status = code
	}
}

// WriteHeaderNow 立即寫入 header
func (w *responseWriter) WriteHeaderNow() {
	if !w.written {
		w.mu.Lock()
		defer w.mu.Unlock()

		if !w.written {
			w.written = true
			w.ResponseWriter.WriteHeader(w.status)
		}
	}
}

// Write 寫入資料
func (w *responseWriter) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	n, err := w.ResponseWriter.Write(data)
	w.size += n
	return n, err
}

// WriteString 寫入字串
func (w *responseWriter) WriteString(s string) (int, error) {
	w.WriteHeaderNow()
	n, err := io.WriteString(w.ResponseWriter, s)
	w.size += n
	return n, err
}

// Status 返回狀態碼
func (w *responseWriter) Status() int {
	return w.status
}

// Size 返回已寫入的大小
func (w *responseWriter) Size() int {
	return w.size
}

// Written 返回是否已寫入
func (w *responseWriter) Written() bool {
	return w.written
}

// Hijack 實現 http.Hijacker 介面
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("the ResponseWriter doesn't support hijacking")
}

// Flush 實現 http.Flusher 介面
func (w *responseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		if !w.written {
			w.WriteHeaderNow()
		}
		flusher.Flush()
	}
}

// Push 實現 http.Pusher 介面 (HTTP/2 和 HTTP/3)
func (w *responseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return fmt.Errorf("server push not supported")
}

// Pusher 返回 http.Pusher
func (w *responseWriter) Pusher() http.Pusher {
	pusher, _ := w.ResponseWriter.(http.Pusher)
	return pusher
}

// WriteHeader3 HTTP/3 特定的寫入頭部方法
func (w *responseWriter) WriteHeader3(statusCode int, headers http.Header) {
	// 設置額外的 HTTP/3 特定頭部
	for k, v := range headers {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(statusCode)
}

// PushPromise HTTP/3 推送承諾
func (w *responseWriter) PushPromise(target string, opts *http.PushOptions) error {
	// 實現 HTTP/3 特定的推送承諾邏輯
	return w.Push(target, opts)
}

// StreamID 返回流 ID
func (w *responseWriter) StreamID() uint64 {
	return w.streamID
}

// ===== 物件池支援 =====
/*
var contextPool = &sync.Pool{
	New: func() interface{} {
		return &Context{}
	},
}

// AcquireContext 從物件池獲取 Context
func AcquireContext(w http.ResponseWriter, r *http.Request) *Context {
	c := contextPool.Get().(*Context)
	c.Reset(w, r)
	return c
}

// ReleaseContext 釋放 Context 回物件池
func ReleaseContext(c *Context) {
	c.Reset(nil, nil)
	contextPool.Put(c)
}
*/
// ===== 輔助函數 =====

// bodyAllowedForStatus 檢查狀態碼是否允許有 body
func bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == http.StatusNoContent:
		return false
	case status == http.StatusNotModified:
		return false
	}
	return true
}

// filterFlags 過濾標誌
func filterFlags(content string) string {
	for i, char := range content {
		if char == ' ' || char == ';' {
			return content[:i]
		}
	}
	return content
}

// parseAccept 解析 Accept header
func parseAccept(accept string) []string {
	parts := strings.Split(accept, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if i := strings.IndexByte(part, ';'); i > 0 {
			part = part[:i]
		}
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// isValidIP 檢查是否為有效 IP
func isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// nameOfFunction 獲取函數名稱
func nameOfFunction(f interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
}

// debugPrint 調試打印
func debugPrint(format string, values ...interface{}) {
	// 實現調試輸出
	fmt.Printf("[HYPGO-debug] "+format+"\n", values...)
}

// LogRequest 記錄請求信息
func (c *Context) LogRequest() {
	fmt.Printf("[%s] %s %s %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		c.Request.Method,
		c.Request.URL.Path,
		c.ClientIP())
}
