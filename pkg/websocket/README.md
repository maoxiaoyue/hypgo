# WebSocket Package (`pkg/websocket`)

`websocket` 套件為 HypGo 提供經過高效能最佳化、支援四協議（JSON / Protobuf / FlatBuffers / MessagePack）、內建 AES-256-GCM 加密與 HMAC-SHA256 簽名、permessage-deflate 壓縮、WSS/TLS，以及廣播與房間管理，並且能夠無縫與 `hypcontext.Context` 整合的 WebSocket 實作。

## 主要特色

- **四協議序列化**: 客戶端在 WebSocket 握手時透過 `Sec-WebSocket-Protocol` 子協議選擇序列化格式（`json`、`protobuf`、`flatbuffers`、`msgpack`），伺服器自動協商。預設為 JSON，完全向後兼容。
- **AES-256-GCM 加密**: 訊息載荷透過 AES-256-GCM 加密，每次加密使用隨機 12-byte nonce，支援 Hub 層級與 per-client 金鑰覆寫。
- **HMAC-SHA256 簽名**: 訊息完整性透過 HMAC-SHA256 驗證，偵測傳輸中的竄改攻擊。
- **可組合安全管線**: 支援 Sign-then-Encrypt（預設）或 Encrypt-then-Sign 兩種順序，AES 與 HMAC 可獨立啟用或同時使用。
- **permessage-deflate 壓縮**: 可配置壓縮等級（1-9），降低頻寬佔用。
- **WSS/TLS 支援**: 獨立模式下提供 `ListenAndServeTLS` 快速啟動安全 WebSocket 伺服器。
- **零配置升級**: 內建封裝了 Gorilla WebSocket 的 `Upgrader`，並提供標準設定檔快速進行跨來源檢查與安全性配置。
- **物件池機制 (Object Pooling)**: 將 WebSocket 的連線客戶端 (`Client`)、房間 (`Room`)、訊息物件 (`Message`) 與記憶體緩衝區全面透過 `sync.Pool` 管理，面對高併發連線不易觸發 GC。
- **內建頻道與房間系統**: 提供基於 `Hub` 的集中式管理。開發者可以輕易地管理個別連線對 Channel 與 Room 的訂閱、取消與全區廣播。
- **跨協議廣播**: 同一個頻道或房間中的客戶端可使用不同序列化格式，廣播時每種 codec 最多序列化一次（惰性 N 序列化），不隨客戶端數量線性增長。
- **健康檢測機制**: 內建 Ping/Pong 心跳檢查機制與斷線自動清理死連線 (Cleanup) 迴圈，保持連線池健康度。

## 基礎使用

初始化 `Hub`，定義收到訊息或是連線時的處理邏輯：

```go
package main

import (
	"context"
	"log"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/websocket"
)

func main() {
	r := router.New()
	l := logger.NewLogger()

	// 1. 建立 Hub 來管理所有的連線
	hub := websocket.NewHub(l, websocket.DefaultConfig)

	// 2. 定義各種事件回呼 (Callbacks)
	hub.SetCallbacks(
		func(client *websocket.Client) {
			log.Printf("新連線加入！ID: %s, Codec: %s", client.ID, client.Codec().Name())
		},
		func(client *websocket.Client) {
			log.Printf("連線已中斷！ID: %s", client.ID)
		},
		func(client *websocket.Client, msg *websocket.Message) {
			log.Printf("收到訊息！Type: %s, Data: %s", msg.Type, string(msg.Data))
		},
	)

	// 3. 確保背景開始執行 Hub 的訊息分派與死連線清理機制
	ctx := context.Background()
	go hub.Run(ctx)

	// 4. 定義路由與 WebSocket 連線升級入口
	r.GET("/ws", func(c *hypcontext.Context) {
		hub.ServeHTTP(c)
	})

	// 啟動伺服器...
}
```

## 四協議支援 (JSON / Protobuf / FlatBuffers / MessagePack)

客戶端在建立 WebSocket 連線時，透過標準 `Sec-WebSocket-Protocol` 標頭選擇序列化格式：

```javascript
// JavaScript 客戶端 — JSON（預設）
const ws = new WebSocket("ws://localhost:8080/ws", ["json"]);

// Protobuf（二進制，最小載荷）
const ws = new WebSocket("ws://localhost:8080/ws", ["protobuf"]);

// FlatBuffers（零拷貝二進制）
const ws = new WebSocket("ws://localhost:8080/ws", ["flatbuffers"]);

// MessagePack（緊湊二進制）
const ws = new WebSocket("ws://localhost:8080/ws", ["msgpack"]);

// 不指定子協議時自動使用 JSON（向後兼容）
const ws = new WebSocket("ws://localhost:8080/ws");
```

```go
// Go 客戶端 — 使用 Protobuf 子協議
dialer := websocket.Dialer{
	Subprotocols: []string{"protobuf"},
}
conn, _, err := dialer.Dial("ws://localhost:8080/ws", nil)
```

### Codec 介面

`Codec` 介面抽象了所有序列化格式的編解碼差異：

```go
type Codec interface {
	Name() string                              // "json", "protobuf", "flatbuffers", "msgpack"
	Index() int                                // 穩定唯一索引（用於快取鍵）
	Marshal(msg *Message) ([]byte, error)      // 序列化
	Unmarshal(data []byte, msg *Message) error // 反序列化
	WebSocketMessageType() int                 // TextMessage (JSON) 或 BinaryMessage (其他)
}
```

| Codec | Index | WebSocket Frame | 特點 |
|-------|-------|----------------|------|
| JSON | 0 | TextMessage | 人類可讀，最大相容性 |
| Protobuf | 1 | BinaryMessage | 最小載荷（手動 protowire 編碼） |
| FlatBuffers | 2 | BinaryMessage | 零拷貝存取（手動 Builder API） |
| MessagePack | 3 | BinaryMessage | 緊湊二進制，JSON 超集 |

可透過 `client.Codec()` 取得客戶端協商的 Codec：

```go
hub.SetCallbacks(
	func(client *websocket.Client) {
		codec := client.Codec()
		log.Printf("客戶端 %s 使用 %s 編碼（索引 %d）", client.ID, codec.Name(), codec.Index())
	},
	nil, nil,
)
```

### ControlDecoder 可選介面

控制訊息（subscribe / join_room 等）的 `data` 欄位解析方式取決於 codec 是否實現了 `ControlDecoder`：

```go
type ControlDecoder interface {
	DecodeChannel(data []byte) string
	DecodeRoomID(data []byte) string
}
```

- **ProtobufCodec**: 實現此介面，使用 `ChannelRequest` / `RoomRequest` Protobuf 結構解析。
- **其他 Codec**: 控制訊息 `data` 欄位內部使用 JSON 編碼（預設路徑）。

### 自訂子協議配置

```go
config := websocket.DefaultConfig
config.Subprotocols = []string{"json"}  // 僅允許 JSON
// 或
config.Subprotocols = []string{"json", "protobuf", "flatbuffers", "msgpack"}  // 預設：四者皆支援

hub := websocket.NewHub(l, config)
```

### Protobuf 訊息格式

WebSocket 訊息的 Protobuf schema 定義於 `proto/message.proto`：

```protobuf
message WsMessage {
  string type      = 1;  // 訊息類型
  string channel   = 2;  // 頻道名稱
  bytes  data      = 3;  // 不透明載荷
  int64  timestamp = 4;  // 伺服器時間戳
  string client_id = 5;  // 客戶端標識
}
```

控制訊息（subscribe / join_room 等）的 `data` 欄位使用獨立的 Protobuf 結構：

```protobuf
message ChannelRequest { string channel = 1; }
message RoomRequest    { string room_id = 1; }
```

## 安全層 (AES-256-GCM + HMAC-SHA256)

### 配置

```go
config := websocket.DefaultConfig
config.Security = &websocket.SecurityConfig{
	AESKey:          myAES256Key,  // 32 bytes，nil = 不加密
	HMACKey:         myHMACKey,    // 任意長度，nil = 不簽名
	SignThenEncrypt: true,         // true（預設）: 先簽名再加密
}

hub := websocket.NewHub(l, config)
```

### 安全管線

訊息在 codec 序列化/反序列化的前後自動經過安全管線處理：

**Sign-then-Encrypt（預設，`SignThenEncrypt: true`）：**

```
出站：codec.Marshal → HMAC-Sign → AES-Encrypt → wire
入站：wire → AES-Decrypt → HMAC-Verify → codec.Unmarshal
```

**Encrypt-then-Sign（`SignThenEncrypt: false`）：**

```
出站：codec.Marshal → AES-Encrypt → HMAC-Sign → wire
入站：wire → HMAC-Verify → AES-Decrypt → codec.Unmarshal
```

### AES-256-GCM 加密

- 使用 Go 標準庫 `crypto/aes` + `crypto/cipher`
- 每次加密使用 `crypto/rand` 產生唯一 12-byte nonce
- Nonce 前綴附加於密文之前：`[nonce(12) | ciphertext | GCM-tag(16)]`
- 金鑰必須為 32 bytes（AES-256）

### HMAC-SHA256 簽名

- 使用 Go 標準庫 `crypto/hmac` + `crypto/sha256`
- 32-byte 簽名前綴附加於原始資料之前：`[HMAC(32) | data]`
- 驗證時使用 `hmac.Equal` 常數時間比較，防止時序攻擊

### Per-client 金鑰覆寫

當需要對不同客戶端使用不同金鑰時：

```go
hub.SetCallbacks(
	func(client *websocket.Client) {
		// 根據認證結果設定客戶端專屬金鑰
		clientAESKey := deriveKeyForUser(client.ID)
		client.SetEncryptionKey(clientAESKey)

		clientHMACKey := deriveHMACKeyForUser(client.ID)
		client.SetHMACKey(clientHMACKey)
	},
	nil, nil,
)
```

若未設定 per-client 金鑰，則使用 Hub 層級的 `SecurityConfig` 金鑰。

### 獨立使用加解密函式

安全函式可獨立使用，不限於 WebSocket 場景：

```go
key := make([]byte, 32) // AES-256 金鑰
plaintext := []byte("sensitive data")

// 加密
ciphertext, err := websocket.Encrypt(plaintext, key)

// 解密
decrypted, err := websocket.Decrypt(ciphertext, key)

// 簽名
hmacKey := []byte("my-hmac-secret")
signed := websocket.Sign(plaintext, hmacKey)

// 驗證
verified, err := websocket.Verify(signed, hmacKey)
```

## permessage-deflate 壓縮

### 配置

```go
config := websocket.DefaultConfig
config.Compression = &websocket.CompressionConfig{
	Enabled: true,  // 預設 true
	Level:   6,     // flate 壓縮等級 1-9，0=預設
}

hub := websocket.NewHub(l, config)
```

- `Level` 值越高壓縮率越好但 CPU 消耗越大
- 設為 0 使用 Go 預設壓縮等級
- 與原有 `EnableCompression` 欄位向後兼容：`Compression` 非 nil 時覆寫 `EnableCompression`

## WSS/TLS 支援

### 在 HypGo 框架中

TLS 通常由 `pkg/server` 層處理（`config.Server.TLS`），WebSocket 層自動使用 `wss://`。

### 獨立模式

當 WebSocket Hub 作為獨立伺服器使用時，可透過 `ListenAndServeTLS` 快速啟動：

```go
config := websocket.DefaultConfig
config.TLS = &websocket.TLSConfig{
	CertFile: "/path/to/cert.pem",
	KeyFile:  "/path/to/key.pem",
}
// 或提供預配置的 tls.Config
config.TLS = &websocket.TLSConfig{
	TLSConfig: myTLSConfig,
}

hub := websocket.NewHub(l, config)

handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// ... 處理升級
})

// 啟動 WSS 伺服器
err := hub.ListenAndServeTLS(":8443", handler)
```

## 頻道與房間管理 (Channel & Rooms)

在連線建立後，可以主動改變客戶端的群組狀態，以便進行區塊廣播：

```go
// 讓某個 Client 加入特定頻道
client.Subscribe("news")

// 針對某個頻道內的所有訂閱者推送結構化 Message（支援跨協議 + 安全管線）
msg := websocket.AcquireMessage()
msg.Type = "message"
msg.Channel = "news"
msg.Data = []byte(`{"headline": "Breaking News!"}`)
hub.PublishToChannel("news", msg)
msg.Release()

// 或使用原始位元組的向後兼容 API
hub.PublishToChannelRaw("news", []byte(`{"headline": "Breaking News!"}`))

// 加入遊戲或聊天室用的 Room
client.JoinRoom("room_101")

// 向全域廣播
hub.Broadcast([]byte(`{"event": "server_restart"}`))

// 發送給特定客戶端（自動使用該客戶端的 codec + 安全管線）
hub.SendToClient("client-123", msg)
```

## 完整配置範例

```go
config := websocket.Config{
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	MaxMessageSize:    65536,
	WriteWait:         10 * time.Second,
	PongWait:          60 * time.Second,
	PingPeriod:        54 * time.Second,
	EnableCompression: true,
	Subprotocols:      []string{"json", "protobuf", "flatbuffers", "msgpack"},

	// 壓縮配置（覆寫 EnableCompression）
	Compression: &websocket.CompressionConfig{
		Enabled: true,
		Level:   6,
	},

	// 安全配置
	Security: &websocket.SecurityConfig{
		AESKey:          aes256Key,   // 32 bytes
		HMACKey:         hmacSecret,
		SignThenEncrypt: true,
	},

	// TLS 配置（獨立模式）
	TLS: &websocket.TLSConfig{
		CertFile: "cert.pem",
		KeyFile:  "key.pem",
	},
}

hub := websocket.NewHub(l, config)
```

## 檔案結構

```
pkg/websocket/
├── websocket.go              # 核心：Client, Hub, Room, Config, readPump/writePump,
│                              #   TLS/Security/Compression 整合, ListenAndServeTLS
├── codec.go                  # Codec/ControlDecoder 介面, JSONCodec, ProtobufCodec,
│                              #   marshalForClients (map-based N-codec 快取 + 安全管線)
├── codec_flatbuffers.go      # FlatBuffersCodec（手動 Builder API，零拷貝）
├── codec_msgpack.go          # MsgpackCodec（vmihailenco/msgpack/v5）
├── security.go               # AES-256-GCM 加解密, HMAC-SHA256 簽名/驗證, 安全管線
├── proto/
│   ├── message.proto         # Protobuf schema 定義
│   └── message.pb.go         # Protobuf 編解碼實作
├── codec_test.go             # 跨 codec round-trip, marshalForClients, 索引唯一性
├── codec_flatbuffers_test.go # FlatBuffers 專屬測試
├── codec_msgpack_test.go     # MessagePack 專屬測試
├── security_test.go          # AES/HMAC/管線/per-client 金鑰測試
├── websocket_test.go         # WebSocket 核心 + 子協議協商測試
└── README.md
```

## 依賴

| 套件 | 用途 |
|------|------|
| `github.com/gorilla/websocket` | WebSocket 協議實作 |
| `github.com/google/flatbuffers/go` | FlatBuffers 二進制序列化 |
| `github.com/vmihailenco/msgpack/v5` | MessagePack 二進制序列化 |
| `crypto/aes`, `crypto/cipher` | AES-256-GCM 加密（Go 標準庫） |
| `crypto/hmac`, `crypto/sha256` | HMAC-SHA256 簽名（Go 標準庫） |
| `crypto/tls` | TLS/WSS 支援（Go 標準庫） |

## 更新紀錄

### v0.3.0 — FlatBuffers / MessagePack / WSS / AES / permessage-deflate / HMAC

**新增功能：**

| 功能 | 說明 |
|------|------|
| FlatBuffers Codec | 零拷貝二進制序列化（`codec_flatbuffers.go`），使用手動 Builder API，不需要 `flatc` 編譯器 |
| MessagePack Codec | 緊湊二進制序列化（`codec_msgpack.go`），使用 `vmihailenco/msgpack/v5` |
| AES-256-GCM 加密 | 訊息載荷加密（`security.go`），隨機 nonce、per-client 金鑰覆寫 |
| HMAC-SHA256 簽名 | 訊息完整性驗證（`security.go`），常數時間比較防止時序攻擊 |
| 安全管線 | Sign-then-Encrypt / Encrypt-then-Sign 可組合管線，AES/HMAC 可獨立或同時啟用 |
| permessage-deflate 配置 | `CompressionConfig` 結構，可設定壓縮等級 1-9 |
| WSS/TLS | `TLSConfig` + `Hub.ListenAndServeTLS()` 獨立伺服器 TLS 支援 |

**架構變更：**

| 變更 | 說明 |
|------|------|
| `Codec` 介面 | 新增 `Index() int` 方法，穩定唯一索引用於快取鍵 |
| `ControlDecoder` 介面 | 新增可選介面，取代 `extractChannel`/`extractRoomID` 中的硬編碼型別斷言 |
| `marshalForClients` | 從固定 `[2][]byte` 陣列改為 `map[int][]byte`，支援 N 種 codec 惰性序列化 |
| `Config` 結構 | 新增 `TLS *TLSConfig`、`Security *SecurityConfig`、`Compression *CompressionConfig` |
| `Client` 方法 | 新增 `SetEncryptionKey()`、`SetHMACKey()` per-client 金鑰覆寫 |
| `readPump` / `writePump` | 整合安全管線（入站解密/驗證、出站加密/簽名） |
| Subprotocols | 預設從 `["json", "protobuf"]` 擴展為 `["json", "protobuf", "flatbuffers", "msgpack"]` |

**新增檔案：**

```
codec_flatbuffers.go      — FlatBuffers Codec 實作
codec_flatbuffers_test.go — 5 項測試
codec_msgpack.go          — MessagePack Codec 實作
codec_msgpack_test.go     — 6 項測試
security.go               — AES/HMAC/安全管線
security_test.go          — 13 項測試
```

**新增依賴：**

```
github.com/google/flatbuffers   — FlatBuffers 二進制序列化
github.com/vmihailenco/msgpack/v5 — MessagePack（從 indirect 提升為 direct）
```

**測試覆蓋：** 共 45 項測試全數通過（`go test ./pkg/websocket/... -v`）。

---

### v0.2.0 — Protobuf 雙協議支援

- 新增 `Codec` 介面抽象 JSON / Protobuf 差異
- 新增 `ProtobufCodec`，使用 `protowire` 手動編碼（不依賴 protoc-gen-go runtime）
- 子協議協商（`Sec-WebSocket-Protocol`）
- `marshalForClients` 惰性雙序列化
- Protobuf 控制訊息（`ChannelRequest`、`RoomRequest`）

### v0.1.0 — 初始版本

- JSON 序列化 WebSocket 實作
- Hub / Client / Room / Channel 架構
- sync.Pool 物件池
- Ping/Pong 心跳檢測
- permessage-deflate 基本壓縮
