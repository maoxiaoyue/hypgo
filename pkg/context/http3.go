// @chris
package context

import (
	stdcontext "context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ===== HTTP/3 QUIC 優化方法 =====

// initQuicConnection 初始化 HTTP/3 請求的 QUIC 資訊（使用 Context 自有物件，零分配）
func (c *Context) initQuicConnection() {
	if conn, ok := c.Request.Context().Value("quic_conn").(*QuicConnection); ok {
		c.quicConn = conn // 連線層共用物件：只參照，reset 不會重置它
	} else {
		c.quicConn = &c.ownQuicConn
	}

	c.streamInfo = &c.ownStreamInfo
	c.streamInfo.StreamID = c.extractStreamID()
}

// ensureQuicConn 確保 quicConn 可寫；未設定時指向 Context 自有物件
func (c *Context) ensureQuicConn() *QuicConnection {
	if c.quicConn == nil {
		c.quicConn = &c.ownQuicConn
	}
	return c.quicConn
}

// extractStreamID 提取流 ID
func (c *Context) extractStreamID() uint64 {
	// 實現從請求中提取流 ID 的邏輯
	// 這裡需要根據實際的 QUIC 實現來提取
	if c.Request.Context() != nil {
		if streamID, ok := c.Request.Context().Value("stream_id").(uint64); ok {
			return streamID
		}
	}
	return 0
}

// GetStreamID 獲取流 ID
func (c *Context) GetStreamID() uint64 {
	if c.streamInfo != nil {
		return c.streamInfo.StreamID
	}
	return 0
}

// GetRTT 獲取往返時間（對 HTTP/3 優化）
func (c *Context) GetRTT() time.Duration {
	if c.quicConn != nil {
		return c.quicConn.rtt
	}
	return 0
}

// SetRTT 設置 RTT（用於測試或手動設置）
func (c *Context) SetRTT(rtt time.Duration) {
	c.ensureQuicConn().rtt = rtt
}

// GetCongestionWindow 獲取擁塞窗口大小
func (c *Context) GetCongestionWindow() uint32 {
	if c.quicConn != nil {
		return c.quicConn.congWin
	}
	return 0
}

// SetCongestionWindow 設置擁塞窗口（用於測試或手動設置）
func (c *Context) SetCongestionWindow(size uint32) {
	c.ensureQuicConn().congWin = size
}

// GetBytesRead 獲取已讀取的字節數
func (c *Context) GetBytesRead() uint64 {
	if c.quicConn != nil {
		return c.quicConn.bytesRead
	}
	return 0
}

// UpdateBytesRead 更新已讀取的字節數
func (c *Context) UpdateBytesRead(bytes uint64) {
	c.ensureQuicConn().bytesRead += bytes
}

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

// PushResources 推送多個資源
func (c *Context) PushResources(resources map[string]*http.PushOptions) []error {
	if !c.CanPush() {
		return []error{http.ErrNotSupported}
	}

	var errors []error
	for target, opts := range resources {
		if err := c.Push(target, opts); err != nil {
			errors = append(errors, fmt.Errorf("failed to push %s: %w", target, err))
		}
	}
	return errors
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

// GetQUICConnection 獲取 QUIC 連接資訊
func (c *Context) GetQUICConnection() *QuicConnection {
	return c.quicConn
}

// SetQUICConnection 設置 QUIC 連接資訊
func (c *Context) SetQUICConnection(conn *QuicConnection) {
	c.quicConn = conn
}

// GetStreamInfo 獲取流資訊
func (c *Context) GetStreamInfo() *StreamInfo {
	return c.streamInfo
}

// SetStreamInfo 設置流資訊
func (c *Context) SetStreamInfo(info *StreamInfo) {
	c.streamInfo = info
}
