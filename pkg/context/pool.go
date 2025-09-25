package context

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ===== 全域物件池 =====

var (
	// Context 物件池
	contextPool = &sync.Pool{
		New: func() interface{} {
			return &Context{
				Params:   make(Params, 0, 8),
				handlers: make([]HandlerFunc, 0, 8),
				Keys:     make(map[string]interface{}, 8),
				Errors:   make(errorMsgs, 0, 4),
			}
		},
	}

	// ResponseWriter 物件池
	responseWriterPool = &sync.Pool{
		New: func() interface{} {
			return &responseWriter{}
		},
	}

	// RequestMetrics 物件池
	metricsPool = &sync.Pool{
		New: func() interface{} {
			return &RequestMetrics{}
		},
	}

	// StreamInfo 物件池
	streamInfoPool = &sync.Pool{
		New: func() interface{} {
			return &StreamInfo{}
		},
	}

	// QuicConnection 物件池
	quicConnPool = &sync.Pool{
		New: func() interface{} {
			return &QuicConnection{}
		},
	}

	// 緩衝區池（用於 JSON 編碼等）
	bufferPool = &sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 1024))
		},
	}

	// URL Values 池（用於查詢參數解析）
	urlValuesPool = &sync.Pool{
		New: func() interface{} {
			return make(url.Values, 8)
		},
	}
)

// ===== Context 池操作 =====

// AcquireContext 從池中獲取 Context
func AcquireContext(w http.ResponseWriter, r *http.Request) *Context {
	c := contextPool.Get().(*Context)
	c.reset()
	c.Request = r
	c.Response = acquireResponseWriter(w)
	c.metrics = acquireMetrics()
	c.startTime = time.Now()

	// 檢測並設置協議
	c.detectProtocol()

	// 如果是 HTTP/3，初始化 QUIC 相關資訊
	if c.protocol == HTTP3 {
		c.initQuicConnectionFromPool()
	}

	return c
}

// ReleaseContext 將 Context 返回池中
func ReleaseContext(c *Context) {
	if c == nil {
		return
	}

	// 釋放子物件
	if c.Response != nil {
		releaseResponseWriter(c.Response.(*responseWriter))
	}
	if c.metrics != nil {
		releaseMetrics(c.metrics)
	}
	if c.streamInfo != nil {
		releaseStreamInfo(c.streamInfo)
	}
	if c.quicConn != nil {
		releaseQuicConnection(c.quicConn)
	}

	// 清理並返回池中
	c.reset()
	contextPool.Put(c)
}

// reset 重置 Context
func (c *Context) reset() {
	c.Request = nil
	c.Response = nil
	c.quicConn = nil
	c.streamInfo = nil

	// 清理切片但保留容量
	c.Params = c.Params[:0]
	c.handlers = c.handlers[:0]
	c.Errors = c.Errors[:0]

	// 清理 map
	for k := range c.Keys {
		delete(c.Keys, k)
	}

	// 清理快取
	if c.queryCache != nil {
		for k := range c.queryCache {
			delete(c.queryCache, k)
		}
	}
	if c.formCache != nil {
		for k := range c.formCache {
			delete(c.formCache, k)
		}
	}

	c.index = -1
	c.protocol = 0
	c.startTime = time.Time{}
}

// ===== ResponseWriter 池操作 =====

// acquireResponseWriter 從池中獲取 ResponseWriter
func acquireResponseWriter(w http.ResponseWriter) ResponseWriter {
	rw := responseWriterPool.Get().(*responseWriter)
	rw.reset()
	rw.ResponseWriter = w
	rw.status = http.StatusOK
	return rw
}

// releaseResponseWriter 將 ResponseWriter 返回池中
func releaseResponseWriter(rw *responseWriter) {
	if rw == nil {
		return
	}
	rw.reset()
	responseWriterPool.Put(rw)
}

// reset 重置 responseWriter
func (w *responseWriter) reset() {
	w.ResponseWriter = nil
	w.status = 0
	w.size = 0
	w.written = false
	w.streamID = 0
}

// ===== RequestMetrics 池操作 =====

// acquireMetrics 從池中獲取 RequestMetrics
func acquireMetrics() *RequestMetrics {
	m := metricsPool.Get().(*RequestMetrics)
	m.reset()
	return m
}

// releaseMetrics 將 RequestMetrics 返回池中
func releaseMetrics(m *RequestMetrics) {
	if m == nil {
		return
	}
	m.reset()
	metricsPool.Put(m)
}

// reset 重置 RequestMetrics
func (m *RequestMetrics) reset() {
	m.Duration = 0
	m.BytesIn = 0
	m.BytesOut = 0
	m.StreamsOpened = 0
	m.RTT = 0
}

// ===== StreamInfo 池操作 =====

// acquireStreamInfo 從池中獲取 StreamInfo
func acquireStreamInfo() *StreamInfo {
	s := streamInfoPool.Get().(*StreamInfo)
	s.reset()
	return s
}

// releaseStreamInfo 將 StreamInfo 返回池中
func releaseStreamInfo(s *StreamInfo) {
	if s == nil {
		return
	}
	s.reset()
	streamInfoPool.Put(s)
}

// reset 重置 StreamInfo
func (s *StreamInfo) reset() {
	s.StreamID = 0
	s.Priority = 0
	s.Dependencies = s.Dependencies[:0]
	s.Weight = 0
	s.Exclusive = false
}

// ===== QuicConnection 池操作 =====

// acquireQuicConnection 從池中獲取 QuicConnection
func acquireQuicConnection() *QuicConnection {
	q := quicConnPool.Get().(*QuicConnection)
	q.reset()
	return q
}

// releaseQuicConnection 將 QuicConnection 返回池中
func releaseQuicConnection(q *QuicConnection) {
	if q == nil {
		return
	}
	q.reset()
	quicConnPool.Put(q)
}

// reset 重置 QuicConnection
func (q *QuicConnection) reset() {
	q.conn = nil
	q.streamID = 0
	q.priority = 0
	q.rtt = 0
	q.congWin = 0
	q.bytesRead = 0
}

// ===== 緩衝區池操作 =====

// AcquireBuffer 從池中獲取緩衝區
func AcquireBuffer() *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// ReleaseBuffer 將緩衝區返回池中
func ReleaseBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// 避免內存洩漏，限制緩衝區大小
	if buf.Cap() > 64*1024 { // 64KB
		return
	}
	buf.Reset()
	bufferPool.Put(buf)
}

// ===== URL Values 池操作 =====

// AcquireURLValues 從池中獲取 URL Values
func AcquireURLValues() url.Values {
	v := urlValuesPool.Get().(url.Values)
	// 清空但保留容量
	for k := range v {
		delete(v, k)
	}
	return v
}

// ReleaseURLValues 將 URL Values 返回池中
func ReleaseURLValues(v url.Values) {
	if v == nil {
		return
	}
	// 避免內存洩漏
	if len(v) > 128 {
		return
	}
	urlValuesPool.Put(v)
}

// ===== Context 池化方法更新 =====

// initQuicConnectionFromPool 使用池初始化 QUIC 連接
func (c *Context) initQuicConnectionFromPool() {
	// 從請求中提取 QUIC 連接資訊
	if conn, ok := c.Request.Context().Value("quic_conn").(*QuicConnection); ok {
		c.quicConn = conn
	} else {
		// 從池中獲取
		c.quicConn = acquireQuicConnection()
	}

	// 從池中獲取 StreamInfo
	c.streamInfo = acquireStreamInfo()
	c.streamInfo.StreamID = c.extractStreamID()
	c.streamInfo.Priority = c.extractPriority()
}

// ===== 優化的 JSON 處理 =====

// JSONWithPool 使用物件池優化的 JSON 回應
func (c *Context) JSONWithPool(code int, obj interface{}) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(code)

	// 從池中獲取緩衝區
	buf := AcquireBuffer()
	defer ReleaseBuffer(buf)

	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(obj); err != nil {
		c.Error(err)
		return
	}

	c.Response.Write(buf.Bytes())
}

// ===== 優化的查詢參數處理 =====

// GetQueryWithPool 使用池優化的查詢參數獲取
func (c *Context) GetQueryWithPool(key string) (string, bool) {
	if c.queryCache == nil {
		// 從池中獲取 Values
		c.queryCache = AcquireURLValues()
		// 解析查詢參數
		for k, v := range c.Request.URL.Query() {
			c.queryCache[k] = v
		}
	}
	values := c.queryCache[key]
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// ===== 池狀態監控 =====

// PoolStats 池統計信息
type PoolStats struct {
	ContextPoolSize        int
	ResponseWriterPoolSize int
	MetricsPoolSize        int
	BufferPoolSize         int
}

// GetPoolStats 獲取池統計信息（僅用於調試）
func GetPoolStats() PoolStats {
	// 注意：sync.Pool 沒有直接的方法獲取池大小
	// 這裡只是示例，實際使用中可能需要自行維護計數器
	return PoolStats{
		// 需要自行實現計數邏輯
	}
}

// ===== 效能最佳化建議 =====

/*
使用物件池的最佳實踐：

1. 在處理請求時：
   c := AcquireContext(w, r)
   defer ReleaseContext(c)

2. 使用緩衝區池處理大量資料：
   buf := AcquireBuffer()
   defer ReleaseBuffer(buf)

3. 定期監控池的使用情況，避免內存洩漏

4. 對於大物件，設置容量上限，超過上限不返回池中

5. 在高並發場景下，物件池可以顯著減少 GC 壓力
*/
