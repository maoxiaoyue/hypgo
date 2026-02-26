package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

func TestNewHub(t *testing.T) {
	log := logger.NewLogger()
	hub := NewHub(log, DefaultConfig)

	if hub == nil {
		t.Fatalf("NewHub returned nil")
	}

	if hub.config.ReadBufferSize != DefaultConfig.ReadBufferSize {
		t.Errorf("Config not set correctly")
	}

	stats := hub.GetStats()
	if stats["total_clients"] != 0 {
		t.Errorf("Expected 0 clients, got %v", stats["total_clients"])
	}
}

func TestHub_RunAndShutdown(t *testing.T) {
	log := logger.NewLogger()
	hub := NewHub(log, DefaultConfig)

	ctx, cancel := context.WithCancel(context.Background())

	// Start hub
	go hub.Run(ctx)

	// Give it a moment to run
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	cancel()
	hub.Shutdown()
}

func TestUpgrader_Upgrade(t *testing.T) {
	log := logger.NewLogger()
	hub := NewHub(log, DefaultConfig)
	upgrader := NewUpgrader(DefaultConfig)

	// A simple HTTP handler that upgrades to WebSocket
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock hypcontext locally isn't strictly necessary if we just use upgrader direct for testing basic upgrade config works
		conn, err := upgrader.upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade failed: %v", err)
			return
		}

		// Connect to the Hub (Simulate ServeHTTP)
		client := AcquireClient("test-client", conn, hub)
		hub.register <- client

		// Immediately close the connection for test
		conn.Close()
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect to the server
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial WebSocket server: %v", err)
	}

	// Wait for the server to close the connection
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Errorf("Expected error reading from closed connection")
	}
}

func TestAcquireReleaseMessage(t *testing.T) {
	msg := AcquireMessage()
	if msg == nil {
		t.Fatalf("Failed to acquire message")
	}

	msg.Type = "test"
	msg.Channel = "channel1"

	msg.Release()

	// The values should be reset, although we can't safely assert it directly after Put
	// because another goroutine might Get it. But for a single test:
	msg2 := AcquireMessage()
	if msg2.Type != "" || msg2.Channel != "" {
		t.Errorf("Message not reset upon release/acquire: type=%q, channel=%q", msg2.Type, msg2.Channel)
	}
	msg2.Release()
}
