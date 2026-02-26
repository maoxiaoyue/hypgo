# WebSocket Package (`pkg/websocket`)

`websocket` 套件為 HypGo 提供經過高效能最佳化、包含廣播與房間管理，並且能夠無縫與 `hypcontext.Context` 整合的 WebSocket 實作。

## 主要特色

- **零配置升級**: 內建封裝了 Gorilla WebSocket 的 `Upgrader`，並提供標準設定檔快速進行跨來源檢查與安全性配置。
- **物件池機制 (Object Pooling)**: 將 WebSocket 的連線客戶端 (`Client`)、房間 (`Room`)、訊息物件 (`Message`) 與記憶體緩衝區全面透過 `sync.Pool` 管理，面對高併發連線不易觸發 GC。
- **內建頻道與房間系統**: 提供基於 `Hub` 的集中式管理。開發者可以輕易地管理個別連線對 Channel 與 Room 的訂閱、取消與全區廣播。
- **健康檢測機制**: 內建 Ping/Pong 心跳檢查機制與斷線自動清理死連線 (Cleanup) 迴圈，保持連線池健康度。

## 基礎使用

我們首先初始化一個 `Hub`，定義收到訊息或是連線時的處理邏輯，最後建立 `Upgrader`：

```go
package main

import (
	"context"
	"log"
	"net/http"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/websocket"
)

func main() {
	r := router.New()

	// 1. 建立 Hub 來管理所有的連線
	hub := websocket.NewHub(nil, websocket.DefaultConfig)

	// 2. 定義各種事件回呼 (Callbacks)
	hub.SetCallbacks(
		func(client *websocket.Client) {
			log.Printf("新連線加入！ID: %s", client.ID)
		},
		func(client *websocket.Client) {
			log.Printf("連線已中斷！ID: %s", client.ID)
		},
		func(client *websocket.Client, msg *websocket.Message) {
			log.Printf("收到訊息！Type: %s, Data: %s", msg.Type, string(msg.Data))
			
			// 可以直接回應給該客戶端
			client.Send <- msg.Data
		},
	)

	// 3. 確保背景開始執行 Hub 的訊息分派與死連線清理機制
	ctx := context.Background()
	go hub.Run(ctx) 

	// 4. 定義路由與 WebSocket 連線升級入口
	upgrader := websocket.NewUpgrader(websocket.DefaultConfig)

	r.GET("/ws", func(c *hypcontext.Context) {
		// 將一般 HTTP 請求升級為 WS
		err := hub.ServeHTTP(upgrader, c.Writer, c.Request, c)
		if err != nil {
			log.Printf("WebSocket 升級失敗: %v", err)
		}
	})

	// 啟動伺服器...
}
```

## 頻道與房間管理 (Channel & Rooms)

在連線建立後，你可以主動改變客戶端的群組狀態，以便進行區塊廣播：

```go
// 讓某個 Client 加入特定頻道
client.Subscribe("news")

// 針對某個頻道內的所有訂閱者推送訊息
hub.PublishToChannel("news", []byte(`{"message": "Breaking News!"}`))

// 加入遊戲或聊天室用的 Room
client.JoinRoom("room_101")

// 向房間廣播
hub.BroadcastToRoom("room_101", []byte("Hello Room!"))
```
