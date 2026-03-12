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
		client := AcquireClient("test-client", conn, hub, codecJSON)
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

func TestSubprotocolNegotiation(t *testing.T) {
	log := logger.NewLogger()
	hub := NewHub(log, DefaultConfig)
	upgrader := NewUpgrader(DefaultConfig)

	// 驗證 upgrader 包含子協議配置
	if len(upgrader.upgrader.Subprotocols) != 4 {
		t.Errorf("Expected 4 subprotocols, got %d", len(upgrader.upgrader.Subprotocols))
	}
	expectedSubs := []string{"json", "protobuf", "flatbuffers", "msgpack"}
	for i, expected := range expectedSubs {
		if i < len(upgrader.upgrader.Subprotocols) && upgrader.upgrader.Subprotocols[i] != expected {
			t.Errorf("Subprotocol[%d] should be %q, got %q", i, expected, upgrader.upgrader.Subprotocols[i])
		}
	}

	// 測試 JSON 子協議協商
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		codec := CodecByName(conn.Subprotocol())
		client := AcquireClient("test-sub", conn, hub, codec)

		if client.codec.Name() != "json" {
			t.Errorf("Expected JSON codec, got %q", client.codec.Name())
		}
		if client.wsFrameType != websocket.TextMessage {
			t.Errorf("Expected TextMessage frame type, got %d", client.wsFrameType)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// 使用 json 子協議連接
	dialer := websocket.Dialer{Subprotocols: []string{"json"}}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	if resp.Header.Get("Sec-Websocket-Protocol") != "json" {
		t.Errorf("Expected 'json' subprotocol in response, got %q", resp.Header.Get("Sec-Websocket-Protocol"))
	}
}

func TestProtobufSubprotocolNegotiation(t *testing.T) {
	log := logger.NewLogger()
	hub := NewHub(log, DefaultConfig)
	upgrader := NewUpgrader(DefaultConfig)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		codec := CodecByName(conn.Subprotocol())
		client := AcquireClient("test-pb", conn, hub, codec)

		if client.codec.Name() != "protobuf" {
			t.Errorf("Expected Protobuf codec, got %q", client.codec.Name())
		}
		if client.wsFrameType != websocket.BinaryMessage {
			t.Errorf("Expected BinaryMessage frame type, got %d", client.wsFrameType)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// 使用 protobuf 子協議連接
	dialer := websocket.Dialer{Subprotocols: []string{"protobuf"}}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	if resp.Header.Get("Sec-Websocket-Protocol") != "protobuf" {
		t.Errorf("Expected 'protobuf' subprotocol in response, got %q", resp.Header.Get("Sec-Websocket-Protocol"))
	}
}

func TestDefaultCodecIsJSON(t *testing.T) {
	// 無子協議協商時預設使用 JSON
	codec := CodecByName("")
	if codec.Name() != "json" {
		t.Errorf("Default codec should be JSON, got %q", codec.Name())
	}

	// 客戶端使用預設 codec
	log := logger.NewLogger()
	hub := NewHub(log, DefaultConfig)
	upgrader := NewUpgrader(DefaultConfig)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// 不指定子協議時 Subprotocol() 返回空字串
		codec := CodecByName(conn.Subprotocol())
		client := AcquireClient("test-default", conn, hub, codec)

		if client.codec.Name() != "json" {
			t.Errorf("Default client codec should be JSON, got %q", client.codec.Name())
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// 不指定子協議連接（向後兼容）
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()
}

func TestClientCodecGetter(t *testing.T) {
	log := logger.NewLogger()
	hub := NewHub(log, DefaultConfig)

	// JSON client
	jsonClient := AcquireClient("json-1", nil, hub, codecJSON)
	if jsonClient.Codec().Name() != "json" {
		t.Errorf("Expected 'json', got %q", jsonClient.Codec().Name())
	}

	// Protobuf client
	pbClient := AcquireClient("pb-1", nil, hub, codecProtobuf)
	if pbClient.Codec().Name() != "protobuf" {
		t.Errorf("Expected 'protobuf', got %q", pbClient.Codec().Name())
	}

	// Reset 後 codec 應為 nil
	pbClient.reset()
	if pbClient.codec != nil {
		t.Error("codec should be nil after reset")
	}
	if pbClient.wsFrameType != 0 {
		t.Error("wsFrameType should be 0 after reset")
	}
}
