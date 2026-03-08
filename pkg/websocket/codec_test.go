package websocket

import (
	"encoding/json"
	"testing"

	gorillaWs "github.com/gorilla/websocket"
)

func TestJSONCodecRoundTrip(t *testing.T) {
	codec := JSONCodec{}

	original := &Message{
		Type:      "message",
		Channel:   "test-channel",
		Data:      json.RawMessage(`{"key":"value"}`),
		Timestamp: 1234567890,
		ClientID:  "client-1",
	}

	// Marshal
	data, err := codec.Marshal(original)
	if err != nil {
		t.Fatalf("JSONCodec.Marshal failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("JSONCodec.Marshal returned empty data")
	}

	// Unmarshal
	decoded := &Message{}
	if err := codec.Unmarshal(data, decoded); err != nil {
		t.Fatalf("JSONCodec.Unmarshal failed: %v", err)
	}

	// 驗證欄位
	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.Channel != original.Channel {
		t.Errorf("Channel mismatch: got %q, want %q", decoded.Channel, original.Channel)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if decoded.ClientID != original.ClientID {
		t.Errorf("ClientID mismatch: got %q, want %q", decoded.ClientID, original.ClientID)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("Data mismatch: got %q, want %q", string(decoded.Data), string(original.Data))
	}
}

func TestProtobufCodecRoundTrip(t *testing.T) {
	codec := ProtobufCodec{}

	original := &Message{
		Type:      "subscribe",
		Channel:   "notifications",
		Data:      json.RawMessage(`{"channel":"notifications"}`),
		Timestamp: 9876543210,
		ClientID:  "client-42",
	}

	// Marshal
	data, err := codec.Marshal(original)
	if err != nil {
		t.Fatalf("ProtobufCodec.Marshal failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ProtobufCodec.Marshal returned empty data")
	}

	// Unmarshal
	decoded := &Message{}
	if err := codec.Unmarshal(data, decoded); err != nil {
		t.Fatalf("ProtobufCodec.Unmarshal failed: %v", err)
	}

	// 驗證欄位
	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.Channel != original.Channel {
		t.Errorf("Channel mismatch: got %q, want %q", decoded.Channel, original.Channel)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if decoded.ClientID != original.ClientID {
		t.Errorf("ClientID mismatch: got %q, want %q", decoded.ClientID, original.ClientID)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("Data mismatch: got %q, want %q", string(decoded.Data), string(original.Data))
	}
}

func TestCodecByName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"json", "json"},
		{"protobuf", "protobuf"},
		{"flatbuffers", "flatbuffers"},
		{"msgpack", "msgpack"},
		{"", "json"},           // 空字串預設 JSON
		{"unknown", "json"},    // 未知名稱預設 JSON
		{"xml", "json"},        // 不支援的格式預設 JSON
	}

	for _, tt := range tests {
		codec := CodecByName(tt.name)
		if codec.Name() != tt.expected {
			t.Errorf("CodecByName(%q) = %q, want %q", tt.name, codec.Name(), tt.expected)
		}
	}
}

func TestProtobufCodecWithEmptyData(t *testing.T) {
	codec := ProtobufCodec{}

	// 測試無 data 欄位的訊息
	original := &Message{
		Type:    "ping",
		Channel: "",
	}

	data, err := codec.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	decoded := &Message{}
	if err := codec.Unmarshal(data, decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != "ping" {
		t.Errorf("Type mismatch: got %q, want %q", decoded.Type, "ping")
	}
}

func TestProtobufCodecWithLargeData(t *testing.T) {
	codec := ProtobufCodec{}

	// 測試大 data payload
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
		t.Errorf("Data length mismatch: got %d, want %d", len(decoded.Data), len(original.Data))
	}
	for i := range decoded.Data {
		if decoded.Data[i] != original.Data[i] {
			t.Errorf("Data byte %d mismatch: got %d, want %d", i, decoded.Data[i], original.Data[i])
			break
		}
	}
}

func TestWebSocketMessageType(t *testing.T) {
	jsonCodec := JSONCodec{}
	pbCodec := ProtobufCodec{}

	if jsonCodec.WebSocketMessageType() != gorillaWs.TextMessage {
		t.Errorf("JSONCodec should return TextMessage, got %d", jsonCodec.WebSocketMessageType())
	}

	if pbCodec.WebSocketMessageType() != gorillaWs.BinaryMessage {
		t.Errorf("ProtobufCodec should return BinaryMessage, got %d", pbCodec.WebSocketMessageType())
	}
}

func TestCrossCodecCompatibility(t *testing.T) {
	// 驗證所有 4 種 codec 可以表達相同的 Message
	original := &Message{
		Type:      "broadcast",
		Channel:   "global",
		Data:      json.RawMessage(`{"action":"notify","payload":"hello"}`),
		Timestamp: 5555555555,
		ClientID:  "sender-x",
	}

	codecs := []Codec{JSONCodec{}, ProtobufCodec{}, FlatBuffersCodec{}, MsgpackCodec{}}
	decoded := make([]*Message, len(codecs))
	sizes := make([]int, len(codecs))

	for i, codec := range codecs {
		data, err := codec.Marshal(original)
		if err != nil {
			t.Fatalf("%s marshal failed: %v", codec.Name(), err)
		}
		sizes[i] = len(data)

		decoded[i] = &Message{}
		if err := codec.Unmarshal(data, decoded[i]); err != nil {
			t.Fatalf("%s unmarshal failed: %v", codec.Name(), err)
		}
	}

	// 所有 codec 解碼結果應該相同
	ref := decoded[0] // JSON 作為參考
	for i := 1; i < len(codecs); i++ {
		d := decoded[i]
		name := codecs[i].Name()
		if d.Type != ref.Type {
			t.Errorf("Type differs: json=%q, %s=%q", ref.Type, name, d.Type)
		}
		if d.Channel != ref.Channel {
			t.Errorf("Channel differs: json=%q, %s=%q", ref.Channel, name, d.Channel)
		}
		if d.Timestamp != ref.Timestamp {
			t.Errorf("Timestamp differs: json=%d, %s=%d", ref.Timestamp, name, d.Timestamp)
		}
		if d.ClientID != ref.ClientID {
			t.Errorf("ClientID differs: json=%q, %s=%q", ref.ClientID, name, d.ClientID)
		}
		if string(d.Data) != string(ref.Data) {
			t.Errorf("Data differs: json=%q, %s=%q", string(ref.Data), name, string(d.Data))
		}
	}

	// 輸出各 codec 大小
	t.Logf("Sizes: JSON=%d, Protobuf=%d, FlatBuffers=%d, MessagePack=%d",
		sizes[0], sizes[1], sizes[2], sizes[3])
}

func TestProtobufCodecInvalidData(t *testing.T) {
	codec := ProtobufCodec{}

	// 無效的 protobuf 數據不應該 panic
	invalidData := []byte{0xFF, 0xFE, 0xFD, 0xFC}
	msg := &Message{}
	err := codec.Unmarshal(invalidData, msg)
	// 無效數據可能不會返回錯誤（protowire 可能跳過未知欄位），
	// 但不應該 panic
	_ = err
}

func TestMarshalForClients(t *testing.T) {
	// 創建 4 種 codec 的客戶端
	makeClient := func(id string, codec Codec) *Client {
		return &Client{
			ID:          id,
			codec:       codec,
			wsFrameType: codec.WebSocketMessageType(),
			Send:        make(chan []byte, 10),
			Channels:    make(map[string]bool),
		}
	}

	jsonClient := makeClient("json-client", JSONCodec{})
	pbClient := makeClient("pb-client", ProtobufCodec{})
	fbClient := makeClient("fb-client", FlatBuffersCodec{})
	mpClient := makeClient("mp-client", MsgpackCodec{})

	msg := &Message{
		Type:      "message",
		Channel:   "test",
		Data:      json.RawMessage(`{"text":"hello"}`),
		Timestamp: 12345,
		ClientID:  "sender",
	}

	var totalBytes int64
	clients := []*Client{jsonClient, pbClient, fbClient, mpClient}
	marshalForClients(msg, clients, nil, func(n int64) {
		totalBytes += n
	})

	// 所有客戶端都應該收到訊息
	for _, c := range clients {
		if len(c.Send) != 1 {
			t.Errorf("%s should have 1 message, got %d", c.ID, len(c.Send))
		}
	}
	if totalBytes == 0 {
		t.Error("Expected non-zero totalBytes")
	}

	// 驗證各自的格式能正確反序列化
	codecs := []Codec{JSONCodec{}, ProtobufCodec{}, FlatBuffersCodec{}, MsgpackCodec{}}
	for i, c := range clients {
		data := <-c.Send
		decoded := &Message{}
		if err := codecs[i].Unmarshal(data, decoded); err != nil {
			t.Errorf("%s received invalid data: %v", c.ID, err)
			continue
		}
		if decoded.Type != msg.Type || decoded.Channel != msg.Channel {
			t.Errorf("%s decoded mismatch: got type=%q channel=%q", c.ID, decoded.Type, decoded.Channel)
		}
	}
}

func TestCodecIndexUniqueness(t *testing.T) {
	codecs := []Codec{JSONCodec{}, ProtobufCodec{}, FlatBuffersCodec{}, MsgpackCodec{}}
	indices := make(map[int]string)
	for _, c := range codecs {
		if prev, exists := indices[c.Index()]; exists {
			t.Errorf("Index %d collision: %q and %q", c.Index(), prev, c.Name())
		}
		indices[c.Index()] = c.Name()
	}
}
