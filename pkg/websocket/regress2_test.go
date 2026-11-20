package websocket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

// closeCode 讀到連線結束，回傳 close frame 的代碼（不是 close 時回 0）
func closeCode(c *gws.Conn) int {
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			if ce, ok := err.(*gws.CloseError); ok {
				return ce.Code
			}
			return 0
		}
	}
}

// TestMessageSizeLimitCountsDecompressedBytes：MaxMessageSize 以解壓後的大小計算。
// 先前只靠 SetReadLimit（算壓縮後的位元組）：協商 permessage-deflate 後 200MB 的訊息照樣進到 onMessage
func TestMessageSizeLimitCountsDecompressedBytes(t *testing.T) {
	var got atomic.Int64
	hub, srv, _ := wsServer(t, Config{EnableCompression: true, MaxMessageSize: 64 << 10}, true)
	hub.SetCallbacks(nil, nil, func(_ *Client, m *Message) { got.Add(int64(len(m.Data))) })

	d := &gws.Dialer{EnableCompression: true}
	c, _, err := d.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// 8MB 的重複字元，壓縮後遠小於 64KB
	big := `{"type":"x","data":"` + strings.Repeat("a", 8<<20) + `"}`
	// 伺服器讀到上限就關閉連線，還沒讀完的資料可能讓客戶端看到的是連線重設而不是 close frame
	c.WriteMessage(gws.TextMessage, []byte(big))
	if code := closeCode(c); code != gws.CloseMessageTooBig && code != 0 {
		t.Errorf("close code = %d, want %d (message too big)", code, gws.CloseMessageTooBig)
	}
	time.Sleep(100 * time.Millisecond)
	if n := got.Load(); n != 0 {
		t.Errorf("onMessage received %d bytes of an oversized message", n)
	}

	// 上限內的壓縮訊息照常處理
	c2, _, err := d.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	c2.WriteMessage(gws.TextMessage, []byte(`{"type":"x","data":"`+strings.Repeat("a", 32<<10)+`"}`))
	eventually(t, "message within the limit", func() bool { return got.Load() > 32<<10 })
}

var testKey = []byte("0123456789abcdef0123456789abcdef")

// replayDial 連線並完成 hello
func replayDial(t *testing.T, url string, sec *SecurityConfig) (*gws.Conn, *ClientSession) {
	t.Helper()
	c, _, err := gws.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	s, err := NewClientSession(sec.AESKey, sec.HMACKey, sec.SignThenEncrypt)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, frame, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if mt != gws.BinaryMessage {
		t.Errorf("hello frame type = %d, want binary", mt)
	}
	if _, hello, err := s.Open(frame); err != nil || !hello {
		t.Fatalf("hello: hello=%v err=%v", hello, err)
	}
	return c, s
}

// TestReplayProtection：開啟後，送回伺服器的訊息、重送的訊息、別條連線的訊息都不被接受。
// 先前（與未開啟時）不知道金鑰的連線把收到的廣播原樣送回，所有人就再收到一次
func TestReplayProtection(t *testing.T) {
	for name, mk := range map[string]func() *SecurityConfig{
		"hmac+aes sign-then-encrypt": func() *SecurityConfig {
			return &SecurityConfig{AESKey: testKey, HMACKey: testKey, SignThenEncrypt: true}
		},
		"hmac+aes encrypt-then-sign": func() *SecurityConfig { return &SecurityConfig{AESKey: testKey, HMACKey: testKey} },
		"hmac only":                  func() *SecurityConfig { return &SecurityConfig{HMACKey: testKey} },
		"aes only":                   func() *SecurityConfig { return &SecurityConfig{AESKey: testKey} },
	} {
		t.Run(name, func(t *testing.T) {
			sec := mk()
			sec.ReplayProtection = true
			var received atomic.Int64
			hub, srv, _ := wsServer(t, Config{Security: sec, MaxInvalidMessages: -1}, true)
			hub.SetCallbacks(nil, nil, func(*Client, *Message) { received.Add(1) })

			alice, as := replayDial(t, wsURL(srv), sec)
			mallory, ms := replayDial(t, wsURL(srv), sec)
			eventually(t, "two clients", func() bool { return hub.stats.ActiveConnections.Load() == 2 })

			// 正常往返：伺服器廣播 → 兩邊都解得開
			hub.Broadcast([]byte(`"price=100"`))
			alice.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, aFrame, err := alice.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if p, _, err := as.Open(aFrame); err != nil || !strings.Contains(string(p), "price=100") {
				t.Fatalf("alice could not open broadcast: %q %v", p, err)
			}
			mallory.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, mFrame, err := mallory.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(aFrame, mFrame) {
				t.Error("two connections received identical frames: not bound to the connection")
			}
			// 同一個 frame 再開一次（客戶端這一側的重放）
			if _, _, err := as.Open(aFrame); err == nil {
				t.Error("client accepted the same server frame twice")
			}

			// 客戶端 → 伺服器
			out, _ := as.Seal([]byte(`{"type":"chat","data":1}`))
			alice.WriteMessage(gws.BinaryMessage, out)
			eventually(t, "alice's message", func() bool { return received.Load() == 1 })

			// 1. 把伺服器送來的 frame 原樣送回（方向不同）
			mallory.WriteMessage(gws.BinaryMessage, mFrame)
			// 2. 同一條連線重送已送過的 frame（序號不新）
			alice.WriteMessage(gws.BinaryMessage, out)
			// 3. alice 的 frame 改由 mallory 的連線送（session 不同）
			mallory.WriteMessage(gws.BinaryMessage, out)
			// 4. alice 的下一則，序號竄改成更大的值
			next, _ := as.Seal([]byte(`{"type":"chat","data":2}`))
			tampered := append([]byte(nil), next...)
			tampered[7] += 5
			alice.WriteMessage(gws.BinaryMessage, tampered)
			time.Sleep(150 * time.Millisecond)
			if n := received.Load(); n != 1 {
				t.Errorf("server accepted %d messages, want 1 (replayed / reflected / tampered frames must be rejected)", n)
			}

			// 之後的正常訊息仍被接受（被拒絕的訊息不影響序號）
			alice.WriteMessage(gws.BinaryMessage, next)
			ok, _ := ms.Seal([]byte(`{"type":"chat","data":3}`))
			mallory.WriteMessage(gws.BinaryMessage, ok)
			eventually(t, "the two legitimate messages", func() bool { return received.Load() == 3 })
			time.Sleep(50 * time.Millisecond)
			if n := received.Load(); n != 3 {
				t.Errorf("server accepted %d messages, want 3 (replayed / reflected / tampered frames must be rejected)", n)
			}
		})
	}
}

// TestReplayProtectionKeepsOrderUnderConcurrency：序號在寫入時才編，並行送出的訊息到達時仍然遞增
func TestReplayProtectionKeepsOrderUnderConcurrency(t *testing.T) {
	sec := &SecurityConfig{AESKey: testKey, HMACKey: testKey, SignThenEncrypt: true, ReplayProtection: true}
	var id string
	hub, srv, _ := wsServer(t, Config{Security: sec}, true)
	hub.SetCallbacks(func(c *Client) { id = c.ID; c.Subscribe("news") }, nil, nil)
	c, s := replayDial(t, wsURL(srv), sec)
	eventually(t, "subscription", func() bool { return hub.GetStats()["total_channels"].(int) == 1 })

	const each = 60
	var wg sync.WaitGroup
	for _, send := range []func(){
		func() { hub.Broadcast([]byte(`1`)) },
		func() { hub.PublishToChannelRaw("news", []byte(`2`)) },
		func() { hub.SendToClient(id, &Message{Type: "direct"}) },
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				send()
			}
		}()
	}
	wg.Wait()
	for i := 0; i < 3*each; i++ {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, frame, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("after %d messages: %v", i, err)
		}
		payload, _, err := s.Open(frame)
		if err != nil {
			t.Fatalf("message %d rejected by the client: %v", i, err)
		}
		// 內容必須就是序列化後的訊息（只保護一次）
		if !json.Valid(payload) {
			t.Fatalf("message %d is not the plain serialized message after Open: %q", i, payload)
		}
	}
}

// TestReplayProtectionNeedsKey：沒有金鑰時開啟重放保護是設定錯誤
func TestReplayProtectionNeedsKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewHub accepted ReplayProtection without any key")
		}
	}()
	NewHub(logger.NewLogger(), Config{Security: &SecurityConfig{ReplayProtection: true}})
}

// TestSecuredFramesAreBinary：簽名 / 加密後的內容用 binary frame。
// 先前 JSON codec 一律 text frame，瀏覽器收到非 UTF-8 的 text frame 會關閉連線。
// 未開重放保護時格式不變：Verify 之後就是原本的 JSON
func TestSecuredFramesAreBinary(t *testing.T) {
	hub, srv, _ := wsServer(t, Config{Security: &SecurityConfig{HMACKey: testKey}}, true)
	c := mustDial(t, srv, nil)
	eventually(t, "client", func() bool { return hub.stats.ActiveConnections.Load() == 1 })
	hub.Broadcast([]byte(`"x"`))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, frame, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if mt != gws.BinaryMessage {
		t.Errorf("signed frame type = %d, want binary (%d)", mt, gws.BinaryMessage)
	}
	plain, err := Verify(frame, testKey)
	if err != nil || !json.Valid(plain) {
		t.Errorf("frame without ReplayProtection must keep the old format: %q %v", plain, err)
	}

	// 沒有安全管線時 JSON 仍是 text frame
	hub2, srv2, _ := wsServer(t, Config{}, true)
	c2 := mustDial(t, srv2, nil)
	eventually(t, "client", func() bool { return hub2.stats.ActiveConnections.Load() == 1 })
	hub2.Broadcast([]byte(`"x"`))
	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if mt, _, _ := c2.ReadMessage(); mt != gws.TextMessage {
		t.Errorf("plain JSON frame type = %d, want text", mt)
	}
}

// TestMaxSubscriptions：客戶端訊息能建立的訂閱有上限。先前一條連線可訂閱任意多個頻道
func TestMaxSubscriptions(t *testing.T) {
	var client *Client
	hub, srv, _ := wsServer(t, Config{MaxSubscriptions: 3}, true)
	hub.SetCallbacks(func(c *Client) { client = c }, nil, nil)
	c := mustDial(t, srv, nil)
	for i := 0; i < 2; i++ {
		c.WriteMessage(gws.TextMessage, []byte(fmt.Sprintf(`{"type":"subscribe","data":{"channel":"c%d"}}`, i)))
	}
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"join_room","data":{"room_id":"r0"}}`))
	// 已經有的不算新增
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"subscribe","data":{"channel":"c0"}}`))
	// 超過上限的兩個
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"subscribe","data":{"channel":"over"}}`))
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"join_room","data":{"room_id":"over"}}`))

	for i := 0; i < 2; i++ {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, b, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("expected an error message for the rejected request: %v", err)
		}
		var m Message
		json.Unmarshal(b, &m)
		if m.Type != "error" || m.Channel != "over" {
			t.Errorf("rejection message = %s", b)
		}
	}
	st := hub.GetStats()
	if st["total_channels"].(int) != 2 || st["total_rooms"].(int) != 1 {
		t.Errorf("channels=%v rooms=%v, want 2 and 1", st["total_channels"], st["total_rooms"])
	}
	// 伺服器端直接訂閱不受限
	client.Subscribe("server-side")
	if hub.GetStats()["total_channels"].(int) != 3 {
		t.Error("server-side Subscribe must not be limited")
	}
	// 退訂後可以再訂
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"unsubscribe","data":{"channel":"c0"}}`))
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"unsubscribe","data":{"channel":"server-side"}}`))
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"subscribe","data":{"channel":"again"}}`))
	eventually(t, "resubscribe after unsubscribe", func() bool {
		_, ok := hub.GetStats()["channels"].(map[string]int)["again"]
		return ok
	})
}

// TestInvalidMessagesCloseConnection：連續的無效訊息關閉連線。先前 2 萬則照收不誤
func TestInvalidMessagesCloseConnection(t *testing.T) {
	_, srv, _ := wsServer(t, Config{MaxInvalidMessages: 3}, true)
	c := mustDial(t, srv, nil)
	// 中間有一則正常訊息時重新計數
	for _, m := range []string{"x", "x", `{"type":"ok"}`, "x", "x"} {
		c.WriteMessage(gws.TextMessage, []byte(m))
	}
	c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := c.ReadMessage(); err != nil {
		if _, closed := err.(*gws.CloseError); closed {
			t.Fatal("closed before 3 consecutive invalid messages")
		}
	}
	c2 := mustDial(t, srv, nil)
	for i := 0; i < 3; i++ {
		c2.WriteMessage(gws.TextMessage, []byte("x"))
	}
	if code := closeCode(c2); code != gws.ClosePolicyViolation {
		t.Errorf("close code = %d, want %d", code, gws.ClosePolicyViolation)
	}
}

// TestPingIntervalMustBeShorterThanPongTimeout：先前照常啟動，閒置連線每隔 PongTimeout 被斷一次
func TestPingIntervalMustBeShorterThanPongTimeout(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("NewHub accepted PingInterval >= PongTimeout")
			}
		}()
		NewHub(logger.NewLogger(), Config{PingInterval: 3 * time.Second, PongTimeout: time.Second})
	}()
	// 上限的預設值
	if d := NewHub(logger.NewLogger(), Config{}).config; d.MaxSubscriptions != 100 || d.MaxInvalidMessages != 10 {
		t.Errorf("defaults: MaxSubscriptions=%d MaxInvalidMessages=%d, want 100 and 10", d.MaxSubscriptions, d.MaxInvalidMessages)
	}
	// 只設較短的 PongTimeout：ping 間隔跟著縮短
	h := NewHub(logger.NewLogger(), Config{PongTimeout: 10 * time.Second})
	if h.config.PingInterval >= h.config.PongTimeout {
		t.Errorf("PingInterval = %s with PongTimeout = %s", h.config.PingInterval, h.config.PongTimeout)
	}
}

// TestBroadcastBeforeRunDoesNotBlock：先前第 257 次呼叫起永久阻塞
func TestBroadcastBeforeRunDoesNotBlock(t *testing.T) {
	hub := NewHub(logger.NewLogger(), Config{})
	done := make(chan struct{})
	go func() {
		for i := 0; i < 600; i++ {
			hub.Broadcast([]byte(`1`))
			hub.BroadcastMessage(&Message{Type: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast blocked before Run")
	}
}

// TestDroppedMessagesAreCounted：出站佇列滿時丟掉的訊息有計數。先前沒有任何紀錄
func TestDroppedMessagesAreCounted(t *testing.T) {
	hub := NewHub(logger.NewLogger(), Config{})
	c := newClient("slow", nil, hub, codecJSON)
	for i := 0; i < cap(c.Send); i++ {
		if !c.trySend([]byte("x")) {
			t.Fatalf("send %d failed", i)
		}
	}
	for i := 0; i < 5; i++ {
		c.trySend([]byte("x"))
	}
	if n := hub.GetStats()["messages_dropped"]; n != int64(5) {
		t.Errorf("messages_dropped = %v, want 5", n)
	}
}

// TestWarnsWhenAuthorizeIsNotSet：沒有 Authorize 時啟動記一行警告
func TestWarnsWhenAuthorizeIsNotSet(t *testing.T) {
	var buf bytes.Buffer
	log, _ := logger.New("info", "", &buf, false)
	NewHub(log, Config{})
	if !strings.Contains(buf.String(), "Authorize") {
		t.Errorf("no warning about the missing Authorize hook: %q", buf.String())
	}
	buf.Reset()
	NewHub(log, Config{Authorize: func(*Client, *Message) bool { return true }})
	if strings.Contains(buf.String(), "Authorize") {
		t.Errorf("warned although Authorize is set: %q", buf.String())
	}
}
