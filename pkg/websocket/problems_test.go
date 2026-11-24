package websocket

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
)

// 以文字當心跳、不送別的訊息的客戶端不會被斷線；接連送來的垃圾訊息仍然會。
// 歷史 bug：解析失敗一律計入 MaxInvalidMessages，每 10 次心跳斷線一次，客戶端陷入重連循環
func TestTextHeartbeatsDoNotCloseTheConnection(t *testing.T) {
	old := invalidBurstWindow
	invalidBurstWindow = 20 * time.Millisecond
	defer func() { invalidBurstWindow = old }()

	hub, srv, _ := wsServer(t, Config{MaxInvalidMessages: 3}, true)
	c := mustDial(t, srv, nil)
	eventually(t, "client", func() bool { return hub.stats.ActiveConnections.Load() == 1 })
	for i := 0; i < 8; i++ {
		c.WriteMessage(gws.TextMessage, []byte("ping"))
		time.Sleep(50 * time.Millisecond)
	}
	if hub.stats.ActiveConnections.Load() != 1 {
		t.Fatal("a client that only sends text heartbeats was disconnected")
	}
	// 接連送來的仍然算
	for i := 0; i < 3; i++ {
		c.WriteMessage(gws.TextMessage, []byte("junk"))
	}
	if code := closeCode(c); code != gws.ClosePolicyViolation {
		t.Errorf("burst of junk: close code = %d, want %d", code, gws.ClosePolicyViolation)
	}
}

// 伺服器端加入的頻道與房間不佔 MaxSubscriptions。
// 歷史 bug：上限算的是連線持有的全部頻道與房間，伺服器替使用者訂了幾個之後客戶端自己的請求就被拒絕
func TestServerSideSubscriptionsDoNotUseTheQuota(t *testing.T) {
	var client atomic.Pointer[Client]
	hub, srv, _ := wsServer(t, Config{MaxSubscriptions: 2}, true)
	hub.SetCallbacks(func(c *Client) { client.Store(c) }, nil, nil)
	c := mustDial(t, srv, nil)
	eventually(t, "client", func() bool { return client.Load() != nil })
	for i := 0; i < 5; i++ {
		client.Load().Subscribe(fmt.Sprintf("srv%d", i))
	}
	client.Load().JoinRoom("lobby")

	c.WriteMessage(gws.TextMessage, []byte(`{"type":"subscribe","data":{"channel":"mine1"}}`))
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"join_room","data":{"room_id":"mine2"}}`))
	eventually(t, "client's own subscriptions", func() bool {
		st := hub.GetStats()
		_, ok := st["channels"].(map[string]int)["mine1"]
		return ok && st["total_rooms"].(int) == 2
	})
	// 第三個自己的請求才超過
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"subscribe","data":{"channel":"mine3"}}`))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, b, err := c.ReadMessage(); err != nil || !strings.Contains(string(b), "subscription limit reached") {
		t.Errorf("third request: %s, %v", b, err)
	}
}

// 重放保護與每個客戶端各自的金鑰可以並用：hello 用 Hub 的金鑰，之後用這條連線的。
// 歷史 bug：ClientSession 只有一組金鑰，用哪一組都有一邊驗證失敗
func TestReplayProtectionWithPerClientKeys(t *testing.T) {
	perClient := []byte("fedcba9876543210fedcba9876543210")
	sec := &SecurityConfig{AESKey: testKey, HMACKey: testKey, ReplayProtection: true}
	var received atomic.Int64
	hub, srv, _ := wsServer(t, Config{Security: sec}, true)
	hub.SetCallbacks(func(c *Client) {
		if err := c.SetEncryptionKey(perClient); err != nil {
			t.Error(err)
		}
	}, nil, func(*Client, *Message) { received.Add(1) })

	conn, s := replayDial(t, wsURL(srv), sec) // hello：Hub 的金鑰
	if err := s.SetDataKeys(perClient, nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "client", func() bool { return hub.stats.ActiveConnections.Load() == 1 })

	hub.Broadcast([]byte(`"hi"`))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, frame, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if p, _, err := s.Open(frame); err != nil || !strings.Contains(string(p), "hi") {
		t.Fatalf("server → client with the per-client key: %q, %v", p, err)
	}
	out, err := s.Seal([]byte(`{"type":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	conn.WriteMessage(gws.BinaryMessage, out)
	eventually(t, "client → server with the per-client key", func() bool { return received.Load() == 1 })
}
