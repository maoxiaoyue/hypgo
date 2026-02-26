# Server Package (`pkg/server`)

`server` 套件是 HypGo 框架的網路層核心，負責傾聽連接埠、處理連線，以及協調底層通訊協定與上層的 `router`。它支援多種協議的自動切換與同時運行，並提供優雅重啟 (Graceful Restart) 以達到零停機部署。

## 主要特色

- **多通訊協定支援**: 支援單純的 HTTP/1.1，也支援基於 `h2c` 的透明 HTTP/2，更支援基於 UDP QUIC 協定的 HTTP/3 服務。
- **Auto Protocol 模式**: 只要開啟 TLS 並選擇 Auto 模式（或是設定 `server.protocol: auto`），便會同時啟動 HTTP/1.1、HTTP/2 與 HTTP/3 的服務，自動與瀏覽器協商最佳協定。
- **Graceful Shutdown**: 收到 `SIGINT` 或 `SIGTERM` 訊號時，伺服器將拒絕新連線但確保已建立的請求處理完畢後才退出，並支援超時強行關閉。
- **Graceful Restart (僅支援 Unix)**: 透過發送自訂訊號（例如預設的 `SIGUSR2`），能夠觸發 Fork 新程序接替傾聽相同的 Port，然後舊程序在處理完手頭的請求後退出，達成零停機會。
- **自動 PID 管理**: 啟動時自動寫入 PID 檔案，方便使用腳本或監控系統追蹤程序。

## 基礎使用

利用 `config` 初始化並啟動伺服器：

```go
package main

import (
	"log"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/server"
)

func main() {
	// 1. 讀取配置
	cfg, _ := config.LoadConfig("config.yaml")

	// 2. 初始化 Logger
	logInstance, _ := logger.New("info", "stdout", nil, true)

	// 3. 建立伺服器實例
	srv := server.New(cfg, logInstance)

	// 4. 定義路由 (取得內建的 Router)
	r := srv.Router()
	r.GET("/api/ping", func(c *context.Context) {
		c.String(200, "pong")
	})

	// 5. 啟動伺服器 (會阻塞直到收到關閉訊號)
	if err := srv.Start(); err != nil {
		log.Fatalf("伺服器發生錯誤: %v", err)
	}
}
```

## 配置範例

在 `config.yaml` 控制 Protocol 與 TLS 行為：

```yaml
server:
  addr: ":443"
  protocol: "auto"        # 支援 http1, http2, http3, auto
  graceful_restart: true  # 監聽重啟訊號 (Unix only)
  tls:
    enabled: true         # 若是 http3 或 auto 這裡必須是 true
    cert_file: "/path/to/cert.pem"
    key_file: "/path/to/key.pem"
```

## 自訂路由與中間件結合

如上所述，`server.New()` 已經封裝並實例化了 `router.Router`。你可以透過 `srv.Router()` 直接呼叫所有 Router 支援的功能：

```go
r := srv.Router()

// 掛載全域中間件
r.Use(middleware.Logger(middleware.LoggerConfig{}))

// 掛載靜態檔案
r.Static("/public", "./assets")

// 處理 404
r.NotFound(func(c *context.Context) {
	c.String(404, "Page Not Found!")
})
```
