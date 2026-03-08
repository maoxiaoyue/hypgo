package websocket

import (
	"encoding/json"

	gorillaWs "github.com/gorilla/websocket"
	"github.com/vmihailenco/msgpack/v5"
)

// MsgpackCodec MessagePack 二進制序列化 Codec
type MsgpackCodec struct{}

func (MsgpackCodec) Name() string { return "msgpack" }
func (MsgpackCodec) Index() int   { return 3 }

// Marshal 使用 MessagePack 序列化
// 透過 msgpackMessage 中介結構處理 json.RawMessage → []byte 轉換
func (MsgpackCodec) Marshal(msg *Message) ([]byte, error) {
	m := &msgpackMessage{
		Type:      msg.Type,
		Channel:   msg.Channel,
		Data:      []byte(msg.Data),
		Timestamp: msg.Timestamp,
		ClientID:  msg.ClientID,
	}
	return msgpack.Marshal(m)
}

// Unmarshal 使用 MessagePack 反序列化
func (MsgpackCodec) Unmarshal(data []byte, msg *Message) error {
	m := &msgpackMessage{}
	if err := msgpack.Unmarshal(data, m); err != nil {
		return err
	}
	msg.Type = m.Type
	msg.Channel = m.Channel
	msg.Data = json.RawMessage(m.Data)
	msg.Timestamp = m.Timestamp
	msg.ClientID = m.ClientID
	return nil
}

func (MsgpackCodec) WebSocketMessageType() int {
	return gorillaWs.BinaryMessage
}

// msgpackMessage 是 Message 的 MessagePack 專用中介結構
// 將 json.RawMessage ([]byte) 明確標記為 msgpack bin 型別
type msgpackMessage struct {
	Type      string `msgpack:"type"`
	Channel   string `msgpack:"channel"`
	Data      []byte `msgpack:"data"`
	Timestamp int64  `msgpack:"timestamp"`
	ClientID  string `msgpack:"client_id"`
}
