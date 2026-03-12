package websocket

import (
	"encoding/json"
	"testing"

	gorillaWs "github.com/gorilla/websocket"
)

func TestFlatBuffersCodecRoundTrip(t *testing.T) {
	codec := FlatBuffersCodec{}

	original := &Message{
		Type:      "message",
		Channel:   "test-channel",
		Data:      json.RawMessage(`{"key":"value"}`),
		Timestamp: 1234567890,
		ClientID:  "client-fb",
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

func TestFlatBuffersCodecEmptyData(t *testing.T) {
	codec := FlatBuffersCodec{}

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

func TestFlatBuffersCodecLargeData(t *testing.T) {
	codec := FlatBuffersCodec{}

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
		return
	}
	for i := range decoded.Data {
		if decoded.Data[i] != original.Data[i] {
			t.Errorf("Data byte %d: got %d, want %d", i, decoded.Data[i], original.Data[i])
			break
		}
	}
}

func TestFlatBuffersWebSocketMessageType(t *testing.T) {
	codec := FlatBuffersCodec{}
	if codec.WebSocketMessageType() != gorillaWs.BinaryMessage {
		t.Errorf("Should return BinaryMessage, got %d", codec.WebSocketMessageType())
	}
}

func TestFlatBuffersCodecIndex(t *testing.T) {
	codec := FlatBuffersCodec{}
	if codec.Index() != 2 {
		t.Errorf("Index: got %d, want 2", codec.Index())
	}
}
