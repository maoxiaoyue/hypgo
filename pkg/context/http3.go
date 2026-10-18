// @chris
package context

import (
	stdcontext "context"
	"io"
	"net/http"
)

// ===== 協議判斷 =====

// IsHTTP3 檢查是否為 HTTP/3
func (c *Context) IsHTTP3() bool {
	return c.protocol == HTTP3
}

// IsHTTP2 檢查是否為 HTTP/2
func (c *Context) IsHTTP2() bool {
	return c.protocol == HTTP2
}

// IsHTTP1 檢查是否為 HTTP/1.x
func (c *Context) IsHTTP1() bool {
	return c.protocol == HTTP1
}

// ===== Server Push =====

// Push 發出 HTTP/2 Server Push。只有 HTTP/2 支援：HTTP/1.1 沒有 push，
// quic-go 的 HTTP/3 也未實作；客戶端關閉 push（SETTINGS_ENABLE_PUSH=0）時
// net/http 同樣回 http.ErrNotSupported。
func (c *Context) Push(target string, opts *http.PushOptions) error {
	if p, ok := c.ownWriter.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// CanPush 回報底層連線是否支援 Server Push（實際上只有 HTTP/2）。
// 檢查底層 writer 而非 c.Writer：後者是 HypGo 的包裝，恆實作 http.Pusher。
func (c *Context) CanPush() bool {
	_, ok := c.ownWriter.ResponseWriter.(http.Pusher)
	return ok
}

// ===== 0-RTT (Early Data) =====

// handshakeKey 存放 QUIC 連線的握手完成通道（見 WithHandshakeComplete）
type handshakeKey struct{}

// WithHandshakeComplete 把 QUIC 連線的 HandshakeComplete() 通道放進連線 context，
// 供 IsEarlyData 判斷請求是否在握手完成前（0-RTT）送達。
// 由 server 在 http3.Server.ConnContext 呼叫；quic-go 的請求 context 衍生自連線 context。
func WithHandshakeComplete(ctx stdcontext.Context, done <-chan struct{}) stdcontext.Context {
	return stdcontext.WithValue(ctx, handshakeKey{}, done)
}

// IsEarlyData 回報本請求是否為 0-RTT 早期資料——可被攻擊者原封重放。
// 非冪等操作遇到 true 應回 425，客戶端會在握手完成後重送（RFC 8470）：
//
//	if c.IsEarlyData() { c.AbortWithStatus(http.StatusTooEarly); return }
//
// 兩種來源，與本請求的協議無關：
//   - 經代理：轉發早期資料的代理會加 Early-Data: 1（RFC 8470 §5.1），後端收到的
//     常是 HTTP/1.1 或 HTTP/2。客戶端自行偽造只會讓自己被拒，不需可信代理即可採信
//   - 直連 HTTP/3：quic-go 收到 0-RTT 請求立即派發、不等握手。握手未完成即為早期
//     資料；重放的 0-RTT 永遠無法完成握手，所以握手已完成的連線不會是重放
func (c *Context) IsEarlyData() bool {
	if c.Request == nil {
		return false
	}
	if c.Request.Header.Get(HeaderEarlyData) == "1" {
		return true
	}
	done, ok := c.Request.Context().Value(handshakeKey{}).(<-chan struct{})
	if !ok {
		return false // 非 HTTP/3，或 server 未注入（無法得知，視為已完成）
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

// ===== Stream 控制 =====

// Stream 串流回應（支援 HTTP/3 優化）。
// 回傳 true 表示客戶端已中斷連線，false 表示 step 自行結束串流。
//
// 每輪都檢查請求 context：少了這個檢查，客戶端關閉分頁後 step 仍會
// 被永遠呼叫下去（多數 step 實作會忽略 Write 錯誤），goroutine 永不退出、
// Context 也永不歸還池——每個斷線的客戶端漏一條 goroutine，可被當 DoS 向量。
func (c *Context) Stream(step func(w io.Writer) bool) bool {
	w := c.Writer
	var done <-chan struct{}
	if c.Request != nil {
		done = c.Request.Context().Done()
	}

	for {
		select {
		case <-done:
			return true // 客戶端已離線
		default:
		}

		if !step(w) {
			return false
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// ===== QUIC 特定功能 =====
