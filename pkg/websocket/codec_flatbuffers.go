package websocket

import (
	"encoding/json"
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"
	gorillaWs "github.com/gorilla/websocket"
)

// FlatBuffersCodec FlatBuffers 零拷貝二進制序列化 Codec
type FlatBuffersCodec struct{}

func (FlatBuffersCodec) Name() string { return "flatbuffers" }
func (FlatBuffersCodec) Index() int   { return 2 }

// FlatBuffer Table Layout（手動編碼，不需要 flatc 編譯器）:
//
//	vtable offset 4  → field 0: Type (string)
//	vtable offset 6  → field 1: Channel (string)
//	vtable offset 8  → field 2: Data (byte vector)
//	vtable offset 10 → field 3: Timestamp (int64)
//	vtable offset 12 → field 4: ClientID (string)
const (
	fbFieldType      = 0
	fbFieldChannel   = 1
	fbFieldData      = 2
	fbFieldTimestamp  = 3
	fbFieldClientID  = 4
	fbNumFields      = 5
)

// Marshal 使用 FlatBuffers Builder 序列化 Message
func (FlatBuffersCodec) Marshal(msg *Message) ([]byte, error) {
	builder := flatbuffers.NewBuilder(256)

	// FlatBuffers 需要先建立所有 string/vector（底層向上建構）
	typeOff := builder.CreateString(msg.Type)
	channelOff := builder.CreateString(msg.Channel)
	dataOff := builder.CreateByteVector([]byte(msg.Data))
	clientIDOff := builder.CreateString(msg.ClientID)

	// 開始建構 table
	builder.StartObject(fbNumFields)
	builder.PrependUOffsetTSlot(fbFieldType, flatbuffers.UOffsetT(typeOff), 0)
	builder.PrependUOffsetTSlot(fbFieldChannel, flatbuffers.UOffsetT(channelOff), 0)
	builder.PrependUOffsetTSlot(fbFieldData, flatbuffers.UOffsetT(dataOff), 0)
	builder.PrependInt64Slot(fbFieldTimestamp, msg.Timestamp, 0)
	builder.PrependUOffsetTSlot(fbFieldClientID, flatbuffers.UOffsetT(clientIDOff), 0)
	root := builder.EndObject()
	builder.Finish(root)

	// 複製 FinishedBytes 以避免引用 Builder 內部 buffer
	buf := builder.FinishedBytes()
	result := make([]byte, len(buf))
	copy(result, buf)
	return result, nil
}

// Unmarshal 從 FlatBuffers 二進制格式解碼 Message
func (FlatBuffersCodec) Unmarshal(data []byte, msg *Message) error {
	if len(data) < 4 {
		return fmt.Errorf("flatbuffers data too short: %d bytes", len(data))
	}

	// 取得 root table
	n := flatbuffers.GetUOffsetT(data)
	tab := &flatbuffers.Table{}
	tab.Bytes = data
	tab.Pos = flatbuffers.UOffsetT(n)

	// vtable base offset
	vtableOffset := flatbuffers.SOffsetT(tab.Pos) - tab.GetSOffsetT(tab.Pos)

	// 讀取各欄位
	msg.Type = fbReadString(tab, vtableOffset, fbFieldType)
	msg.Channel = fbReadString(tab, vtableOffset, fbFieldChannel)
	msg.Data = json.RawMessage(fbReadByteVector(tab, vtableOffset, fbFieldData))
	msg.Timestamp = fbReadInt64(tab, vtableOffset, fbFieldTimestamp)
	msg.ClientID = fbReadString(tab, vtableOffset, fbFieldClientID)

	return nil
}

func (FlatBuffersCodec) WebSocketMessageType() int {
	return gorillaWs.BinaryMessage
}

// ===== FlatBuffers 讀取 helpers =====

// fbReadString 從 table 讀取 string 欄位
func fbReadString(tab *flatbuffers.Table, vtableOffset flatbuffers.SOffsetT, field int) string {
	o := flatbuffers.VOffsetT(vtableOffset + flatbuffers.SOffsetT(4+field*2))
	if o >= flatbuffers.VOffsetT(len(tab.Bytes)) {
		return ""
	}

	// 讀取 vtable 中此欄位的偏移量
	vtablePos := flatbuffers.UOffsetT(vtableOffset)
	if vtablePos+flatbuffers.UOffsetT(4+field*2+2) > flatbuffers.UOffsetT(len(tab.Bytes)) {
		return ""
	}

	fieldOff := tab.GetVOffsetTSlot(flatbuffers.VOffsetT(4+field*2), 0)
	if fieldOff == 0 {
		return ""
	}

	start := tab.Pos + flatbuffers.UOffsetT(fieldOff)
	strOffset := tab.Pos + flatbuffers.UOffsetT(fieldOff) + flatbuffers.UOffsetT(tab.GetInt32(start))
	strLen := tab.GetInt32(strOffset)
	return string(tab.Bytes[strOffset+4 : strOffset+flatbuffers.UOffsetT(4+strLen)])
}

// fbReadByteVector 從 table 讀取 byte vector 欄位
func fbReadByteVector(tab *flatbuffers.Table, vtableOffset flatbuffers.SOffsetT, field int) []byte {
	_ = vtableOffset // 使用 tab 的方法
	fieldOff := tab.GetVOffsetTSlot(flatbuffers.VOffsetT(4+field*2), 0)
	if fieldOff == 0 {
		return nil
	}

	start := tab.Pos + flatbuffers.UOffsetT(fieldOff)
	vecOffset := start + flatbuffers.UOffsetT(tab.GetInt32(start))
	vecLen := tab.GetInt32(vecOffset)
	if vecLen == 0 {
		return nil
	}

	// 複製以避免引用原始 buffer
	result := make([]byte, vecLen)
	copy(result, tab.Bytes[vecOffset+4:vecOffset+flatbuffers.UOffsetT(4+vecLen)])
	return result
}

// fbReadInt64 從 table 讀取 int64 欄位
func fbReadInt64(tab *flatbuffers.Table, vtableOffset flatbuffers.SOffsetT, field int) int64 {
	_ = vtableOffset
	fieldOff := tab.GetVOffsetTSlot(flatbuffers.VOffsetT(4+field*2), 0)
	if fieldOff == 0 {
		return 0
	}
	return tab.GetInt64(tab.Pos + flatbuffers.UOffsetT(fieldOff))
}
