package websocket

import (
	"encoding/json"
	"fmt"

	gorillaWs "github.com/gorilla/websocket"
	wspb "github.com/maoxiaoyue/hypgo/pkg/websocket/proto"
)

// Codec 訊息序列化/反序列化介面
// 抽象 JSON、Protobuf、FlatBuffers、MessagePack 等編解碼差異
type Codec interface {
	// Name 返回 codec 識別名稱（用於子協議協商）
	Name() string

	// Index 返回穩定唯一索引（用於 marshalForClients 快取鍵）
	// JSON=0, Protobuf=1, FlatBuffers=2, MessagePack=3
	Index() int

	// Marshal 將 Message 序列化為線格式位元組
	Marshal(msg *Message) ([]byte, error)

	// Unmarshal 從線格式位元組反序列化到 Message
	Unmarshal(data []byte, msg *Message) error

	// WebSocketMessageType 返回 WebSocket frame 類型
	// TextMessage (JSON) 或 BinaryMessage (Protobuf/FlatBuffers/MessagePack)
	WebSocketMessageType() int
}

// ControlDecoder 可選介面：非 JSON codec 用於解析控制訊息的 data 欄位
// 若 codec 未實現此介面，extractChannel/extractRoomID 使用 JSON 解析（預設路徑）
type ControlDecoder interface {
	DecodeChannel(data []byte) string
	DecodeRoomID(data []byte) string
}

// ===== 預設 Codec 實例 =====

var (
	codecJSON        Codec = JSONCodec{}
	codecProtobuf    Codec = ProtobufCodec{}
	codecFlatBuffers Codec = FlatBuffersCodec{}
	codecMsgpack     Codec = MsgpackCodec{}
)

// CodecByName 根據子協議名稱查找 Codec
// 空字串或未知名稱返回 JSONCodec（向後兼容）
func CodecByName(name string) Codec {
	switch name {
	case "protobuf":
		return codecProtobuf
	case "flatbuffers":
		return codecFlatBuffers
	case "msgpack":
		return codecMsgpack
	default:
		return codecJSON
	}
}

// ===== JSONCodec =====

// JSONCodec JSON 序列化 Codec（預設）
type JSONCodec struct{}

func (JSONCodec) Name() string  { return "json" }
func (JSONCodec) Index() int    { return 0 }

func (JSONCodec) Marshal(msg *Message) ([]byte, error) {
	return json.Marshal(msg)
}

func (JSONCodec) Unmarshal(data []byte, msg *Message) error {
	return json.Unmarshal(data, msg)
}

func (JSONCodec) WebSocketMessageType() int {
	return gorillaWs.TextMessage
}

// ===== ProtobufCodec =====

// ProtobufCodec Protocol Buffers 二進制序列化 Codec
type ProtobufCodec struct{}

func (ProtobufCodec) Name() string  { return "protobuf" }
func (ProtobufCodec) Index() int    { return 1 }

func (ProtobufCodec) Marshal(msg *Message) ([]byte, error) {
	pbMsg := &wspb.WsMessage{
		Type:      msg.Type,
		Channel:   msg.Channel,
		Data:      []byte(msg.Data),
		Timestamp: msg.Timestamp,
		ClientId:  msg.ClientID,
	}
	return pbMsg.Marshal()
}

func (ProtobufCodec) Unmarshal(data []byte, msg *Message) error {
	pbMsg := &wspb.WsMessage{}
	if err := pbMsg.Unmarshal(data); err != nil {
		return fmt.Errorf("protobuf unmarshal failed: %w", err)
	}
	msg.Type = pbMsg.Type
	msg.Channel = pbMsg.Channel
	msg.Data = json.RawMessage(pbMsg.Data)
	msg.Timestamp = pbMsg.Timestamp
	msg.ClientID = pbMsg.ClientId
	return nil
}

func (ProtobufCodec) WebSocketMessageType() int {
	return gorillaWs.BinaryMessage
}

// DecodeChannel 實現 ControlDecoder — 解碼 Protobuf ChannelRequest
func (ProtobufCodec) DecodeChannel(data []byte) string {
	req := &wspb.ChannelRequest{}
	if err := req.Unmarshal(data); err == nil {
		return req.Channel
	}
	return ""
}

// DecodeRoomID 實現 ControlDecoder — 解碼 Protobuf RoomRequest
func (ProtobufCodec) DecodeRoomID(data []byte) string {
	req := &wspb.RoomRequest{}
	if err := req.Unmarshal(data); err == nil {
		return req.RoomId
	}
	return ""
}

// ===== 跨協議廣播 Helper =====

// marshalForClients 將 Message 序列化後發送給多個客戶端
// 使用惰性序列化：每種 codec 最多序列化一次，無論有多少客戶端
// 支援安全管線（AES 加密 / HMAC 簽名），安全管線逐 client 套用
func marshalForClients(msg *Message, clients []*Client, security *SecurityConfig, onSent func(n int64)) {
	cache := make(map[int][]byte, 4)
	errs := make(map[int]error, 4)

	for _, client := range clients {
		idx := client.codec.Index()

		// 惰性序列化：同一 codec 只序列化一次
		if _, cached := cache[idx]; !cached {
			if errs[idx] == nil {
				cache[idx], errs[idx] = client.codec.Marshal(msg)
			}
		}
		if errs[idx] != nil {
			continue
		}

		data := cache[idx]

		// 安全管線（逐 client 套用，因金鑰可能不同）
		if security != nil {
			var err error
			data, err = applySecurityOut(data, client, security)
			if err != nil {
				continue
			}
		}

		select {
		case client.Send <- data:
			if onSent != nil {
				onSent(int64(len(data)))
			}
		default:
			// 緩衝區滿，跳過
		}
	}
}
