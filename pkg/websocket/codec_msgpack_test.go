package websocket

import (
	"encoding/json"
	"testing"

	gorillaWs "github.com/gorilla/websocket"
)

func TestMsgpackCodecRoundTrip(t *testing.T) {
	codec := MsgpackCodec{}

	original := &Message{
		Type:      "subscribe",
		Channel:   "events",
		Data:      json.RawMessage(`{"key":"value","nested":{"a":1}}`),
		Timestamp: 9876543210,
		ClientID:  "client-mp",
	}

	data, err := codec.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Marshal returned empty data")
	}

	decoded := &Message{}
	if err := codec.Unmarshal(data, decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.Channel != original.Channel {
		t.Errorf("Channel: got %q, want %q", decoded.Channel, original.Channel)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp: got %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if decoded.ClientID != original.ClientID {
		t.Errorf("ClientID: got %q, want %q", decoded.ClientID, original.ClientID)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("Data: got %q, want %q", string(decoded.Data), string(original.Data))
	}
}

func TestMsgpackCodecEmptyData(t *testing.T) {
	codec := MsgpackCodec{}

	original := &Message{Type: "ping"}

	data, err := codec.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	decoded := &Message{}
	if err := codec.Unmarshal(data, decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != "ping" {
		t.Errorf("Type: got %q, want %q", decoded.Type, "ping")
	}
}

func TestMsgpackCodecLargeData(t *testing.T) {
	codec := MsgpackCodec{}

	largePayload := make([]byte, 4096)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	original := &Message{
		Type:      "publish",
		Channel:   "data-stream",
		Data:      json.RawMessage(largePayload),
		Timestamp: 1000000,
		ClientID:  "producer-1",
	}

	data, err := codec.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	decoded := &Message{}
	if err := codec.Unmarshal(data, decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(decoded.Data) != len(original.Data) {
		t.Errorf("Data length: got %d, want %d", len(decoded.Data), len(original.Data))
	}
}

func TestMsgpackWebSocketMessageType(t *testing.T) {
	codec := MsgpackCodec{}
	if codec.WebSocketMessageType() != gorillaWs.BinaryMessage {
		t.Errorf("Should return BinaryMessage, got %d", codec.WebSocketMessageType())
	}
}

func TestMsgpackCodecIndex(t *testing.T) {
	codec := MsgpackCodec{}
	if codec.Index() != 3 {
		t.Errorf("Index: got %d, want 3", codec.Index())
	}
}

func TestMsgpackJsonRawMessagePreservation(t *testing.T) {
	codec := MsgpackCodec{}

	// 驗證 JSON 字串在 MessagePack round-trip 後保持一致
	jsonStr := `{"users":[{"name":"Alice","age":30},{"name":"Bob","age":25}]}`
	original := &Message{
		Type: "message",
		Data: json.RawMessage(jsonStr),
	}

	data, err := codec.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	decoded := &Message{}
	if err := codec.Unmarshal(data, decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if string(decoded.Data) != jsonStr {
		t.Errorf("JSON data not preserved:\ngot:  %s\nwant: %s", string(decoded.Data), jsonStr)
	}
}
