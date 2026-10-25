// Package websocket 提供優化的 WebSocket 實現，整合 HypGo Context
//
// @chris
package websocket

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

// ===== 物件池 =====
//
// Client 與 Room 不池化：兩者的存活期與連線相同，且有 readPump / writePump 等 goroutine
// 持有指標。先前斷線時把 Client 重設後放回池，仍在執行的 goroutine 隨即讀寫到被重設、
// 甚至已分配給下一條連線的同一個物件（data race、對新連線並發寫入、Shutdown 時 nil Conn panic）

var (
	// Message 物件池
	messagePool = &sync.Pool{
		New: func() interface{} {
			return &Message{}
		},
	}

	// GC 優化：broadcast 用的 client slice pool
	// 避免每次廣播都 make 新 slice（10k 客戶端 × 100msg/s = 100萬次分配/s）
	clientSlicePool = &sync.Pool{
		New: func() interface{} {
			s := make([]*Client, 0, 256)
			return &s
		},
	}
)

// ===== 配置 =====

// TLSConfig 獨立 WSS 伺服器的 TLS 配置
// 當 websocket 不透過 HypGo server 使用時，提供 wss:// 支援
type TLSConfig struct {
	CertFile  string      // TLS 證書檔案路徑
	KeyFile   string      // TLS 金鑰檔案路徑
	TLSConfig *tls.Config // 可選：預配置的 tls.Config（優先使用）
}

// CompressionConfig permessage-deflate 壓縮配置
type CompressionConfig struct {
	Enabled bool // 是否啟用壓縮（預設 true）
	Level   int  // flate 壓縮等級 1-9，0 表示使用預設值
}

// Config WebSocket 配置
type Config struct {
	ReadBufferSize    int
	WriteBufferSize   int
	HandshakeTimeout  time.Duration
	EnableCompression bool // 向後兼容：Compression 為 nil 時使用此欄位
	// CheckOrigin 自訂 Origin 檢查。未設定時依 AllowedOrigins；兩者都未設定時採 gorilla 的
	// 同源檢查。先前未設定即放行任何來源：惡意網站能帶著使用者的 cookie 連線（跨站 WebSocket 劫持）
	CheckOrigin func(*http.Request) bool
	// AllowedOrigins 允許的 Origin（如 "https://app.example.com"，大小寫不分；"*" 允許全部）
	AllowedOrigins []string
	MaxMessageSize int64
	PingInterval   time.Duration
	PongTimeout    time.Duration
	WriteTimeout   time.Duration
	Subprotocols   []string           // 支援的子協議（JSON/Protobuf/FlatBuffers/MessagePack）
	TLS            *TLSConfig         // nil = ws://，non-nil = wss://（獨立模式）
	Security       *SecurityConfig    // nil = 無安全層（AES + HMAC）
	Compression    *CompressionConfig // nil 時回退 EnableCompression

	// ClientID 由升級請求決定客戶端 ID（例如取自 JWT 驗證後的使用者）；nil 或回傳空字串時產生隨機 ID。
	// 同一 ID 再次連線時，舊連線會被關閉（後連者優先）。
	// 先前直接採用客戶端送來的 X-Client-ID / ?client_id=（任何人都能冒用他人 ID 收取
	// SendToClient 的訊息），預設 ID 為 client-<UnixNano>（時鐘解析度粗時大量重複）
	ClientID func(c *hypcontext.Context) string

	// Authorize 對每則入站訊息授權；回傳 false 時丟棄該訊息（不觸發 onMessage，也不執行內建指令）。
	// 內建指令的目標已解析進 msg.Channel：subscribe / unsubscribe / publish 為頻道名稱，
	// join_room / leave_room 為房間 ID；broadcast 會送給全部連線。
	// nil 表示全部允許——任何已連線的客戶端都能廣播、發布到任何頻道、加入任何房間
	Authorize func(client *Client, msg *Message) bool
}

// DefaultConfig 預設配置
var DefaultConfig = Config{
	ReadBufferSize:    4096,
	WriteBufferSize:   4096,
	HandshakeTimeout:  10 * time.Second,
	EnableCompression: true,
	MaxMessageSize:    512 * 1024, // 512KB
	PingInterval:      54 * time.Second,
	PongTimeout:       60 * time.Second,
	WriteTimeout:      10 * time.Second,
	Subprotocols:      []string{"json", "protobuf", "flatbuffers", "msgpack"},
}

// withDefaults 以 DefaultConfig 補上未設定的時間與大小：PingInterval 為 0 時 writePump 的
// time.NewTicker 會 panic，PongTimeout 為 0 則每條連線一建立就逾時
func (c Config) withDefaults() Config {
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = DefaultConfig.HandshakeTimeout
	}
	if c.MaxMessageSize <= 0 {
		c.MaxMessageSize = DefaultConfig.MaxMessageSize
	}
	if c.PingInterval <= 0 {
		c.PingInterval = DefaultConfig.PingInterval
	}
	if c.PongTimeout <= 0 {
		c.PongTimeout = DefaultConfig.PongTimeout
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = DefaultConfig.WriteTimeout
	}
	return c
}

// ===== Upgrader =====

// Upgrader WebSocket 升級器
type Upgrader struct {
	upgrader websocket.Upgrader
	config   Config
}

// NewUpgrader 創建新的 Upgrader
func NewUpgrader(config Config) *Upgrader {
	checkOrigin := config.CheckOrigin
	if checkOrigin == nil && len(config.AllowedOrigins) > 0 {
		checkOrigin = originChecker(config.AllowedOrigins)
	}
	// 仍為 nil 時交給 gorilla：Origin 須與 Host 相同（沒有 Origin 標頭的非瀏覽器客戶端放行）

	subprotocols := config.Subprotocols
	if len(subprotocols) == 0 {
		subprotocols = []string{"json", "protobuf", "flatbuffers", "msgpack"}
	}

	// 決定壓縮啟用狀態
	enableCompression := config.EnableCompression
	if config.Compression != nil {
		enableCompression = config.Compression.Enabled
	}

	return &Upgrader{
		upgrader: websocket.Upgrader{
			ReadBufferSize:    config.ReadBufferSize,
			WriteBufferSize:   config.WriteBufferSize,
			HandshakeTimeout:  config.HandshakeTimeout,
			EnableCompression: enableCompression,
			CheckOrigin:       checkOrigin,
			Subprotocols:      subprotocols,
		},
		config: config,
	}
}

// originChecker 依允許清單檢查 Origin；沒有 Origin 標頭（非瀏覽器客戶端）時放行
func originChecker(allowed []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		for _, a := range allowed {
			if a == "*" || strings.EqualFold(a, origin) {
				return true
			}
		}
		return false
	}
}

// ===== Message =====

// Message WebSocket 訊息
type Message struct {
	Type      string          `json:"type"`
	Channel   string          `json:"channel,omitempty"`
	Data      json.RawMessage `json:"data"`
	Timestamp int64           `json:"timestamp,omitempty"` // UnixNano
	ClientID  string          `json:"client_id,omitempty"`
}

// AcquireMessage 從池中獲取 Message
func AcquireMessage() *Message {
	msg := messagePool.Get().(*Message)
	msg.reset()
	msg.Timestamp = time.Now().UnixNano()
	return msg
}

// Release 釋放 Message 回池中
func (m *Message) Release() {
	m.reset()
	messagePool.Put(m)
}

// reset 重置 Message
func (m *Message) reset() {
	m.Type = ""
	m.Channel = ""
	m.Data = nil
	m.Timestamp = 0
	m.ClientID = ""
}

// ===== Client =====

// Client WebSocket 客戶端
type Client struct {
	ID   string
	Conn *websocket.Conn
	// Send 出站訊息佇列。不會被關閉：斷線以 done 通知 writePump。
	// 先前斷線時 close(Send)，其他 goroutine（頻道發布、SendToClient）仍可能送入而 panic
	Send     chan []byte
	Hub      *Hub
	Channels map[string]bool // mu 保護
	// Context 升級請求的 Context 副本（c.Copy()，不屬於物件池），連線期間可安全讀取 Keys（如 JWT
	// claims、角色）與 Request。先前保留原 Context：handler 返回即被放回池並由其他請求重用
	Context     *hypcontext.Context
	codec       Codec // 協商的序列化 codec（JSON/Protobuf/FlatBuffers/MessagePack）
	wsFrameType int   // websocket.TextMessage 或 BinaryMessage（快取）
	mu          sync.RWMutex
	// lastActivity 最後活動時間（UnixNano）：readPump 與 pong handler 寫入、hub 清理時讀取
	lastActivity atomic.Int64
	metadata     map[string]interface{} // 客戶端元數據（mu 保護）
	rooms        map[string]bool        // 已加入的房間 ID（mu 保護；斷線時只走訪所屬房間）

	// per-client 安全金鑰（mu 保護；覆寫 hub 層預設）
	aead    cipher.AEAD
	hmacKey []byte

	done      chan struct{} // 斷線、被同 ID 新連線取代或 Hub 關閉時關閉
	closeOnce sync.Once
	gone      bool // 已完成注銷（Hub.mu 保護）
}

// newClient 建立連線的 Client
func newClient(id string, conn *websocket.Conn, hub *Hub, codec Codec) *Client {
	c := &Client{
		ID:          id,
		Conn:        conn,
		Hub:         hub,
		codec:       codec,
		wsFrameType: codec.WebSocketMessageType(),
		Send:        make(chan []byte, 256),
		Channels:    make(map[string]bool, 4),
		metadata:    make(map[string]interface{}, 4),
		done:        make(chan struct{}),
	}
	c.lastActivity.Store(time.Now().UnixNano())
	return c
}

// randomClientID 產生不可預測的客戶端 ID
func randomClientID() string {
	b := make([]byte, 16)
	rand.Read(b) // Go 1.24 起 crypto/rand.Read 不會回傳錯誤
	return "client-" + hex.EncodeToString(b)
}

// Codec 獲取客戶端使用的序列化 Codec
func (c *Client) Codec() Codec {
	return c.codec
}

// shutdown 通知 writePump 送出 close frame 並關閉連線；可重複呼叫
func (c *Client) shutdown() {
	c.closeOnce.Do(func() {
		if c.done != nil {
			close(c.done)
		}
	})
}

// isClosed 回報連線是否已關閉或正在關閉
func (c *Client) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// trySend 非阻塞地送入出站佇列；已斷線或佇列滿時回傳 false
func (c *Client) trySend(data []byte) bool {
	if c.isClosed() {
		return false
	}
	select {
	case c.Send <- data:
		return true
	default:
		return false
	}
}

// SetMetadata 設置客戶端元數據
func (c *Client) SetMetadata(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.metadata == nil {
		c.metadata = make(map[string]interface{})
	}
	c.metadata[key] = value
}

// GetMetadata 獲取客戶端元數據
func (c *Client) GetMetadata(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.metadata[key]
	return val, ok
}

// SetEncryptionKey 設置 per-client AES-256 加密金鑰（覆寫 hub 層預設；須為 32 bytes）
func (c *Client) SetEncryptionKey(key []byte) error {
	gcm, err := newAEAD(key)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.aead = gcm
	c.mu.Unlock()
	return nil
}

// SetHMACKey 設置 per-client HMAC 簽名金鑰（覆寫 hub 層預設）
func (c *Client) SetHMACKey(key []byte) {
	c.mu.Lock()
	c.hmacKey = append([]byte(nil), key...)
	c.mu.Unlock()
}

// ===== Hub =====

// Hub WebSocket 中心
type Hub struct {
	clients    map[string]*Client
	broadcast  chan *Message
	register   chan *Client
	unregister chan *Client
	channels   map[string]map[*Client]bool
	rooms      map[string]*Room
	logger     *logger.Logger
	config     Config
	upgrader   *Upgrader
	security   *SecurityConfig // AES + HMAC 安全管線配置
	mu         sync.RWMutex

	started  atomic.Bool
	done     chan struct{} // Run 結束（Shutdown）時關閉
	stopOnce sync.Once

	// 統計資訊。atomic：MessagesReceived/BytesReceived 由每條連線的
	// readPump 並發遞增、MessagesSent/BytesSent 由多個發送路徑遞增
	stats struct {
		TotalConnections  atomic.Int64
		ActiveConnections atomic.Int64
		MessagesSent      atomic.Int64
		MessagesReceived  atomic.Int64
		BytesSent         atomic.Int64
		BytesReceived     atomic.Int64
	}

	// 回調函數
	onConnect    func(*Client)
	onDisconnect func(*Client)
	onMessage    func(*Client, *Message)
}

// NewHub 創建新的 Hub。Security.AESKey 不是 32 bytes 時 panic（設定錯誤應在啟動時發現）
func NewHub(logger *logger.Logger, config Config) *Hub {
	config = config.withDefaults()
	if config.Security != nil {
		if _, err := config.Security.cipher(); err != nil {
			panic(fmt.Sprintf("websocket: SecurityConfig.AESKey: %v", err))
		}
	}
	return &Hub{
		clients:    make(map[string]*Client),
		broadcast:  make(chan *Message, 256),
		register:   make(chan *Client, 16),
		unregister: make(chan *Client, 16),
		channels:   make(map[string]map[*Client]bool),
		rooms:      make(map[string]*Room),
		logger:     logger,
		config:     config,
		upgrader:   NewUpgrader(config),
		security:   config.Security,
		done:       make(chan struct{}),
	}
}

// SetCallbacks 設置回調函數
func (h *Hub) SetCallbacks(
	onConnect func(*Client),
	onDisconnect func(*Client),
	onMessage func(*Client, *Message),
) {
	h.onConnect = onConnect
	h.onDisconnect = onDisconnect
	h.onMessage = onMessage
}

// Run 運行 Hub，直到 ctx 結束或呼叫 Shutdown
func (h *Hub) Run(ctx context.Context) {
	h.started.Store(true)
	// 定期清理不活躍的連接
	cleanupTicker := time.NewTicker(30 * time.Second)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.Shutdown()
			return

		case <-h.done:
			return

		case client := <-h.register:
			h.handleRegister(client)

		case client := <-h.unregister:
			h.handleUnregister(client)

		case message := <-h.broadcast:
			h.handleBroadcast(message)
			message.Release()

		case <-cleanupTicker.C:
			h.cleanupInactiveClients()
		}
	}
}

// stopped 回報 Hub 是否已關閉
func (h *Hub) stopped() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

// handleRegister 處理客戶端註冊
func (h *Hub) handleRegister(client *Client) {
	h.mu.Lock()
	if client.gone {
		// 註冊前就已斷線（Run 先處理了 unregister）：不再放進 map，否則永遠留著
		h.mu.Unlock()
		return
	}
	old := h.clients[client.ID]
	h.clients[client.ID] = client
	if old != nil && old != client {
		// 同一 ID 再次連線（多開分頁 / 重連）：後連者優先，關閉舊連線。
		// 舊連線的注銷以指標比對，不會刪掉新連線的項目
		old.shutdown()
	} else {
		h.stats.ActiveConnections.Add(1)
	}
	h.mu.Unlock()
	h.stats.TotalConnections.Add(1)

	if h.onConnect != nil {
		h.onConnect(client)
	}

	h.logger.Infof("Client %s connected", client.ID)
}

// handleUnregister 處理客戶端註銷（重複呼叫無作用）。
// 先前以 ID 查 map：同一 ID 的第二條連線會被第一條的斷線一併刪除，留下收不到任何訊息的連線
func (h *Hub) handleUnregister(client *Client) {
	h.mu.Lock()
	if client.gone {
		h.mu.Unlock()
		return
	}
	client.gone = true
	// 在 h.mu 內關閉：之後的 Subscribe / JoinRoom 看到已關閉即不再加入
	client.shutdown()
	if cur, ok := h.clients[client.ID]; ok && cur == client {
		delete(h.clients, client.ID)
		h.stats.ActiveConnections.Add(-1)
	}

	client.mu.RLock()
	for channel := range client.Channels {
		if clients, ok := h.channels[channel]; ok {
			delete(clients, client)
			if len(clients) == 0 {
				delete(h.channels, channel)
			}
		}
	}
	// 只走訪 client 實際加入的房間；清空的房間一併刪除（先前留在 map 中無限累積）
	for id := range client.rooms {
		if room, ok := h.rooms[id]; ok {
			room.RemoveClient(client)
			if room.Len() == 0 {
				delete(h.rooms, id)
			}
		}
	}
	client.mu.RUnlock()
	h.mu.Unlock()

	if h.onDisconnect != nil {
		h.onDisconnect(client)
	}
	h.logger.Infof("Client %s disconnected", client.ID)
}

// handleBroadcast 處理廣播（支持跨協議序列化 + 安全管線）
// GC 優化：使用 clientSlicePool 避免每次廣播分配新 slice
func (h *Hub) handleBroadcast(msg *Message) {
	// 從 pool 取得 client slice
	slicePtr := clientSlicePool.Get().(*[]*Client)
	clients := (*slicePtr)[:0]

	h.mu.RLock()
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	marshalForClients(msg, clients, h.security, h.countSent)

	// 清除引用防止 client 被 pool 持有而無法 GC
	for i := range clients {
		clients[i] = nil
	}
	clients = clients[:0]
	*slicePtr = clients
	clientSlicePool.Put(slicePtr)
}

// countSent 累計一則送出的訊息
func (h *Hub) countSent(n int64) {
	h.stats.MessagesSent.Add(1)
	h.stats.BytesSent.Add(n)
}

// cleanupInactiveClients 清理不活躍的客戶端
func (h *Hub) cleanupInactiveClients() {
	h.mu.RLock()
	cutoff := time.Now().Add(-2 * h.config.PongTimeout).UnixNano()

	var inactiveClients []*Client
	for _, client := range h.clients {
		if client.lastActivity.Load() < cutoff {
			inactiveClients = append(inactiveClients, client)
		}
	}
	h.mu.RUnlock()

	// 直接呼叫 handleUnregister 而非送進 h.unregister channel：
	// 本函式在 Hub 的 Run goroutine 內執行，而該 channel 的唯一消費者
	// 就是 Run 自己——一次清掃超過 channel 容量的不活躍連線時，
	// 發送方（Run）會永久阻塞，整個 Hub 凍結
	for _, client := range inactiveClients {
		h.logger.Debugf("Cleaning up inactive client: %s", client.ID)
		h.handleUnregister(client)
	}
}

// ServeHTTP 處理 WebSocket 升級（整合 HypGo Context）。Hub 尚未 Run 或已關閉時回 503
func (h *Hub) ServeHTTP(c *hypcontext.Context) {
	if !h.started.Load() || h.stopped() {
		// 先前送進 register channel 後永久阻塞，HTTP handler 一直掛著
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	conn, err := h.upgrader.upgrader.Upgrade(c.Response, c.Request, nil)
	if err != nil {
		// Upgrade 已寫出錯誤回應（如 Origin 不符時的 403），不再重複寫
		h.logger.Warningf("WebSocket upgrade failed: %v", err)
		c.Abort()
		return
	}

	clientID := ""
	if h.config.ClientID != nil {
		clientID = h.config.ClientID(c)
	}
	if clientID == "" {
		clientID = randomClientID()
	}

	// 根據協商的子協議選擇 Codec
	client := newClient(clientID, conn, h, CodecByName(conn.Subprotocol()))
	client.Context = c.Copy()

	// 套用 permessage-deflate 壓縮配置
	if h.config.Compression != nil {
		conn.EnableWriteCompression(h.config.Compression.Enabled)
		if h.config.Compression.Level != 0 {
			conn.SetCompressionLevel(h.config.Compression.Level)
		}
	}

	// 設置連接參數
	conn.SetReadLimit(h.config.MaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(h.config.PongTimeout))
	conn.SetPongHandler(func(string) error {
		client.lastActivity.Store(time.Now().UnixNano())
		conn.SetReadDeadline(time.Now().Add(h.config.PongTimeout))
		return nil
	})

	// 註冊客戶端
	select {
	case h.register <- client:
	case <-h.done:
		conn.Close()
		return
	}

	// 啟動讀寫循環
	go client.writePump(h.config)
	go client.readPump()
}

// unregisterClient 送出注銷請求；Hub 已關閉時直接返回（先前 Run 結束後，
// 超過 channel 容量的 readPump 在送出時永久阻塞）
func (h *Hub) unregisterClient(c *Client) {
	select {
	case h.unregister <- c:
	case <-h.done:
	}
}

// ===== Client 讀寫方法 =====

// readPump 讀取循環
func (c *Client) readPump() {
	defer func() {
		// 處理單則訊息時的 panic（如 callback 或 codec 錯誤）只關閉這條連線：
		// readPump 是獨立 goroutine，Recovery 中介層管不到，先前會讓整個行程崩潰
		if r := recover(); r != nil {
			c.Hub.logger.Errorf("websocket: panic while handling a message from client %s: %v", c.ID, r)
		}
		c.Hub.unregisterClient(c)
		c.Conn.Close()
	}()

	hub := c.Hub
	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				hub.logger.Warningf("WebSocket error for client %s: %v", c.ID, err)
			}
			return
		}

		c.lastActivity.Store(time.Now().UnixNano())
		hub.stats.MessagesReceived.Add(1)
		hub.stats.BytesReceived.Add(int64(len(data)))

		// 安全管線：解密 + 驗證簽名
		if hub.security != nil {
			var secErr error
			data, secErr = applySecurityIn(data, c, hub.security)
			if secErr != nil {
				hub.logger.Warningf("Security verification failed for client %s: %v", c.ID, secErr)
				continue
			}
		}

		msg := AcquireMessage()
		if err := c.codec.Unmarshal(data, msg); err != nil {
			hub.logger.Warningf("Invalid message format from client %s: %v", c.ID, err)
			msg.Release()
			continue
		}
		if !withinSkew(hub.security, msg.Timestamp) {
			hub.logger.Warningf("Message from client %s rejected: timestamp outside MaxClockSkew", c.ID)
			msg.Release()
			continue
		}

		msg.ClientID = c.ID
		c.resolveTarget(msg)
		if auth := hub.config.Authorize; auth != nil && !auth(c, msg) {
			hub.logger.Debugf("Message %q from client %s denied by Authorize", msg.Type, c.ID)
			msg.Release()
			continue
		}

		// 觸發回調
		if hub.onMessage != nil {
			hub.onMessage(c, msg)
		}

		c.handleMessage(msg)
		msg.Release()
	}
}

// writePump 寫入循環；結束時關閉連線（readPump 隨之結束並注銷）
func (c *Client) writePump(config Config) {
	ticker := time.NewTicker(config.PingInterval)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case <-c.done:
			c.Conn.SetWriteDeadline(time.Now().Add(config.WriteTimeout))
			c.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return

		case message := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(config.WriteTimeout))
			if err := c.Conn.WriteMessage(c.wsFrameType, message); err != nil {
				return
			}
			// 批量發送佇列中已有的訊息（只有本 goroutine 讀取 Send，長度不會縮減）
			for n := len(c.Send); n > 0; n-- {
				if err := c.Conn.WriteMessage(c.wsFrameType, <-c.Send); err != nil {
					return
				}
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(config.WriteTimeout))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// extractChannel 從控制訊息的 data 欄位提取頻道名稱（codec 感知）
// 若 codec 實現 ControlDecoder 介面則使用其自訂解析，否則預設 JSON
func (c *Client) extractChannel(data []byte) string {
	if cd, ok := c.codec.(ControlDecoder); ok {
		return cd.DecodeChannel(data)
	}
	var parsed struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(data, &parsed); err == nil {
		return parsed.Channel
	}
	return ""
}

// extractRoomID 從控制訊息的 data 欄位提取房間 ID（codec 感知）
// 若 codec 實現 ControlDecoder 介面則使用其自訂解析，否則預設 JSON
func (c *Client) extractRoomID(data []byte) string {
	if cd, ok := c.codec.(ControlDecoder); ok {
		return cd.DecodeRoomID(data)
	}
	var parsed struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(data, &parsed); err == nil {
		return parsed.RoomID
	}
	return ""
}

// resolveTarget 把內建控制訊息的目標解析進 msg.Channel，讓 Authorize 不必自行解碼 Data
func (c *Client) resolveTarget(msg *Message) {
	switch msg.Type {
	case "subscribe", "unsubscribe":
		msg.Channel = c.extractChannel(msg.Data)
	case "join_room", "leave_room":
		msg.Channel = c.extractRoomID(msg.Data)
	}
}

// handleMessage 處理內建控制訊息（目標已由 resolveTarget 解析進 msg.Channel）
func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case "subscribe":
		if msg.Channel != "" {
			c.Subscribe(msg.Channel)
		}

	case "unsubscribe":
		if msg.Channel != "" {
			c.Unsubscribe(msg.Channel)
		}

	case "publish":
		c.Hub.PublishToChannel(msg.Channel, msg)

	case "broadcast":
		c.Hub.BroadcastMessage(msg)

	case "join_room":
		if msg.Channel != "" {
			c.JoinRoom(msg.Channel)
		}

	case "leave_room":
		if msg.Channel != "" {
			c.LeaveRoom(msg.Channel)
		}
	}
}

// ===== 頻道管理 =====

// Subscribe 訂閱頻道（已斷線的客戶端不會被加入）
func (c *Client) Subscribe(channel string) {
	h := c.Hub
	h.mu.Lock()
	// 與 handleUnregister 同在 h.mu 內：先前斷線後抵達的訂閱會把客戶端重新加回頻道
	if c.isClosed() {
		h.mu.Unlock()
		return
	}
	if h.channels[channel] == nil {
		h.channels[channel] = make(map[*Client]bool)
	}
	h.channels[channel][c] = true
	c.mu.Lock()
	c.Channels[channel] = true
	c.mu.Unlock()
	h.mu.Unlock()

	h.logger.Debugf("Client %s subscribed to channel %s", c.ID, channel)
}

// Unsubscribe 取消訂閱頻道
func (c *Client) Unsubscribe(channel string) {
	h := c.Hub
	h.mu.Lock()
	if clients, ok := h.channels[channel]; ok {
		delete(clients, c)
		if len(clients) == 0 {
			delete(h.channels, channel)
		}
	}
	c.mu.Lock()
	delete(c.Channels, channel)
	c.mu.Unlock()
	h.mu.Unlock()

	h.logger.Debugf("Client %s unsubscribed from channel %s", c.ID, channel)
}

// ===== Room 管理 =====

// Room WebSocket 房間。成員增減與房間建立 / 刪除都在 Hub.mu 內進行：
// 先前 JoinRoom 與 LeaveRoom 各自取鎖，可能把客戶端加進剛被刪除、放回池並分配給
// 其他房間 ID 的 Room
type Room struct {
	ID           string
	Clients      map[*Client]bool // mu 保護
	Metadata     map[string]interface{}
	mu           sync.RWMutex
	hub          *Hub
	created      time.Time
	lastActivity time.Time
}

func newRoom(id string, hub *Hub) *Room {
	now := time.Now()
	return &Room{
		ID:           id,
		Clients:      make(map[*Client]bool),
		Metadata:     make(map[string]interface{}),
		hub:          hub,
		created:      now,
		lastActivity: now,
	}
}

// Len 回傳房間內的客戶端數
func (r *Room) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Clients)
}

// AddClient 添加客戶端到房間
func (r *Room) AddClient(client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Clients[client] = true
	r.lastActivity = time.Now()
}

// RemoveClient 從房間移除客戶端
func (r *Room) RemoveClient(client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Clients, client)
	r.lastActivity = time.Now()
}

// Broadcast 房間廣播原始位元組：包成 Type 為 "broadcast" 的 Message，
// 依各客戶端的 codec 序列化並經過安全管線。先前直接送出原始位元組，
// 設定了 AES / HMAC 時仍以明文送出
func (r *Room) Broadcast(data []byte) {
	r.BroadcastMessage(&Message{Type: "broadcast", Data: data, Timestamp: time.Now().UnixNano()})
}

// BroadcastMessage 房間廣播（結構化 Message，支持跨協議序列化 + 安全管線）
func (r *Room) BroadcastMessage(msg *Message) {
	r.mu.RLock()
	clients := make([]*Client, 0, len(r.Clients))
	for client := range r.Clients {
		clients = append(clients, client)
	}
	r.mu.RUnlock()

	var security *SecurityConfig
	var onSent func(int64)
	if r.hub != nil {
		security = r.hub.security
		onSent = r.hub.countSent
	}
	marshalForClients(msg, clients, security, onSent)
}

// JoinRoom 加入房間（已斷線的客戶端不會被加入）
func (c *Client) JoinRoom(roomID string) {
	h := c.Hub
	h.mu.Lock()
	if c.isClosed() {
		h.mu.Unlock()
		return
	}
	room, exists := h.rooms[roomID]
	if !exists {
		room = newRoom(roomID, h)
		h.rooms[roomID] = room
	}
	room.AddClient(c)
	c.mu.Lock()
	if c.rooms == nil {
		c.rooms = make(map[string]bool, 2)
	}
	c.rooms[roomID] = true
	c.mu.Unlock()
	h.mu.Unlock()

	h.logger.Debugf("Client %s joined room %s", c.ID, roomID)
}

// LeaveRoom 離開房間；房間清空時刪除
func (c *Client) LeaveRoom(roomID string) {
	h := c.Hub
	h.mu.Lock()
	room, exists := h.rooms[roomID]
	if exists {
		room.RemoveClient(c)
		if room.Len() == 0 {
			delete(h.rooms, roomID)
		}
	}
	c.mu.Lock()
	delete(c.rooms, roomID)
	c.mu.Unlock()
	h.mu.Unlock()

	if exists {
		h.logger.Debugf("Client %s left room %s", c.ID, roomID)
	}
}

// ===== Hub 便利方法 =====

// enqueueBroadcast 送進廣播佇列；Hub 已關閉時丟棄（先前永久阻塞）
func (h *Hub) enqueueBroadcast(msg *Message) {
	select {
	case h.broadcast <- msg:
	case <-h.done:
		msg.Release()
	}
}

// Broadcast 廣播訊息（原始位元組，向後兼容）
// 將 data 包裝為 Message 後發送到廣播通道
func (h *Hub) Broadcast(data []byte) {
	msg := AcquireMessage()
	msg.Type = "broadcast"
	msg.Data = data
	h.enqueueBroadcast(msg)
}

// BroadcastMessage 廣播結構化 Message（支持跨協議序列化）
func (h *Hub) BroadcastMessage(msg *Message) {
	// 複製 Message 以避免在 readPump 中被釋放
	clone := AcquireMessage()
	clone.Type = msg.Type
	clone.Channel = msg.Channel
	clone.Data = make(json.RawMessage, len(msg.Data))
	copy(clone.Data, msg.Data)
	clone.Timestamp = msg.Timestamp
	clone.ClientID = msg.ClientID
	h.enqueueBroadcast(clone)
}

// PublishToChannel 發布結構化 Message 到頻道（支持跨協議序列化）
func (h *Hub) PublishToChannel(channel string, msg *Message) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.channels[channel]))
	for client := range h.channels[channel] {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	if len(clients) == 0 {
		return
	}

	// 確保 channel 和 type 正確
	pubMsg := AcquireMessage()
	defer pubMsg.Release()

	pubMsg.Type = "message"
	pubMsg.Channel = channel
	pubMsg.Data = msg.Data
	pubMsg.Timestamp = msg.Timestamp
	pubMsg.ClientID = msg.ClientID

	marshalForClients(pubMsg, clients, h.security, h.countSent)
}

// PublishToChannelRaw 發布原始位元組到頻道（向後兼容）
func (h *Hub) PublishToChannelRaw(channel string, data []byte) {
	msg := AcquireMessage()
	defer msg.Release()
	msg.Type = "message"
	msg.Channel = channel
	msg.Data = data
	h.PublishToChannel(channel, msg)
}

// SendToClient 發送給特定客戶端（codec 感知）
// 支持 *Message 結構體或任意 data（後者回退 json.Marshal）
func (h *Hub) SendToClient(clientID string, data interface{}) error {
	h.mu.RLock()
	client, ok := h.clients[clientID]
	h.mu.RUnlock()

	if !ok {
		return fmt.Errorf("client %s not found", clientID)
	}

	var msgBytes []byte
	var err error

	// 若為 *Message 則使用客戶端的 codec 序列化
	if msg, isMsg := data.(*Message); isMsg {
		msgBytes, err = client.codec.Marshal(msg)
	} else {
		msgBytes, err = json.Marshal(data)
	}
	if err != nil {
		return err
	}

	// 安全管線：簽名 + 加密
	if h.security != nil {
		msgBytes, err = applySecurityOut(msgBytes, client, h.security)
		if err != nil {
			return fmt.Errorf("security pipeline failed: %w", err)
		}
	}

	if !client.trySend(msgBytes) {
		return fmt.Errorf("client %s disconnected or send buffer full", clientID)
	}
	h.countSent(int64(len(msgBytes)))
	return nil
}

// GetStats 獲取統計資訊
func (h *Hub) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	channelStats := make(map[string]int)
	for channel, clients := range h.channels {
		channelStats[channel] = len(clients)
	}

	roomStats := make(map[string]int)
	for roomID, room := range h.rooms {
		roomStats[roomID] = room.Len()
	}

	return map[string]interface{}{
		"total_connections":  h.stats.TotalConnections.Load(),
		"active_connections": h.stats.ActiveConnections.Load(),
		"messages_sent":      h.stats.MessagesSent.Load(),
		"messages_received":  h.stats.MessagesReceived.Load(),
		"bytes_sent":         h.stats.BytesSent.Load(),
		"bytes_received":     h.stats.BytesReceived.Load(),
		"total_clients":      len(h.clients),
		"total_channels":     len(h.channels),
		"total_rooms":        len(h.rooms),
		"channels":           channelStats,
		"rooms":              roomStats,
	}
}

// ListenAndServeTLS 啟動獨立 WSS 伺服器
// 此為便利方法，當 WebSocket Hub 不透過 HypGo server 使用時提供 wss:// 支援
func (h *Hub) ListenAndServeTLS(addr string, handler http.Handler) error {
	if h.config.TLS == nil {
		return fmt.Errorf("TLS config is nil; use standard http.ListenAndServe for ws://")
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// 使用預配置的 tls.Config（若有）
	if h.config.TLS.TLSConfig != nil {
		srv.TLSConfig = h.config.TLS.TLSConfig
		return srv.ListenAndServeTLS("", "")
	}

	return srv.ListenAndServeTLS(h.config.TLS.CertFile, h.config.TLS.KeyFile)
}

// Shutdown 關閉 Hub：通知所有連線送出 close frame 並關閉，之後的連線請求回 503（可重複呼叫）
func (h *Hub) Shutdown() {
	h.stopOnce.Do(func() {
		close(h.done)

		h.mu.Lock()
		clients := h.clients
		h.clients = make(map[string]*Client)
		h.channels = make(map[string]map[*Client]bool)
		h.rooms = make(map[string]*Room)
		h.stats.ActiveConnections.Store(0)
		h.mu.Unlock()

		for _, client := range clients {
			client.shutdown()
		}
		h.logger.Info("WebSocket Hub shutdown completed")
	})
}
