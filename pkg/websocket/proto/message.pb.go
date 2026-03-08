// Package wspb 提供 WebSocket 訊息的 Protocol Buffers 編解碼
//
// 此檔案使用 protowire 手動實現 protobuf 線格式編解碼，
// 對應 message.proto 中定義的訊息結構。
//
// 若已安裝 protoc，可重新生成：
//
//	protoc --go_out=. --go_opt=paths=source_relative proto/message.proto
package wspb

import (
	"google.golang.org/protobuf/encoding/protowire"
)

// WsMessage 是 WebSocket 訊息的 protobuf 表示
// 對應 proto 定義中的 WsMessage
type WsMessage struct {
	Type      string // field 1
	Channel   string // field 2
	Data      []byte // field 3
	Timestamp int64  // field 4
	ClientId  string // field 5
}

// Marshal 將 WsMessage 編碼為 protobuf 二進制格式
func (m *WsMessage) Marshal() ([]byte, error) {
	var b []byte

	// field 1: type (string)
	if m.Type != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, m.Type)
	}

	// field 2: channel (string)
	if m.Channel != "" {
		b = protowire.AppendTag(b, 2, protowire.BytesType)
		b = protowire.AppendString(b, m.Channel)
	}

	// field 3: data (bytes)
	if len(m.Data) > 0 {
		b = protowire.AppendTag(b, 3, protowire.BytesType)
		b = protowire.AppendBytes(b, m.Data)
	}

	// field 4: timestamp (int64)
	if m.Timestamp != 0 {
		b = protowire.AppendTag(b, 4, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(m.Timestamp))
	}

	// field 5: client_id (string)
	if m.ClientId != "" {
		b = protowire.AppendTag(b, 5, protowire.BytesType)
		b = protowire.AppendString(b, m.ClientId)
	}

	return b, nil
}

// Unmarshal 從 protobuf 二進制格式解碼 WsMessage
func (m *WsMessage) Unmarshal(b []byte) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]

		switch num {
		case 1: // type (string)
			if typ != protowire.BytesType {
				return errFieldType(num, typ)
			}
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.Type = v
			b = b[n:]

		case 2: // channel (string)
			if typ != protowire.BytesType {
				return errFieldType(num, typ)
			}
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.Channel = v
			b = b[n:]

		case 3: // data (bytes)
			if typ != protowire.BytesType {
				return errFieldType(num, typ)
			}
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.Data = append([]byte(nil), v...) // 複製一份，避免引用原始切片
			b = b[n:]

		case 4: // timestamp (int64)
			if typ != protowire.VarintType {
				return errFieldType(num, typ)
			}
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.Timestamp = int64(v)
			b = b[n:]

		case 5: // client_id (string)
			if typ != protowire.BytesType {
				return errFieldType(num, typ)
			}
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.ClientId = v
			b = b[n:]

		default:
			// 跳過未知欄位（向前兼容）
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
		}
	}
	return nil
}

// Reset 重置 WsMessage
func (m *WsMessage) Reset() {
	m.Type = ""
	m.Channel = ""
	m.Data = nil
	m.Timestamp = 0
	m.ClientId = ""
}

// ChannelRequest 頻道操作請求
type ChannelRequest struct {
	Channel string // field 1
}

// Marshal 編碼 ChannelRequest
func (m *ChannelRequest) Marshal() ([]byte, error) {
	var b []byte
	if m.Channel != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, m.Channel)
	}
	return b, nil
}

// Unmarshal 解碼 ChannelRequest
func (m *ChannelRequest) Unmarshal(b []byte) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]

		switch num {
		case 1:
			if typ != protowire.BytesType {
				return errFieldType(num, typ)
			}
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.Channel = v
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
		}
	}
	return nil
}

// RoomRequest 房間操作請求
type RoomRequest struct {
	RoomId string // field 1
}

// Marshal 編碼 RoomRequest
func (m *RoomRequest) Marshal() ([]byte, error) {
	var b []byte
	if m.RoomId != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, m.RoomId)
	}
	return b, nil
}

// Unmarshal 解碼 RoomRequest
func (m *RoomRequest) Unmarshal(b []byte) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]

		switch num {
		case 1:
			if typ != protowire.BytesType {
				return errFieldType(num, typ)
			}
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.RoomId = v
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
		}
	}
	return nil
}

// errFieldType 返回欄位型別不匹配的錯誤
func errFieldType(num protowire.Number, typ protowire.Type) error {
	return protowire.ParseError(-1)
}
