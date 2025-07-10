package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// 在生產環境中應該檢查 Origin
		return true
	},
}

type Message struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data"`
}

type Client struct {
	ID       string
	Conn     *websocket.Conn
	Send     chan []byte
	Hub      *Hub
	Channels map[string]bool
	mu       sync.RWMutex
}

type Hub struct {
	clients    map[string]*Client
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	channels   map[string]map[*Client]bool
	logger     *logger.Logger
	mu         sync.RWMutex
}

func NewHub(logger *logger.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		channels:   make(map[string]map[*Client]bool),
		logger:     logger,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			h.mu.Unlock()
			h.logger.Info("Client %s connected", client.ID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.Send)

				// 從所有頻道中移除
				for channel := range client.Channels {
					if clients, ok := h.channels[channel]; ok {
						delete(clients, client)
						if len(clients) == 0 {
							delete(h.channels, channel)
						}
					}
				}
			}
			h.mu.Unlock()
			h.logger.Info("Client %s disconnected", client.ID)

		case message := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client.ID)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warning("WebSocket upgrade failed: %v", err)
		return
	}

	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		clientID = fmt.Sprintf("client-%d", time.Now().UnixNano())
	}

	client := &Client{
		ID:       clientID,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		Hub:      h,
		Channels: make(map[string]bool),
	}

	client.Hub.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512 * 1024)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.logger.Warning("WebSocket error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			c.Hub.logger.Warning("Invalid message format: %v", err)
			continue
		}

		c.handleMessage(msg)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.Conn.WriteMessage(websocket.TextMessage, message)

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case "subscribe":
		var data struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return
		}
		c.Subscribe(data.Channel)

	case "unsubscribe":
		var data struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return
		}
		c.Unsubscribe(data.Channel)

	case "publish":
		c.Hub.PublishToChannel(msg.Channel, msg.Data)

	case "broadcast":
		c.Hub.Broadcast(msg.Data)
	}
}

func (c *Client) Subscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Channels[channel] = true

	c.Hub.mu.Lock()
	if c.Hub.channels[channel] == nil {
		c.Hub.channels[channel] = make(map[*Client]bool)
	}
	c.Hub.channels[channel][c] = true
	c.Hub.mu.Unlock()

	c.Hub.logger.Debug("Client %s subscribed to channel %s", c.ID, channel)
}

func (c *Client) Unsubscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.Channels, channel)

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

func (h *Hub) Broadcast(data []byte) {
	h.broadcast <- data
}

func (h *Hub) PublishToChannel(channel string, data []byte) {
	h.mu.RLock()
	clients := h.channels[channel]
	h.mu.RUnlock()

	msg := Message{
		Type:    "message",
		Channel: channel,
		Data:    data,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		h.logger.Warning("Failed to marshal message: %v", err)
		return
	}

	for client := range clients {
		select {
		case client.Send <- msgBytes:
		default:
			close(client.Send)
			delete(clients, client)
		}
	}
}

func (h *Hub) GetClient(clientID string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	client, ok := h.clients[clientID]
	return client, ok
}

func (h *Hub) GetChannelClients(channel string) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var clients []*Client
	if channelClients, ok := h.channels[channel]; ok {
		for client := range channelClients {
			clients = append(clients, client)
		}
	}
	return clients
}

// WebSocket 中間件
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 在這裡實現認證邏輯
		// 例如：檢查 JWT token
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		// TODO: 驗證 token

		next(w, r)
	}
}

// 便利函數
func (h *Hub) SendToClient(clientID string, data interface{}) error {
	client, ok := h.GetClient(clientID)
	if !ok {
		return fmt.Errorf("client %s not found", clientID)
	}

	msgBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	select {
	case client.Send <- msgBytes:
		return nil
	default:
		return fmt.Errorf("client %s send buffer full", clientID)
	}
}

func (h *Hub) BroadcastJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.Broadcast(data)
	return nil
}

func (h *Hub) PublishToChannelJSON(channel string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.PublishToChannel(channel, data)
	return nil
}

// 統計資訊
func (h *Hub) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	channelStats := make(map[string]int)
	for channel, clients := range h.channels {
		channelStats[channel] = len(clients)
	}

	return map[string]interface{}{
		"total_clients":  len(h.clients),
		"total_channels": len(h.channels),
		"channels":       channelStats,
	}
}

// 群組管理
type Room struct {
	ID      string
	Clients map[*Client]bool
	mu      sync.RWMutex
}

func (h *Hub) CreateRoom(roomID string) *Room {
	return &Room{
		ID:      roomID,
		Clients: make(map[*Client]bool),
	}
}

func (r *Room) AddClient(client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Clients[client] = true
}

func (r *Room) RemoveClient(client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Clients, client)
}

func (r *Room) Broadcast(data []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for client := range r.Clients {
		select {
		case client.Send <- data:
		default:
			close(client.Send)
			delete(r.Clients, client)
		}
	}
}
