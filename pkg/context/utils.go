// @chris
package context

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"sort"
	"strconv"
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

// WriteHeader 記錄狀態碼，實際送出延後到 WriteHeaderNow / 首次 Write。
// header 已送出後再改無效且不記錄：先前會改 status 並往 stdout 印 debug——
// Status() 回報客戶端從未收到的值，生產環境還繞過 logger 刷 stdout
func (w *responseWriter) WriteHeader(code int) {
	if code > 0 && !w.written {
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
		conn, rw, err := hijacker.Hijack()
		if err == nil {
			// 連線已交出（如 WebSocket 升級），標記已寫入，
			// 讓請求結束時的 WriteHeaderNow 成為 no-op，避免對 hijacked 連線寫 header
			w.written = true
		}
		return conn, rw, err
	}
	return nil, nil, fmt.Errorf("the ResponseWriter doesn't support hijacking")
}

// ReadFrom 實現 io.ReaderFrom 介面
// 確保 http.ServeFile 等函式在 HTTP/2 下能正確傳輸 body
func (w *responseWriter) ReadFrom(src io.Reader) (n int64, err error) {
	w.WriteHeaderNow()
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(src)
	} else {
		// 包裝以避免 io.Copy 遞迴檢查 ReaderFrom
		n, err = io.Copy(writerOnly{w.ResponseWriter}, src)
	}
	w.size += int(n)
	return
}

// writerOnly 將 io.Writer 包裝為純 Writer，隱藏其他介面
// 防止 io.Copy 遞迴偵測 ReaderFrom
type writerOnly struct {
	io.Writer
}

// Unwrap 回傳底層的 ResponseWriter，讓 http.ResponseController 能設定讀寫期限
func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
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

// Push 實現 http.Pusher 介面（只有 HTTP/2 的底層 writer 支援）
func (w *responseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Pusher 返回 http.Pusher
func (w *responseWriter) Pusher() http.Pusher {
	pusher, _ := w.ResponseWriter.(http.Pusher)
	return pusher
}

// WriteHeader3 一次加上多個回應標頭並設定狀態碼（名稱沿用舊 API，任何協議都可用）
func (w *responseWriter) WriteHeader3(statusCode int, headers http.Header) {
	// 設置額外的 HTTP/3 特定頭部
	for k, v := range headers {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(statusCode)
}

// StreamID 返回流 ID。目前恆為 0：net/http 與 quic-go 都不在不接管 stream 的情況下
// 提供這個值，保留方法是為了介面相容
func (w *responseWriter) StreamID() uint64 {
	return w.streamID
}

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

// parseAccept 解析 Accept header，依 q 值由高到低排序（同值維持原順序），q=0 的型別捨棄。
// 先前忽略 q 值：text/html;q=0.1, application/json;q=0.9 選到 text/html
func parseAccept(accept string) []string {
	type item struct {
		media string
		q     float64
	}
	parts := strings.Split(accept, ",")
	items := make([]item, 0, len(parts))
	for _, part := range parts {
		media, params, _ := strings.Cut(part, ";")
		media = strings.ToLower(strings.TrimSpace(media))
		if media == "" {
			continue
		}
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			if k, v, ok := strings.Cut(strings.TrimSpace(p), "="); ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = f
				}
			}
		}
		if q > 0 {
			items = append(items, item{media, q})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].q > items[j].q })
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.media
	}
	return out
}

// acceptMatches 回報 Accept 的一個型別是否涵蓋 offer（支援 */* 與 type/*）
func acceptMatches(accept, offer string) bool {
	if accept == offer || accept == "*/*" {
		return true
	}
	if prefix, ok := strings.CutSuffix(accept, "/*"); ok {
		return strings.HasPrefix(offer, prefix+"/")
	}
	return false
}

// discardWriter 丟棄所有輸出的 http.ResponseWriter（Context.Copy 的副本使用）
type discardWriter struct{ header http.Header }

func (d *discardWriter) Header() http.Header         { return d.header }
func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardWriter) WriteHeader(int)             {}

// isValidIP 檢查是否為有效 IP
func isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// nameOfFunction 獲取函數名稱
func nameOfFunction(f interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
}

// LogRequest 把一行請求摘要印到 stdout（除錯用；不經過 logger，正式環境請用 Logger 中間件）
func (c *Context) LogRequest() {
	fmt.Printf("[%s] %s %s %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		c.Request.Method,
		c.Request.URL.Path,
		c.ClientIP())
}
