package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// wsServer 啟動 Hub（run=true 時）並以 HypGo router 在 /ws 提供升級
func wsServer(t *testing.T, cfg Config, run bool) (*Hub, *httptest.Server, *router.Router) {
	t.Helper()
	hub := NewHub(logger.NewLogger(), cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if run {
		go hub.Run(ctx)
		for !hub.started.Load() {
			time.Sleep(time.Millisecond)
		}
	}
	r := router.New()
	r.GET("/ws", hub.ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return hub, srv, r
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func mustDial(t *testing.T, srv *httptest.Server, h http.Header) *gws.Conn {
	t.Helper()
	c, _, err := gws.DefaultDialer.Dial(wsURL(srv), h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// eventually 等待 cond 成立（最多 2 秒）
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestFlatBuffersMalformedNoPanic 迴歸測試（A1）：惡意的 FlatBuffers 訊息回錯誤而非 panic。
// 歷史 bug：4 bytes 的訊息就讓解碼索引越界，在 readPump 中等同讓整個行程崩潰
func TestFlatBuffersMalformedNoPanic(t *testing.T) {
	for name, in := range map[string][]byte{
		"root offset past end": {0xff, 0xff, 0x00, 0x00, 0, 0, 0, 0},
		"vtable past end":      {0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80},
		"huge vector length":   {0x0c, 0, 0, 0, 0x06, 0, 0x08, 0, 0x04, 0, 0, 0, 0x06, 0, 0, 0, 0x04, 0, 0, 0, 0xff, 0xff, 0xff, 0x7f},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panicked: %v", name, r)
				}
			}()
			if err := (FlatBuffersCodec{}).Unmarshal(in, &Message{}); err == nil {
				t.Errorf("%s: want an error", name)
			}
		}()
	}
}

// TestConnectionChurnIsRaceFree 迴歸測試（A2）：連線反覆建立 / 訂閱 / 加入房間 / 斷線，最後 Shutdown，
// 在 -race 下沒有競爭、不 panic。歷史 bug：斷線時 Client 被重設並放回池，仍在執行的
// readPump / writePump 讀寫到同一個（甚至已分配給下一條連線的）物件，Shutdown 時對 nil Conn panic
func TestConnectionChurnIsRaceFree(t *testing.T) {
	hub, srv, _ := wsServer(t, DefaultConfig, true)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _, err := gws.DefaultDialer.Dial(wsURL(srv), nil)
			if err != nil {
				return
			}
			c.WriteMessage(gws.TextMessage, []byte(`{"type":"subscribe","data":{"channel":"x"}}`))
			c.WriteMessage(gws.TextMessage, []byte(`{"type":"join_room","data":{"room_id":"r"}}`))
			c.Close()
		}()
	}
	wg.Wait()
	hub.PublishToChannelRaw("x", []byte(`"hi"`))
	hub.Shutdown()
}

// TestClientContextIsACopy 迴歸測試（A3）：client.Context 是 Context 副本，不會被其他請求重用。
// 歷史 bug：保留池化的 Context，handler 返回即被回收，其他請求拿到同一個物件
func TestClientContextIsACopy(t *testing.T) {
	hub, srv, r := wsServer(t, DefaultConfig, true)
	var wsCtx atomic.Pointer[hypcontext.Context]
	hub.SetCallbacks(func(c *Client) { wsCtx.Store(c.Context) }, nil, nil)
	var reused atomic.Bool
	r.GET("/other", func(c *hypcontext.Context) {
		if c == wsCtx.Load() {
			reused.Store(true)
		}
		c.String(200, "ok")
	})
	mustDial(t, srv, nil)
	eventually(t, "onConnect", func() bool { return wsCtx.Load() != nil })
	for i := 0; i < 100; i++ {
		resp, err := http.Get(srv.URL + "/other")
		if err == nil {
			resp.Body.Close()
		}
	}
	if reused.Load() {
		t.Error("the websocket client's Context was reused by another request")
	}
	if wsCtx.Load().Request == nil {
		t.Error("client.Context.Request was cleared by the pool")
	}
}

// TestDefaultClientIDsAreUnique 迴歸測試（A4）：同時建立的連線取得不同的預設 ID。
// 歷史 bug：預設 ID 為 client-<UnixNano>，時鐘解析度粗時大量重複，後來的連線覆蓋前一個
func TestDefaultClientIDsAreUnique(t *testing.T) {
	hub, srv, _ := wsServer(t, DefaultConfig, true)
	var mu sync.Mutex
	ids := map[string]int{}
	hub.SetCallbacks(func(c *Client) { mu.Lock(); ids[c.ID]++; mu.Unlock() }, nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); mustDial(t, srv, nil) }()
	}
	wg.Wait()
	eventually(t, "20 connections", func() bool {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, c := range ids {
			n += c
		}
		return n == 20
	})
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 20 {
		t.Errorf("20 connections got %d distinct ids", len(ids))
	}
}

// TestClientIDFromConfigAndDuplicates 迴歸測試（A4）：客戶端送來的 X-Client-ID 不被採用；
// Config.ClientID 決定 ID，同一 ID 再次連線時舊連線被關閉，新連線仍收得到 SendToClient。
// 歷史 bug：直接採用客戶端送來的 ID（可冒用他人），且第一條連線斷線時刪掉了第二條的項目
func TestClientIDFromConfigAndDuplicates(t *testing.T) {
	cfg := DefaultConfig
	cfg.ClientID = func(c *hypcontext.Context) string { return c.Query("user") }
	hub, srv, _ := wsServer(t, cfg, true)

	spoof := mustDial(t, srv, http.Header{"X-Client-Id": {"victim"}})
	_ = spoof
	time.Sleep(50 * time.Millisecond)
	if err := hub.SendToClient("victim", "x"); err == nil {
		t.Error("client-supplied X-Client-ID must not be trusted")
	}

	first, _, err := gws.DefaultDialer.Dial(wsURL(srv)+"?user=alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	eventually(t, "alice registered", func() bool { return hub.SendToClient("alice", "ping") == nil })
	second, _, err := gws.DefaultDialer.Dial(wsURL(srv)+"?user=alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	// 舊連線被伺服器關閉
	first.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := first.ReadMessage(); err != nil {
			break
		}
	}
	time.Sleep(100 * time.Millisecond)
	if err := hub.SendToClient("alice", map[string]string{"to": "second"}); err != nil {
		t.Fatalf("SendToClient after the first connection closed: %v", err)
	}
	second.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, data, err := second.ReadMessage()
		if err != nil {
			t.Fatalf("second connection did not receive: %v", err)
		}
		if strings.Contains(string(data), "second") {
			break
		}
	}
}

// TestCrossOriginRejectedByDefault 迴歸測試（A5）：預設拒絕跨來源升級；AllowedOrigins 可放行。
// 歷史 bug：CheckOrigin 預設放行任何來源（跨站 WebSocket 劫持）
func TestCrossOriginRejectedByDefault(t *testing.T) {
	_, srv, _ := wsServer(t, DefaultConfig, true)
	if c, resp, err := gws.DefaultDialer.Dial(wsURL(srv), http.Header{"Origin": {"https://evil.example"}}); err == nil {
		c.Close()
		t.Error("cross-origin upgrade accepted")
	} else if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("want 403, got %v / %v", resp, err)
	}

	cfg := DefaultConfig
	cfg.AllowedOrigins = []string{"https://app.example"}
	_, srv2, _ := wsServer(t, cfg, true)
	mustDial(t, srv2, http.Header{"Origin": {"https://APP.example"}})
	if _, _, err := gws.DefaultDialer.Dial(wsURL(srv2), http.Header{"Origin": {"https://evil.example"}}); err == nil {
		t.Error("origin outside AllowedOrigins accepted")
	}
}

// TestRoomBroadcastUsesSecurityPipeline 迴歸測試（A6）：Room.Broadcast 經過加密。
// 歷史 bug：直接送出原始位元組，設定了 AES 仍是明文
func TestRoomBroadcastUsesSecurityPipeline(t *testing.T) {
	key := make([]byte, 32)
	hub := NewHub(logger.NewLogger(), Config{Security: &SecurityConfig{AESKey: key}})
	c := newClient("c", nil, hub, codecJSON)
	room := newRoom("r", hub)
	room.AddClient(c)
	room.Broadcast([]byte(`"secret"`))
	got := <-c.Send
	if strings.Contains(string(got), "secret") {
		t.Fatalf("room broadcast sent plaintext: %q", got)
	}
	plain, err := Decrypt(got, key)
	if err != nil || !strings.Contains(string(plain), `"data":"secret"`) {
		t.Errorf("decrypted = %q, %v", plain, err)
	}
}

// TestBinaryDataReachesJSONClients 迴歸測試（A7）：非 JSON 的 Data 以 base64 字串送給 JSON 客戶端。
// 歷史 bug：json.Marshal 對無效的 RawMessage 回錯誤，JSON 客戶端靜默收不到
func TestBinaryDataReachesJSONClients(t *testing.T) {
	jsonClient := &Client{Send: make(chan []byte, 1), codec: codecJSON, metadata: map[string]interface{}{}}
	marshalForClients(&Message{Type: "message", Data: []byte{0x81, 0xa1, 'k'}}, []*Client{jsonClient}, nil, nil)
	if len(jsonClient.Send) != 1 {
		t.Fatal("JSON client received nothing")
	}
	var got struct{ Data []byte }
	if err := json.Unmarshal(<-jsonClient.Send, &got); err != nil || string(got.Data) != "\x81\xa1k" {
		t.Errorf("data = %q, %v", got.Data, err)
	}
}

// TestEmptyRoomsAreDeleted 迴歸測試（A8）：最後一個成員斷線後房間被刪除。
// 歷史 bug：斷線只把客戶端移出房間，空房間永遠留在 map
func TestEmptyRoomsAreDeleted(t *testing.T) {
	hub, srv, _ := wsServer(t, DefaultConfig, true)
	c := mustDial(t, srv, nil)
	c.WriteMessage(gws.TextMessage, []byte(`{"type":"join_room","data":{"room_id":"lobby"}}`))
	eventually(t, "room created", func() bool { return hub.GetStats()["total_rooms"].(int) == 1 })
	c.Close()
	eventually(t, "room deleted", func() bool { return hub.GetStats()["total_rooms"].(int) == 0 })
}

// TestAuthorizeHook Config.Authorize 拒絕的訊息不觸發 onMessage 也不執行內建指令；
// 內建指令的目標已解析進 msg.Channel
func TestAuthorizeHook(t *testing.T) {
	cfg := DefaultConfig
	var seen sync.Map
	cfg.Authorize = func(c *Client, msg *Message) bool {
		seen.Store(msg.Type+":"+msg.Channel, true)
		return msg.Type != "broadcast" && msg.Channel != "admin"
	}
	hub, srv, _ := wsServer(t, cfg, true)
	var onMessage atomic.Int32
	hub.SetCallbacks(nil, nil, func(*Client, *Message) { onMessage.Add(1) })

	listener := mustDial(t, srv, nil)
	sender := mustDial(t, srv, nil)
	sender.WriteMessage(gws.TextMessage, []byte(`{"type":"subscribe","data":{"channel":"admin"}}`))
	sender.WriteMessage(gws.TextMessage, []byte(`{"type":"broadcast","data":"nope"}`))
	sender.WriteMessage(gws.TextMessage, []byte(`{"type":"join_room","data":{"room_id":"lobby"}}`))
	eventually(t, "allowed message handled", func() bool { return onMessage.Load() == 1 })

	if _, ok := seen.Load("subscribe:admin"); !ok {
		t.Error("subscribe target not resolved into msg.Channel")
	}
	if _, ok := seen.Load("join_room:lobby"); !ok {
		t.Error("join_room target not resolved into msg.Channel")
	}
	if n := hub.GetStats()["total_channels"].(int); n != 0 {
		t.Errorf("denied subscribe created %d channels", n)
	}
	listener.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, data, err := listener.ReadMessage(); err == nil {
		t.Errorf("denied broadcast was delivered: %q", data)
	}
}

// TestHubLifecycle 迴歸測試（A11）：Hub 尚未 Run 時升級回 503；Shutdown 後連線收到 close frame、
// 之後的升級回 503。歷史 bug：register channel 無人消費時 handler 永久阻塞
func TestHubLifecycle(t *testing.T) {
	_, notRunning, _ := wsServer(t, DefaultConfig, false)
	if _, resp, err := gws.DefaultDialer.Dial(wsURL(notRunning), nil); err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("hub not running: want 503, got %v / %v", resp, err)
	}

	hub, srv, _ := wsServer(t, DefaultConfig, true)
	c := mustDial(t, srv, nil)
	eventually(t, "registered", func() bool { return hub.GetStats()["total_clients"].(int) == 1 })
	hub.Shutdown()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := c.ReadMessage(); !gws.IsCloseError(err, gws.CloseNormalClosure) {
		t.Errorf("after Shutdown: want a normal close frame, got %v", err)
	}
	if _, resp, err := gws.DefaultDialer.Dial(wsURL(srv), nil); err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("after Shutdown: want 503, got %v / %v", resp, err)
	}
	hub.Broadcast([]byte(`"late"`)) // 不阻塞
}

// TestZeroConfigWorks 未設定的時間欄位套用預設值。歷史 bug：PingInterval 為 0 時 writePump panic
func TestZeroConfigWorks(t *testing.T) {
	hub, srv, _ := wsServer(t, Config{}, true)
	c := mustDial(t, srv, nil)
	eventually(t, "registered", func() bool { return hub.GetStats()["total_clients"].(int) == 1 })
	if err := c.WriteMessage(gws.TextMessage, []byte(`{"type":"subscribe","data":{"channel":"x"}}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "subscribed", func() bool { return hub.GetStats()["total_channels"].(int) == 1 })
}

// TestSecurityKeyValidation 迴歸測試（A12）：AES 金鑰須為 32 bytes；MaxClockSkew 限制重放窗口
func TestSecurityKeyValidation(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("NewHub with a 16-byte AES key should panic")
			}
		}()
		NewHub(logger.NewLogger(), Config{Security: &SecurityConfig{AESKey: make([]byte, 16)}})
	}()
	if err := newClient("c", nil, nil, codecJSON).SetEncryptionKey(make([]byte, 24)); err == nil {
		t.Error("SetEncryptionKey with a 24-byte key should fail")
	}
	sec := &SecurityConfig{MaxClockSkew: time.Minute}
	now := time.Now().UnixNano()
	if !withinSkew(sec, now) || withinSkew(sec, now-int64(2*time.Minute)) || withinSkew(sec, 0) {
		t.Error("MaxClockSkew window not enforced")
	}
	if !withinSkew(&SecurityConfig{}, 0) {
		t.Error("no MaxClockSkew should accept any timestamp")
	}
}

// TestSlowCallbackDoesNotBlockHub 迴歸測試（C）：一條連線的 onConnect 卡住時，其他連線仍能
// 註冊並收到廣播。歷史 bug：callback 在 Hub 唯一的 goroutine 上執行，任一 callback 慢下來就卡住全部連線
func TestSlowCallbackDoesNotBlockHub(t *testing.T) {
	hub, srv, _ := wsServer(t, DefaultConfig, true)
	block := make(chan struct{})
	defer close(block)
	var first atomic.Bool
	var connected atomic.Int32
	hub.SetCallbacks(func(c *Client) {
		if first.CompareAndSwap(false, true) {
			<-block // 第一條連線的 onConnect 卡住
		}
		connected.Add(1)
	}, nil, nil)

	mustDial(t, srv, nil)
	eventually(t, "first onConnect entered", first.Load)
	fast := mustDial(t, srv, nil)
	eventually(t, "second connection's onConnect", func() bool { return connected.Load() == 1 })

	hub.Broadcast([]byte(`"hello"`))
	fast.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, data, err := fast.ReadMessage(); err != nil || !strings.Contains(string(data), "hello") {
		t.Errorf("broadcast while another onConnect is blocked: %q, %v", data, err)
	}
}

// TestOnDisconnectExactlyOnce onConnect 時已可 SendToClient；onDisconnect 每條連線恰好一次，
// 不論斷線原因（客戶端關閉、被同 ID 取代、Shutdown）
func TestOnDisconnectExactlyOnce(t *testing.T) {
	cfg := DefaultConfig
	cfg.ClientID = func(c *hypcontext.Context) string { return c.Query("user") }
	hub, srv, _ := wsServer(t, cfg, true)
	var mu sync.Mutex
	disconnects := map[*Client]int{}
	hub.SetCallbacks(
		func(c *Client) { hub.SendToClient(c.ID, "welcome") },
		func(c *Client) { mu.Lock(); disconnects[c]++; mu.Unlock() },
		nil)
	total := func() int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, v := range disconnects {
			n += v
		}
		return n
	}

	dialUser := func(user string) *gws.Conn {
		c, _, err := gws.DefaultDialer.Dial(wsURL(srv)+"?user="+user, nil)
		if err != nil {
			t.Fatal(err)
		}
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, data, err := c.ReadMessage(); err != nil || !strings.Contains(string(data), "welcome") {
			t.Fatalf("%s: onConnect SendToClient not delivered: %q, %v", user, data, err)
		}
		return c
	}
	a := dialUser("a")
	a.Close() // 客戶端關閉
	eventually(t, "a disconnected", func() bool { return total() == 1 })
	dialUser("b")
	dialUser("b") // 取代第一個 b
	eventually(t, "replaced b disconnected", func() bool { return total() == 2 })
	dialUser("c")
	hub.Shutdown() // 關閉剩下的 b 與 c
	eventually(t, "all disconnected", func() bool { return total() == 4 })
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for c, n := range disconnects {
		if n != 1 {
			t.Errorf("client %s: onDisconnect called %d times", c.ID, n)
		}
	}
}
