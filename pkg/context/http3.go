package context

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// ===== HTTP/3 QUIC 優化方法 =====

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
	// 這裡需要根據實際的 QUIC 實現來提取
	if c.Request.Context() != nil {
		if streamID, ok := c.Request.Context().Value("stream_id").(uint64); ok {
			return streamID
		}
	}
	return 0
}

// extractPriority 提取優先級
func (c *Context) extractPriority() uint8 {
	// 實現優先級提取邏輯
	if c.Request.Context() != nil {
		if priority, ok := c.Request.Context().Value("stream_priority").(uint8); ok {
			return priority
		}
	}
	// 檢查 Priority header (RFC 9218)
	if priority := c.GetHeader("Priority"); priority != "" {
		// 解析 priority header
		return parsePriority(priority)
	}
	return 0
}

// SetStreamPriority 設置流優先級（HTTP/3 特性）
func (c *Context) SetStreamPriority(priority uint8) error {
	if c.protocol != HTTP3 {
		return fmt.Errorf("stream priority only available in HTTP/3")
	}
	if c.streamInfo == nil {
		c.streamInfo = &StreamInfo{}
	}
	c.streamInfo.Priority = priority

	// 實際設置 QUIC 流優先級
	// 這裡需要調用底層 QUIC 實現的 API
	if c.quicConn != nil && c.quicConn.conn != nil {
		// 設置優先級的實際實現
		// c.quicConn.conn.SetStreamPriority(c.streamInfo.StreamID, priority)
	}

	return nil
}

// GetStreamPriority 獲取流優先級
func (c *Context) GetStreamPriority() uint8 {
	if c.streamInfo != nil {
		return c.streamInfo.Priority
	}
	return 0
}

// SetStreamWeight 設置流權重
func (c *Context) SetStreamWeight(weight uint8) error {
	if c.protocol != HTTP3 && c.protocol != HTTP2 {
		return fmt.Errorf("stream weight only available in HTTP/2 and HTTP/3")
	}
	if c.streamInfo == nil {
		c.streamInfo = &StreamInfo{}
	}
	c.streamInfo.Weight = weight
	return nil
}

// GetStreamWeight 獲取流權重
func (c *Context) GetStreamWeight() uint8 {
	if c.streamInfo != nil {
		return c.streamInfo.Weight
	}
	return 16 // 默認權重
}

// SetStreamDependency 設置流依賴
func (c *Context) SetStreamDependency(streamID uint64, exclusive bool) error {
	if c.protocol != HTTP3 && c.protocol != HTTP2 {
		return fmt.Errorf("stream dependency only available in HTTP/2 and HTTP/3")
	}
	if c.streamInfo == nil {
		c.streamInfo = &StreamInfo{}
	}
	c.streamInfo.Dependencies = append(c.streamInfo.Dependencies, streamID)
	c.streamInfo.Exclusive = exclusive
	return nil
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
	if c.quicConn == nil {
		c.quicConn = &QuicConnection{}
	}
	c.quicConn.rtt = rtt
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
	if c.quicConn == nil {
		c.quicConn = &QuicConnection{}
	}
	c.quicConn.congWin = size
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
	if c.quicConn == nil {
		c.quicConn = &QuicConnection{}
	}
	c.quicConn.bytesRead += bytes
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

// Push 使用 HTTP/3 或 HTTP/2 Server Push
func (c *Context) Push(target string, opts *http.PushOptions) error {
	if c.protocol != HTTP3 && c.protocol != HTTP2 {
		return fmt.Errorf("server push only available in HTTP/2 and HTTP/3")
	}

	pusher, ok := c.Writer.(http.Pusher)
	if !ok {
		return fmt.Errorf("server push not supported")
	}

	return pusher.Push(target, opts)
}

// CanPush 檢查是否支援 Server Push
func (c *Context) CanPush() bool {
	if c.protocol != HTTP3 && c.protocol != HTTP2 {
		return false
	}
	_, ok := c.Writer.(http.Pusher)
	return ok
}

// PushResources 推送多個資源
func (c *Context) PushResources(resources map[string]*http.PushOptions) []error {
	if !c.CanPush() {
		return []error{fmt.Errorf("server push not supported")}
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

// IsEarlyData 檢查是否為 0-RTT 早期數據
func (c *Context) IsEarlyData() bool {
	if c.protocol != HTTP3 {
		return false
	}
	// 檢查 Early-Data header
	return c.GetHeader("Early-Data") == "1"
}

// AcceptEarlyData 接受早期數據
func (c *Context) AcceptEarlyData() {
	if c.protocol == HTTP3 {
		c.Header("Early-Data-Accepted", "1")
	}
}

// ===== Stream 控制 =====

// Stream 串流回應（支援 HTTP/3 優化）
func (c *Context) Stream(step func(w io.Writer) bool) bool {
	w := c.Writer
	for {
		if !step(w) {
			return false
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// HTTP/3 優化：調整流控制
		if c.protocol == HTTP3 && c.quicConn != nil {
			c.adaptiveStreamControl()
		}
	}
}

// adaptiveStreamControl HTTP/3 自適應流控制
func (c *Context) adaptiveStreamControl() {
	if c.quicConn == nil {
		return
	}

	// 根據 RTT 動態調整發送速率
	rtt := c.quicConn.rtt
	switch {
	case rtt > 200*time.Millisecond:
		// 高延遲：降低發送頻率
		time.Sleep(20 * time.Millisecond)
	case rtt > 100*time.Millisecond:
		// 中等延遲：適度調整
		time.Sleep(10 * time.Millisecond)
	case rtt > 50*time.Millisecond:
		// 低延遲：小幅調整
		time.Sleep(5 * time.Millisecond)
	default:
		// 極低延遲：無需調整
	}

	// 根據擁塞窗口調整
	if c.quicConn.congWin > 0 && c.quicConn.bytesRead > uint64(c.quicConn.congWin)*3/4 {
		// 接近擁塞窗口限制，降速
		time.Sleep(10 * time.Millisecond)
	}
}

// StreamWithPriority 帶優先級的串流
func (c *Context) StreamWithPriority(priority uint8, step func(w io.Writer) bool) error {
	if err := c.SetStreamPriority(priority); err != nil {
		return err
	}
	c.Stream(step)
	return nil
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

// EnableDatagrams 啟用 QUIC 數據報
func (c *Context) EnableDatagrams() error {
	if c.protocol != HTTP3 {
		return fmt.Errorf("datagrams only available in HTTP/3")
	}
	// 實際啟用數據報的實現
	c.Header("Datagram-Flow-ID", "1")
	return nil
}

// SendDatagram 發送 QUIC 數據報
func (c *Context) SendDatagram(data []byte) error {
	if c.protocol != HTTP3 {
		return fmt.Errorf("datagrams only available in HTTP/3")
	}
	if c.quicConn == nil || c.quicConn.conn == nil {
		return fmt.Errorf("no QUIC connection available")
	}
	// 實際發送數據報的實現
	// return c.quicConn.conn.SendDatagram(data)
	return fmt.Errorf("datagram sending not implemented")
}

// ===== 輔助函數 =====

// parsePriority 解析 Priority header
func parsePriority(header string) uint8 {
	// 簡化的 Priority header 解析
	// 實際應該按照 RFC 9218 解析
	// u=3, i
	// urgency=3, incremental=true

	// 默認返回中等優先級
	return 3
}
