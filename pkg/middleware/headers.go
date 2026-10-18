// @chris
package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// ===== 壓縮中間件 =====

// CompressionConfig 壓縮配置
type CompressionConfig struct {
	Level         int      // gzip 壓縮等級（1–9；0 = 預設）
	MinLength     int      // 小於此長度（bytes）不壓縮，預設 1024
	ExcludedPaths []string // 不壓縮的路徑
	ExcludedTypes []string // 不壓縮的 Content-Type 前綴（如 "image/"、"video/"）
}

// Compression 創建 gzip 壓縮中間件。
//
// 先前的實作：Level / MinLength / ExcludedTypes 全未生效（2 bytes 的回應也壓成 26 bytes）、
// 204 等無 body 回應也帶 Content-Encoding、不支援 Flush（SSE 卡在 gzip 緩衝）、
// "gzip;q=0" 被當成接受、且每個請求新建一個 gzip.Writer。
// 現在 body 先緩衝到 MinLength 才決定壓縮；不足即原樣送出
func Compression(config CompressionConfig) hypcontext.HandlerFunc {
	level := config.Level
	if level == 0 {
		level = gzip.DefaultCompression
	}
	if config.MinLength == 0 {
		config.MinLength = 1024
	}
	excludedPaths := make(map[string]bool)
	for _, path := range config.ExcludedPaths {
		excludedPaths[path] = true
	}
	excludedTypes := make([]string, len(config.ExcludedTypes))
	for i, t := range config.ExcludedTypes {
		excludedTypes[i] = strings.ToLower(t)
	}
	pool := &sync.Pool{New: func() interface{} {
		gz, err := gzip.NewWriterLevel(io.Discard, level)
		if err != nil { // 等級超出範圍時退回預設
			gz = gzip.NewWriter(io.Discard)
		}
		return gz
	}}

	return func(c *hypcontext.Context) {
		if excludedPaths[c.Request.URL.Path] || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}
		// 回應內容依 Accept-Encoding 而定，無論這次是否壓縮都要告知快取
		c.Writer.Header().Add("Vary", "Accept-Encoding")
		if !acceptsGzip(c.GetHeader("Accept-Encoding")) {
			c.Next()
			return
		}

		cw := &compressWriter{ResponseWriter: c.Writer, minLen: config.MinLength, excludedTypes: excludedTypes, pool: pool}
		c.Writer, c.Response = cw, cw
		defer func() {
			if p := recover(); p != nil {
				cw.discard() // panic：不送出緩衝內容，交給外層 Recovery 回 500
				panic(p)
			}
			cw.finish()
		}()
		c.Next()
	}
}

// acceptsGzip 依 Accept-Encoding 判斷客戶端是否接受 gzip（q=0 表示明確拒絕）
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		for _, param := range strings.Split(params, ";") {
			if k, v, ok := strings.Cut(strings.TrimSpace(param), "="); ok && strings.EqualFold(k, "q") {
				q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
				return err == nil && q > 0
			}
		}
		return true
	}
	return false
}

// compressWriter 延後決定是否壓縮：body 累積到 minLen 才開始 gzip，不足則原樣送出
type compressWriter struct {
	hypcontext.ResponseWriter

	minLen        int
	excludedTypes []string
	pool          *sync.Pool

	buf     []byte       // 決定前的緩衝
	gz      *gzip.Writer // 決定壓縮後非 nil
	decided bool
}

// compressible 依狀態碼與已設定的標頭判斷這個回應能否壓縮
func (w *compressWriter) compressible() bool {
	switch status := w.Status(); {
	case status < 200, status == http.StatusNoContent, status == http.StatusNotModified:
		return false
	}
	h := w.Header()
	if h.Get("Content-Encoding") != "" {
		return false
	}
	ct := strings.ToLower(h.Get("Content-Type"))
	for _, t := range w.excludedTypes {
		if strings.HasPrefix(ct, t) {
			return false
		}
	}
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n < w.minLen {
			return false
		}
	}
	return true
}

// startGzip 設定壓縮標頭並把已緩衝的內容寫進 gzip
func (w *compressWriter) startGzip() error {
	h := w.Header()
	if h.Get("Content-Type") == "" {
		// 未設 Content-Type 時 net/http 會嗅探 body；壓縮後嗅探會變成 application/x-gzip
		h.Set("Content-Type", http.DetectContentType(w.buf))
	}
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")

	w.gz = w.pool.Get().(*gzip.Writer)
	w.gz.Reset(w.ResponseWriter)
	w.decided = true
	_, err := w.gz.Write(w.buf)
	w.buf = nil
	return err
}

// passthrough 決定不壓縮，送出已緩衝的內容
func (w *compressWriter) passthrough() error {
	w.decided = true
	if len(w.buf) == 0 {
		return nil
	}
	_, err := w.ResponseWriter.Write(w.buf)
	w.buf = nil
	return err
}

func (w *compressWriter) Write(p []byte) (int, error) {
	if !w.decided {
		if !w.compressible() {
			if err := w.passthrough(); err != nil {
				return 0, err
			}
			return w.ResponseWriter.Write(p)
		}
		w.buf = append(w.buf, p...)
		if len(w.buf) >= w.minLen {
			if err := w.startGzip(); err != nil {
				return 0, err
			}
		}
		return len(p), nil
	}
	if w.gz != nil {
		return w.gz.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

func (w *compressWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

// WriteHeaderNow 在決定前就要送出標頭（如 204、或明確提前送標頭）：之後無法再加
// Content-Encoding，因此改為不壓縮
func (w *compressWriter) WriteHeaderNow() {
	if !w.decided {
		w.passthrough()
	}
	w.ResponseWriter.WriteHeaderNow()
}

// Flush 支援串流（SSE / c.Stream）：決定壓縮與否，並把 gzip 緩衝推到客戶端
func (w *compressWriter) Flush() {
	if !w.decided {
		if len(w.buf) > 0 && w.compressible() {
			w.startGzip()
		} else {
			w.passthrough()
		}
	}
	if w.gz != nil {
		w.gz.Flush()
	}
	w.ResponseWriter.Flush()
}

// finish 請求結束：未達 minLen 的內容原樣送出；壓縮中則收尾並歸還 gzip.Writer
func (w *compressWriter) finish() {
	if !w.decided {
		w.passthrough()
		return
	}
	if w.gz != nil {
		w.gz.Close()
		w.pool.Put(w.gz)
		w.gz = nil
	}
}

// discard panic 時丟棄緩衝；gzip.Writer 下次取出時會 Reset，可直接歸還
func (w *compressWriter) discard() {
	w.buf = nil
	if w.gz != nil {
		w.pool.Put(w.gz)
		w.gz = nil
	}
}

// ===== 請求 ID 中間件 =====

// RequestIDConfig 請求 ID 配置
type RequestIDConfig struct {
	Header    string
	Generator func() string
}

// RequestID 創建請求 ID 中間件
func RequestID(config RequestIDConfig) hypcontext.HandlerFunc {
	if config.Header == "" {
		config.Header = "X-Request-ID"
	}

	if config.Generator == nil {
		config.Generator = generateRequestID
	}

	return func(c *hypcontext.Context) {
		// 沿用客戶端帶來的 ID 前先驗證：先前任意長度與字元都原樣回傳並寫進 log
		requestID := c.GetHeader(config.Header)
		if !validRequestID(requestID) {
			requestID = config.Generator()
		}

		// 設置請求 ID
		c.Set("request_id", requestID)
		c.Header(config.Header, requestID)

		c.Next()
	}
}

// validRequestID 只接受 1–128 個 [A-Za-z0-9-_.:] 字元
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		switch ch := id[i]; {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == ':':
		default:
			return false
		}
	}
	return true
}

// generateRequestID 生成安全的請求 ID
func generateRequestID() string {
	return secureRandHex(16)
}
