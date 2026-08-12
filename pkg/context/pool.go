// @chris
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
	// Context 物件池（Params/Keys 延遲初始化：Params 每請求由
	// AcquireParams 供給、Keys 由 Set() 首次使用時建立）
	contextPool = &sync.Pool{
		New: func() interface{} {
			return &Context{
				handlers: make([]HandlerFunc, 0, 8),
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
	rw := acquireResponseWriter(w)
	c.rw = rw // 記住自己擁有的原始 writer，供 Release 歸還
	c.Response = rw
	c.Writer = rw // Gin 兼容別名，必須設置
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
	// released 檢查讓重複呼叫成為 no-op（見 Context.released 說明）
	if c == nil || c.released {
		return
	}

	// 釋放子物件。歸還 c.rw（本 Context 擁有的原始 writer）而非對
	// c.Response 做型別斷言——中間件可能已把 Response 換成包裝器
	// （如 Compression 的 gzipWriter），斷言會 panic。
	releaseResponseWriter(c.rw)
	if c.metrics != nil {
		releaseMetrics(c.metrics)
	}
	if c.streamInfo != nil {
		releaseStreamInfo(c.streamInfo)
	}
	if c.quicConn != nil {
		releaseQuicConnection(c.quicConn)
	}

	// 清理並返回池中（reset 會把 released 歸零，故在其後標記）
	c.reset()
	c.released = true
	contextPool.Put(c)
}

// reset 重置 Context
func (c *Context) reset() {
	c.Request = nil
	c.Response = nil
	c.Writer = nil // 與 Response 同為別名，漏清會殘留已歸還 writer 的指標
	c.rw = nil
	c.quicConn = nil
	c.streamInfo = nil
	c.metrics = nil // 漏清會讓 double-release 把同一 metrics Put 進池兩次

	// Params 歸還池中：router 每請求經 AcquireParams 取得新 slice 蓋掉
	// 本欄位。先前只截斷（[:0]）不歸還，ReleaseParams 全 module 零呼叫
	// 者——pool 有進無出、永遠 miss，每個帶參數請求照樣分配
	if c.Params != nil {
		ReleaseParams(c.Params)
		c.Params = nil
	}
	// 清理切片但保留容量
	c.handlers = c.handlers[:0]
	c.Errors = c.Errors[:0]

	// Keys 延遲初始化：Set() 已有 nil 檢查、讀 nil map 合法。
	// 先前每次 reset 重建 make(map,8)，而 reset 在 Acquire 與 Release
	// 各跑一次——未用到 Keys 的請求（多數）也付 2 次 map 分配
	c.Keys = nil

	// 清理快取：直接置 nil，下次使用時延遲初始化。
	// rawData 有 memo 語義（GetRawData/ShouldBindBodyWith 會先看它），
	// 漏清會讓下一個請求讀到上一個請求的完整 body —— 跨請求資料洩漏
	c.queryCache = nil
	c.formCache = nil
	c.rawData = nil

	c.index = -1
	c.protocol = 0
	c.startTime = time.Time{}
	c.fullPath = ""    // 漏清會讓監控/日誌把本請求歸到上一個請求的路由
	c.routerGroup = nil

	// 內容協商與 cookie 設定：漏清 sameSite 會讓上一個請求設的
	// SameSite=None 套用到下一個請求的 session cookie（CSRF 防護失效）
	c.Accepted = nil
	c.sameSite = 0

	// Schema-first 綁定狀態
	c.schemaInput = nil
	c.schemaRouteKey = ""
	c.bindInputCalled = false

	c.released = false
}

// ===== ResponseWriter 池操作 =====

// acquireResponseWriter 從池中獲取 responseWriter（回傳具體型別，供 Context 記住所有權）
func acquireResponseWriter(w http.ResponseWriter) *responseWriter {
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

// ===== Params 池操作 =====

// paramsPool 路由參數池，避免每請求分配新 Params slice
var paramsPool = &sync.Pool{
	New: func() interface{} {
		p := make(Params, 0, 8)
		return &p
	},
}

// AcquireParams 從池中獲取 Params slice
func AcquireParams(size int) Params {
	pp := paramsPool.Get().(*Params)
	p := *pp
	if cap(p) >= size {
		return p[:size]
	}
	// 容量不足時重新分配
	return make(Params, size)
}

// ReleaseParams 將 Params 歸還池中
func ReleaseParams(p Params) {
	if cap(p) > 32 {
		return // 過大的不歸還
	}
	p = p[:0]
	paramsPool.Put(&p)
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
// GC 優化：重建 map 替代逐一 delete
func AcquireURLValues() url.Values {
	// ponytail: 直接配置即可；先前的 urlValuesPool.Get() 取出後丟棄，是無效操作
	return make(url.Values, 8)
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
