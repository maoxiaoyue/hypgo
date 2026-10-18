// @chris
package context

import (
	"bytes"
	"net/http"
	"sync"
	"time"
)

// ===== 全域物件池 =====

// paramsBufCap 每個 Context 自帶的路由參數緩衝容量。router 直接在這塊緩衝上
// search，參數數 ≤ 此值的路由每請求零分配；超過時 search 自行擴容（僅該請求
// 多一次分配，下次 reset 仍回到自帶緩衝）。
const paramsBufCap = 8

var (
	// Context 物件池。與 Context 生命週期完全相同的子物件（responseWriter、
	// RequestMetrics、QUIC 資訊、路由參數緩衝）直接內嵌在 Context 裡隨之池化，
	// 不再各自走一個 sync.Pool——分開池化只是每請求多數組 Get/Put。
	// Keys 刻意不重用，見 reset 說明。
	contextPool = &sync.Pool{
		New: func() interface{} {
			return &Context{
				handlers:  make([]HandlerFunc, 0, 8),
				Errors:    make(errorMsgs, 0, 4),
				paramsBuf: make(Params, 0, paramsBufCap),
			}
		},
	}

	// 緩衝區池（用於 JSON 編碼等）
	bufferPool = &sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 1024))
		},
	}
)

// ===== Context 池操作 =====

// AcquireContext 從池中獲取 Context
func AcquireContext(w http.ResponseWriter, r *http.Request) *Context {
	c := contextPool.Get().(*Context)
	c.reset()
	c.Request = r
	c.ownWriter.ResponseWriter = w
	c.ownWriter.status = http.StatusOK
	c.Response = &c.ownWriter
	c.Writer = &c.ownWriter // Gin 兼容別名，必須設置
	c.startTime = time.Now()

	// 檢測並設置協議
	c.detectProtocol()

	return c
}

// ReleaseContext 將 Context 返回池中
func ReleaseContext(c *Context) {
	// released 檢查讓重複呼叫成為 no-op（見 Context.released 說明）
	if c == nil || c.released {
		return
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
	c.Writer = nil // 與 Response 同為別名，漏清會殘留上一個請求的 writer 指標
	// 重置自有 writer 而非對 c.Response 做型別斷言——中間件可能已把
	// Response 換成包裝器（如 Compression 的 gzipWriter），斷言會 panic
	c.ownWriter.reset()
	c.metrics = RequestMetrics{}

	// Params 一律指回自帶緩衝：router 直接在上面 search，帶參數路由零分配。
	// 不沿用 c.Params 的底層陣列——它是 exported 欄位，可能被設成外部 slice，
	// 沿用會讓下一個請求寫進別人的陣列
	c.Params = c.paramsBuf[:0]
	// 清理切片但保留容量
	c.handlers = c.handlers[:0]
	c.Errors = c.Errors[:0]

	// Keys 延遲初始化：Set() 已有 nil 檢查、讀 nil map 合法。
	// 刻意不清空重用：使用者若在請求結束後仍持有 c.Keys（例如交給非同步
	// logger），重用會讓它與下一個請求並行讀寫同一個 map——Go 對此直接
	// fatal error 終止行程，recover 也救不回來；置 nil 讓舊 map 留給持有者
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
	c.fullPath = "" // 漏清會讓監控/日誌把本請求歸到上一個請求的路由
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

// reset 重置 responseWriter
func (w *responseWriter) reset() {
	w.ResponseWriter = nil
	w.status = 0
	w.size = 0
	w.written = false
	w.streamID = 0
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
