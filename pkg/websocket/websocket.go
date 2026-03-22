// Package websocket 提供優化的 WebSocket 實現，整合 HypGo Context
package websocket

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

// ===== 物件池 =====

var (
	// Message 物件池
	messagePool = &sync.Pool{
		New: func() interface{} {
			return &Message{}
		},
	}

	// Client 物件池
	clientPool = &sync.Pool{
		New: func() interface{} {
			return &Client{
				Send:     make(chan []byte, 256),
				Channels: make(map[string]bool),
			}
		},
	}

	// Room 物件池
	roomPool = &sync.Pool{
		New: func() interface{} {
			return &Room{
				Clients: make(map[*Client]bool),
			}
		},
	}

	// 緩衝區池
	bufferPool = &sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 4096)
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
	CheckOrigin       func(*http.Request) bool
	MaxMessageSize    int64
	PingInterval      time.Duration
	PongTimeout       time.Duration
	WriteTimeout      time.Duration
	Subprotocols      []string           // 支援的子協議（JSON/Protobuf/FlatBuffers/MessagePack）
	TLS               *TLSConfig         // nil = ws://，non-nil = wss://（獨立模式）
	Security          *SecurityConfig    // nil = 無安全層（AES + HMAC）
	Compression       *CompressionConfig // nil 時回退 EnableCompression
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

// ===== Upgrader =====

// Upgrader WebSocket 升級器
type Upgrader struct {
	upgrader websocket.Upgrader
	config   Config
}

// NewUpgrader 創建新的 Upgrader
func NewUpgrader(config Config) *Upgrader {
	if config.CheckOrigin == nil {
		config.CheckOrigin = func(r *http.Request) bool {
			// 生產環境應該驗證 Origin
			return true
		}
	}

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
			CheckOrigin:       config.CheckOrigin,
			Subprotocols:      subprotocols,
		},
		config: config,
	}
}

// ===== Message =====

// Message WebSocket 訊息
type Message struct {
	Type      string          `json:"type"`
	Channel   string          `json:"channel,omitempty"`
	Data      json.RawMessage `json:"data"`
	Timestamp int64           `json:"timestamp,omitempty"`
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
	ID           string
	Conn         *websocket.Conn
	Send         chan []byte
	Hub          *Hub
	Channels     map[string]bool
	Context      *hypcontext.Context // 整合 HypGo Context
	codec        Codec               // 協商的序列化 codec（JSON/Protobuf/FlatBuffers/MessagePack）
	wsFrameType  int                 // websocket.TextMessage 或 BinaryMessage（快取）
	mu           sync.RWMutex
	pingTicker   *time.Ticker
	isClosing    bool
	lastActivity time.Time
	metadata     map[string]interface{} // 客戶端元數據
}

// AcquireClient 從池中獲取 Client
func AcquireClient(id string, conn *websocket.Conn, hub *Hub, codec Codec) *Client {
	client := clientPool.Get().(*Client)
	client.reset()
	client.ID = id
	client.Conn = conn
	client.Hub = hub
	client.codec = codec
	client.wsFrameType = codec.WebSocketMessageType()
	client.lastActivity = time.Now()
	return client
}

// Codec 獲取客戶端使用的序列化 Codec
func (c *Client) Codec() Codec {
	return c.codec
}

// Release 釋放 Client 回池中
func (c *Client) Release() {
	c.reset()
	clientPool.Put(c)
}

// reset 重置 Client
func (c *Client) reset() {
	c.ID = ""
	c.Conn = nil
	c.Hub = nil
	c.Context = nil
	c.codec = nil
	c.wsFrameType = 0
	c.isClosing = false

	// 清空但保留容量
	for k := range c.Channels {
		delete(c.Channels, k)
	}
	for k := range c.metadata {
		delete(c.metadata, k)
	}

	// 清空通道
	for len(c.Send) > 0 {
		<-c.Send
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

// SetEncryptionKey 設置 per-client AES 加密金鑰（覆寫 hub 層預設）
func (c *Client) SetEncryptionKey(key []byte) {
	c.SetMetadata("_aes_key", key)
}

// SetHMACKey 設置 per-client HMAC 簽名金鑰（覆寫 hub 層預設）
func (c *Client) SetHMACKey(key []byte) {
	c.SetMetadata("_hmac_key", key)
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

	// 統計資訊
	stats struct {
		TotalConnections  int64
		ActiveConnections int32
		MessagesSent      int64
		MessagesReceived  int64
		BytesSent         int64
		BytesReceived     int64
		mu                sync.RWMutex
	}

	// 回調函數
	onConnect    func(*Client)
	onDisconnect func(*Client)
	onMessage    func(*Client, *Message)
}

// NewHub 創建新的 Hub
func NewHub(logger *logger.Logger, config Config) *Hub {
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

// Run 運行 Hub
func (h *Hub) Run(ctx context.Context) {
	// 定期清理不活躍的連接
	cleanupTicker := time.NewTicker(30 * time.Second)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.Shutdown()
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

// handleRegister 處理客戶端註冊
func (h *Hub) handleRegister(client *Client) {
	h.mu.Lock()
	h.clients[client.ID] = client
	h.stats.TotalConnections++
	h.stats.ActiveConnections++
	h.mu.Unlock()

	if h.onConnect != nil {
		h.onConnect(client)
	}

	h.logger.Info("Client %s connected", client.ID)
}

// handleUnregister 處理客戶端註銷
func (h *Hub) handleUnregister(client *Client) {
	h.mu.Lock()
	if _, ok := h.clients[client.ID]; ok {
		delete(h.clients, client.ID)
		h.stats.ActiveConnections--

		// 從所有頻道移除
		for channel := range client.Channels {
			if clients, ok := h.channels[channel]; ok {
				delete(clients, client)
				if len(clients) == 0 {
					delete(h.channels, channel)
				}
			}
		}

		// 從所有房間移除
		for _, room := range h.rooms {
			room.RemoveClient(client)
		}
	}
	h.mu.Unlock()

	if h.onDisconnect != nil {
		h.onDisconnect(client)
	}

	close(client.Send)
	client.Release() // 返回池中
	h.logger.Info("Client %s disconnected", client.ID)
}

// handleBroadcast 處理廣播（支持跨協議序列化 + 安全管線）
func (h *Hub) handleBroadcast(msg *Message) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	marshalForClients(msg, clients, h.security, func(n int64) {
		h.stats.MessagesSent++
		h.stats.BytesSent += n
	})
}

// cleanupInactiveClients 清理不活躍的客戶端
func (h *Hub) cleanupInactiveClients() {
	h.mu.RLock()
	now := time.Now()
	timeout := 2 * h.config.PongTimeout

	var inactiveClients []*Client
	for _, client := range h.clients {
		if now.Sub(client.lastActivity) > timeout {
			inactiveClients = append(inactiveClients, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range inactiveClients {
		h.logger.Debug("Cleaning up inactive client: %s", client.ID)
		h.unregister <- client
	}
}

// ServeHTTP 處理 WebSocket 升級（整合 HypGo Context）
func (h *Hub) ServeHTTP(c *hypcontext.Context) {
	conn, err := h.upgrader.upgrader.Upgrade(c.Response, c.Request, nil)
	if err != nil {
		h.logger.Warning("WebSocket upgrade failed: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// 生成或獲取客戶端 ID
	clientID := c.GetHeader("X-Client-ID")
	if clientID == "" {
		clientID = c.Query("client_id")
		if clientID == "" {
			clientID = fmt.Sprintf("client-%d", time.Now().UnixNano())
		}
	}

	// 根據協商的子協議選擇 Codec
	codec := CodecByName(conn.Subprotocol())

	// 從池中獲取客戶端
	client := AcquireClient(clientID, conn, h, codec)
	client.Context = c // 關聯 HypGo Context

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
		client.lastActivity = time.Now()
		conn.SetReadDeadline(time.Now().Add(h.config.PongTimeout))
		return nil
	})

	// 註冊客戶端
	h.register <- client

	// 啟動讀寫循環
	go client.writePump(h.config)
	go client.readPump(h.config)
}

// ===== Client 讀寫方法 =====

// readPump 讀取循環
func (c *Client) readPump(config Config) {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.logger.Warning("WebSocket error for client %s: %v", c.ID, err)
			}
			break
		}

		c.lastActivity = time.Now()
		c.Hub.stats.MessagesReceived++
		c.Hub.stats.BytesReceived += int64(len(data))

		// 安全管線：解密 + 驗證簽名
		if c.Hub.security != nil {
			var secErr error
			data, secErr = applySecurityIn(data, c, c.Hub.security)
			if secErr != nil {
				c.Hub.logger.Warning("Security verification failed for client %s: %v", c.ID, secErr)
				continue
			}
		}

		// 從池中獲取 Message
		msg := AcquireMessage()

		if err := c.codec.Unmarshal(data, msg); err != nil {
			c.Hub.logger.Warning("Invalid message format from client %s: %v", c.ID, err)
			msg.Release()
			continue
		}

		msg.ClientID = c.ID

		// 觸發回調
		if c.Hub.onMessage != nil {
			c.Hub.onMessage(c, msg)
		}

		c.handleMessage(msg)
		msg.Release()
	}
}

// writePump 寫入循環
func (c *Client) writePump(config Config) {
	ticker := time.NewTicker(config.PingInterval)
	defer func() {
		if r := recover(); r != nil {
			// 防止 nil Conn 導致 panic 崩潰整個程式
		}
		ticker.Stop()
		if c.Conn != nil {
			c.Conn.Close()
		}
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if c.Conn == nil {
				return
			}
			c.Conn.SetWriteDeadline(time.Now().Add(config.WriteTimeout))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// 批量發送優化（使用協商的 frame 類型）
			c.Conn.WriteMessage(c.wsFrameType, message)

			// 檢查是否有更多消息可以批量發送
			n := len(c.Send)
			for i := 0; i < n; i++ {
				c.Conn.WriteMessage(c.wsFrameType, <-c.Send)
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

// handleMessage 處理訊息
func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case "subscribe":
		if ch := c.extractChannel(msg.Data); ch != "" {
			c.Subscribe(ch)
		}

	case "unsubscribe":
		if ch := c.extractChannel(msg.Data); ch != "" {
			c.Unsubscribe(ch)
		}

	case "publish":
		c.Hub.PublishToChannel(msg.Channel, msg)

	case "broadcast":
		c.Hub.BroadcastMessage(msg)

	case "join_room":
		if roomID := c.extractRoomID(msg.Data); roomID != "" {
			c.JoinRoom(roomID)
		}

	case "leave_room":
		if roomID := c.extractRoomID(msg.Data); roomID != "" {
			c.LeaveRoom(roomID)
		}
	}
}

// ===== 頻道管理 =====

// Subscribe 訂閱頻道
func (c *Client) Subscribe(channel string) {
	c.mu.Lock()
	c.Channels[channel] = true
	c.mu.Unlock()

	c.Hub.mu.Lock()
	if c.Hub.channels[channel] == nil {
		c.Hub.channels[channel] = make(map[*Client]bool)
	}
	c.Hub.channels[channel][c] = true
	c.Hub.mu.Unlock()

	c.Hub.logger.Debug("Client %s subscribed to channel %s", c.ID, channel)
}

// Unsubscribe 取消訂閱頻道
func (c *Client) Unsubscribe(channel string) {
	c.mu.Lock()
	delete(c.Channels, channel)
	c.mu.Unlock()

	c.Hub.mu.Lock()
	if clients, ok := c.Hub.channels[channel]; ok {
		delete(clients, c)
		if len(clients) == 0 {
			delete(c.Hub.channels, channel)
		}
	}
	c.Hub.mu.Unlock()

	c.Hub.logger.Debug("Client %s unsubscribed from channel %s", c.ID, channel)
}

// ===== Room 管理 =====

// Room WebSocket 房間
type Room struct {
	ID           string
	Clients      map[*Client]bool
	Metadata     map[string]interface{}
	mu           sync.RWMutex
	created      time.Time
	lastActivity time.Time
}

// AcquireRoom 從池中獲取 Room
func AcquireRoom(id string) *Room {
	room := roomPool.Get().(*Room)
	room.reset()
	room.ID = id
	room.created = time.Now()
	room.lastActivity = time.Now()
	return room
}

// Release 釋放 Room 回池中
func (r *Room) Release() {
	r.reset()
	roomPool.Put(r)
}

// reset 重置 Room
func (r *Room) reset() {
	r.ID = ""
	for client := range r.Clients {
		delete(r.Clients, client)
	}
	for k := range r.Metadata {
		delete(r.Metadata, k)
	}
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

// Broadcast 房間廣播（原始位元組，向後兼容）
func (r *Room) Broadcast(data []byte) {
	r.mu.RLock()
	clients := make([]*Client, 0, len(r.Clients))
	for client := range r.Clients {
		clients = append(clients, client)
	}
	r.mu.RUnlock()

	for _, client := range clients {
		select {
		case client.Send <- data:
		default:
			// 緩衝區滿，跳過
		}
	}
}

// BroadcastMessage 房間廣播（結構化 Message，支持跨協議序列化 + 安全管線）
// 透過客戶端的 Hub 取得安全配置
func (r *Room) BroadcastMessage(msg *Message) {
	r.mu.RLock()
	clients := make([]*Client, 0, len(r.Clients))
	for client := range r.Clients {
		clients = append(clients, client)
	}
	r.mu.RUnlock()

	// 從第一個客戶端取得 Hub 的安全配置
	var security *SecurityConfig
	if len(clients) > 0 && clients[0].Hub != nil {
		security = clients[0].Hub.security
	}

	marshalForClients(msg, clients, security, nil)
}

// JoinRoom 加入房間
func (c *Client) JoinRoom(roomID string) {
	c.Hub.mu.Lock()
	room, exists := c.Hub.rooms[roomID]
	if !exists {
		room = AcquireRoom(roomID)
		c.Hub.rooms[roomID] = room
	}
	c.Hub.mu.Unlock()

	room.AddClient(c)
	c.Hub.logger.Debug("Client %s joined room %s", c.ID, roomID)
}

// LeaveRoom 離開房間
func (c *Client) LeaveRoom(roomID string) {
	c.Hub.mu.RLock()
	room, exists := c.Hub.rooms[roomID]
	c.Hub.mu.RUnlock()

	if exists {
		room.RemoveClient(c)
		c.Hub.logger.Debug("Client %s left room %s", c.ID, roomID)

		// 如果房間為空，刪除房間
		if len(room.Clients) == 0 {
			c.Hub.mu.Lock()
			delete(c.Hub.rooms, roomID)
			c.Hub.mu.Unlock()
			room.Release() // 返回池中
		}
	}
}

// ===== Hub 便利方法 =====

// Broadcast 廣播訊息（原始位元組，向後兼容）
// 將 data 包裝為 Message 後發送到廣播通道
func (h *Hub) Broadcast(data []byte) {
	msg := AcquireMessage()
	msg.Type = "broadcast"
	msg.Data = data
	h.broadcast <- msg
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
	h.broadcast <- clone
}

// PublishToChannel 發布結構化 Message 到頻道（支持跨協議序列化）
func (h *Hub) PublishToChannel(channel string, msg *Message) {
	h.mu.RLock()
	clients := make([]*Client, 0)
	if channelClients, ok := h.channels[channel]; ok {
		for client := range channelClients {
			clients = append(clients, client)
		}
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

	marshalForClients(pubMsg, clients, h.security, func(n int64) {
		h.stats.MessagesSent++
		h.stats.BytesSent += n
	})
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

	select {
	case client.Send <- msgBytes:
		h.stats.MessagesSent++
		h.stats.BytesSent += int64(len(msgBytes))
		return nil
	default:
		return fmt.Errorf("client %s send buffer full", clientID)
	}
}

// GetStats 獲取統計資訊
func (h *Hub) GetStats() map[string]interface{} {
	h.mu.RLock()
	h.stats.mu.RLock()
	defer h.mu.RUnlock()
	defer h.stats.mu.RUnlock()

	channelStats := make(map[string]int)
	for channel, clients := range h.channels {
		channelStats[channel] = len(clients)
	}

	roomStats := make(map[string]int)
	for roomID, room := range h.rooms {
		roomStats[roomID] = len(room.Clients)
	}

	return map[string]interface{}{
		"total_connections":  h.stats.TotalConnections,
		"active_connections": h.stats.ActiveConnections,
		"messages_sent":      h.stats.MessagesSent,
		"messages_received":  h.stats.MessagesReceived,
		"bytes_sent":         h.stats.BytesSent,
		"bytes_received":     h.stats.BytesReceived,
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

// Shutdown 關閉 Hub
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 關閉所有客戶端連接
	for _, client := range h.clients {
		client.Conn.Close()
	}

	// 清空所有資料
	h.clients = make(map[string]*Client)
	h.channels = make(map[string]map[*Client]bool)
	h.rooms = make(map[string]*Room)

	h.logger.Info("WebSocket Hub shutdown completed")
}
