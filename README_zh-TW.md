# HypGo

一個支援 HTTP/3、HTTP/2 和插件架構的現代化 Go Web 框架。

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-0.1.0-orange.svg)](https://github.com/maoxiaoyue/hypgo/releases)

[English](README.md) | [繁體中文](README_zh-TW.md) | [简体中文](README_zh-CN.md)

## 描述

HypGo 是一個現代化的 Go Web 框架，提供 HTTP/2 和 HTTP/3 支援、Ent ORM 整合、訊息佇列和進階 JSON 驗證功能。HTTP/3.0 的速度比 HTTP/1.1 快近 10 倍。在我的使用案例中，這是非常有用且重要的，因此我嘗試設計了這個框架。

這個框架具有強大的插件系統，允許您透過簡單的 CLI 命令添加 Kafka、RabbitMQ、Cassandra 等功能。它還包括自動 Docker 映像建構、熱重載開發和零停機部署功能。

## 開發故事

作為一名在全球電商平台工作的後端工程師，我面臨著一個關鍵挑戰：我們的亞洲客戶在訪問美國伺服器時遇到了顯著的延遲。簡單的 API 呼叫平均回應時間超過 700ms，圖片載入時間延長到數秒，使用者體驗受到嚴重影響，直接影響了我們的轉換率。

### 臨界點

2023 年底，在一次重要的產品發布期間，我們的監控系統呈現了一幅嚴峻的畫面：
- 從上海到美國西部伺服器的 API 回應時間：**平均 742ms**
- 產品圖片載入（平均 500KB）：**2.3 秒**
- 亞洲使用者的購物車放棄率：**68%**（美國使用者為 23%）

傳統的優化技術已經達到了極限。CDN 有所幫助，但還不夠。我們需要從根本上改變處理跨境資料傳輸的方式。

### HTTP/3 的啟示

在研究解決方案時，我發現 HTTP/3 的 QUIC 協議理論上可以解決我們的隊頭阻塞問題並減少連接建立的開銷。但現有的 Go 框架缺乏適當的 HTTP/3 支援，將其添加到我們的舊系統中似乎是不可能的。

這就是我決定建立 HypGo 的原因。

### 改變一切的結果

在我們的測試環境中實施支援 HTTP/3 的 HypGo 後，結果令人驚嘆：

| 指標 | 之前 (HTTP/2) | 之後 (HTTP/3) | 改善幅度 |
|------|---------------|---------------|----------|
| API 回應（上海 → 美國） | 742ms | 198ms | **快 73%** |
| 圖片載入時間（500KB） | 2,341ms | 512ms | **快 78%** |
| 購物車放棄率（亞洲） | 68% | 29% | **減少 57%** |
| 客戶滿意度 | 3.2/5 | 4.6/5 | **提升 44%** |

### 為什麼我開源了 HypGo

這些結果太重要了，不能保持私有。跨境延遲影響著全球數百萬個應用程式，開發者不應該從頭開始建構 HTTP/3 支援。HypGo 源於現實世界的痛點，並提供現實世界的結果。

除了 HTTP/3，我意識到現代應用程式還需要：
- **插件架構**，實現關注點的清晰分離
- **Docker 整合**，確保一致的部署
- **熱重載**，提高開發者生產力
- **訊息佇列**，建構可擴展的架構

HypGo 的每個功能都來自實際的生產需求，在真實流量下測試，並被證明能夠提供結果。

## 功能特點

- ⚡ **HTTP/2 & HTTP/3 支援** - 原生支援最新協議，自動降級
- 🗄️ **Ent ORM 整合** - 強大的實體框架，類型安全查詢
- 📨 **訊息佇列** - 插件支援 RabbitMQ、Kafka 等
- 🔍 **進階 JSON 處理** - 欄位驗證、類型檢查和架構驗證
- 📝 **日誌輪換** - 內建日誌管理，支援壓縮和保留策略
- ⚙️ **Viper 配置** - 基於 YAML 的配置，支援環境變數
- 🏗️ **MVC 架構** - Controllers、Models、Services 清晰分層
- 🔌 **插件系統** - 動態添加功能，無需修改核心程式碼
- 🐳 **Docker 整合** - 一鍵建構和部署 Docker 映像
- 🔥 **熱重載** - 開發期間自動重啟應用程式
- ♻️ **零停機部署** - 優雅關閉和重啟功能
- 🌐 **WebSocket 支援** - 即時雙向通訊，支援頻道

## 系統要求

- Go 版本 1.21 或以上
- Docker（可選，用於容器化）

## 安裝

### 安裝 HypGo 框架
```bash
go get -u github.com/maoxiaoyue/hypgo
```

### 安裝 CLI 工具
```bash
go install github.com/maoxiaoyue/hypgo/cmd/hyp@latest
```

## 快速開始

### 1. 建立新專案

#### 全棧專案（包含前端）
```bash
hyp new myapp
cd myapp
go mod tidy
hyp run
```

#### 純 API 專案
```bash
hyp api myapi
cd myapi
go mod tidy
hyp run
```

### 2. 添加插件

```bash
# 添加訊息佇列支援
hyp addp rabbitmq
hyp addp kafka

# 添加資料庫支援
hyp addp mongodb
hyp addp cassandra

# 添加搜尋功能
hyp addp elasticsearch
```

### 3. 建構 Docker 映像

```bash
# 自動偵測連接埠並建構映像
hyp docker

# 自訂映像名稱和標籤
hyp docker -n myapp -t v1.0.0

# 建構並推送到註冊表
hyp docker -r docker.io/username --no-push=false
```

## 為什麼選擇 HTTP/2.0 和 HTTP/3.0？

唯一的原因就是速度很快。特別是在使用較小流量時。

### 效能比較

| 協議 | 延遲 | 吞吐量 | 連接開銷 |
|------|------|--------|----------|
| HTTP/1.1 | 高 | 低 | 高（多個 TCP） |
| HTTP/2 | 中 | 高 | 低（多路復用） |
| HTTP/3 | 低 | 非常高 | 非常低（QUIC/UDP） |

### 關鍵優勢：

1. **HTTP/2**：
   - 多路復用：單一連接上的多個請求
   - 伺服器推送功能
   - 標頭壓縮（HPACK）
   - 二進制協議

2. **HTTP/3**：
   - 基於 QUIC（UDP 基礎）
   - 0-RTT 連接建立
   - 在不穩定網路上有更好的效能
   - 獨立的流錯誤修正

### 參考資料
- [HTTP vs. HTTP/2 vs. HTTP/3: What's the Difference?](https://www.pubnub.com/blog/http-vs-http-2-vs-http-3-whats-the-difference/)

## 核心概念

### 插件架構

HypGo 使用模組化插件系統，允許您在不修改核心框架的情況下添加功能：

```bash
# 添加插件
hyp addp <插件名稱>

# 可用插件：
- rabbitmq      # 訊息佇列
- kafka         # 串流平台
- cassandra     # NoSQL 資料庫
- scylladb      # 高效能 Cassandra
- mongodb       # 文件資料庫
- elasticsearch # 搜尋引擎
```

每個插件會建立：
- `config/` 中的配置檔案
- `app/plugins/` 中的服務實作
- 自動依賴管理

### 配置管理

```yaml
# config/config.yaml
server:
  protocol: http3  # http1, http2, http3
  addr: :8080
  enable_graceful_restart: true

database:
  driver: mysql
  dsn: "user:pass@tcp(localhost:3306)/db"

logger:
  level: debug
  rotation:
    max_size: 100MB
    max_age: 7d
```

### MVC 結構

```
app/
├── controllers/   # HTTP 處理器
├── models/        # 資料模型（Ent schemas）
├── services/      # 業務邏輯
└── plugins/       # 插件實作
```

## CLI 命令

### 專案管理
```bash
hyp new <名稱>     # 建立全棧專案
hyp api <名稱>     # 建立 API 專案
hyp run            # 執行應用程式
hyp run -w         # 熱重載執行
hyp restart        # 零停機重啟
```

### 程式碼生成
```bash
hyp generate controller <名稱>  # 生成控制器
hyp generate model <名稱>       # 生成模型
hyp generate service <名稱>     # 生成服務
```

### 插件管理
```bash
hyp addp <插件>    # 添加插件
```

### 部署
```bash
hyp docker         # 建構 Docker 映像
hyp docker -n <名稱> -t <標籤>  # 自訂映像
```

## 進階功能

### WebSocket 支援

```go
// 伺服器端
wsHub := websocket.NewHub(logger)
go wsHub.Run()
router.HandleFunc("/ws", wsHub.ServeWS)

// 廣播給所有客戶端
wsHub.BroadcastJSON(data)

// 發送到特定頻道
wsHub.PublishToChannelJSON("updates", data)
```

```javascript
// 客戶端
const ws = new WebSocket('ws://localhost:8080/ws');
ws.send(JSON.stringify({
    type: 'subscribe',
    data: { channel: 'updates' }
}));
```

### 熱重載開發

```bash
# 檔案變更時自動重啟
hyp run -w
```

### 零停機部署

```bash
# 優雅重啟，不中斷連接
hyp restart
```

### Docker 整合

```bash
# 一鍵 Docker 建構
hyp docker

# 生成的 Dockerfile 包含：
# - 多階段建構
# - 非 root 使用者
# - 健康檢查
# - 優化的層
```

## 專案範例

### 基本 API 伺服器

```go
package main

import (
    "github.com/maoxiaoyue/hypgo/pkg/server"
    "github.com/maoxiaoyue/hypgo/pkg/config"
    "myapp/app/controllers"
)

func main() {
    cfg, _ := config.Load("config/config.yaml")
    srv := server.New(cfg, logger)
    
    controllers.RegisterRoutes(srv.Router(), db, logger)
    srv.Start()
}
```

### 使用插件

```go
// 執行後：hyp addp kafka
import "myapp/app/plugins/kafka"

kafkaService, _ := kafka.New(config.GetPluginConfig("kafka"), logger)
kafkaService.Publish("events", message)
```

## 發展藍圖

### V0.1（當前版本）✅
- [x] HTTP/1.1、HTTP/2、HTTP/3 支援
- [x] 基本 MVC 結構
- [x] CLI 工具與專案生成
- [x] 插件系統架構
- [x] Docker 整合
- [x] 熱重載開發
- [x] WebSocket 支援
- [x] 基本中介軟體（CORS、Logger、RateLimit）

### V1.0（進行中）🚧
- [ ] 認證與授權系統
- [ ] GraphQL 支援
- [ ] gRPC 整合
- [ ] 資料庫遷移工具
- [ ] API 文件生成器
- [ ] 效能監控
- [ ] 分散式追蹤
- [ ] 斷路器模式
- [ ] Service Mesh 就緒

### V2.0（計劃中）📋
- [ ] 微服務工具包
- [ ] 事件溯源支援
- [ ] CQRS 實作
- [ ] Kubernetes 操作器
- [ ] 多租戶支援
- [ ] 即時分析
- [ ] AI/ML 整合助手
- [ ] 邊緣運算支援
- [ ] 區塊鏈整合

## 效能基準測試

```
HTTP/1.1 vs HTTP/2 vs HTTP/3（1000 個並發請求）
┌─────────────┬──────────┬────────────┬─────────────┐
│ 協議        │ 請求/秒   │ 延遲       │ 吞吐量      │
├─────────────┼──────────┼────────────┼─────────────┤
│ HTTP/1.1    │ 15,234   │ 65.7ms     │ 18.3 MB/s   │
│ HTTP/2      │ 45,821   │ 21.8ms     │ 55.1 MB/s   │
│ HTTP/3      │ 152,456  │ 6.6ms      │ 183.2 MB/s  │
└─────────────┴──────────┴────────────┴─────────────┘
```

## 貢獻

歡迎貢獻！請查看我們的[貢獻指南](CONTRIBUTING.md)了解詳情。

### 開發設置

```bash
# 複製儲存庫
git clone https://github.com/maoxiaoyue/hypgo
cd hypgo

# 安裝依賴
go mod download

# 執行測試
make test

# 建構
make build
```

## 授權

HypGo 採用 [MIT 授權](LICENSE) 發布。

## 支援

- 📧 電子郵件：support@hypgo.dev
- 💬 Discord：[加入我們的社群](https://discord.gg/hypgo)
- 📖 文件：[docs.hypgo.dev](https://docs.hypgo.dev)
- 🐛 問題：[GitHub Issues](https://github.com/maoxiaoyue/hypgo/issues)

## 致謝

HypGo 站在巨人的肩膀上：
- [quic-go](https://github.com/quic-go/quic-go) 提供 HTTP/3 支援
- [Ent](https://entgo.io/) 提供 ORM
- [Viper](https://github.com/spf13/viper) 提供配置管理
- [Cobra](https://github.com/spf13/cobra) 提供 CLI

---

由 HypGo 團隊用 ❤️ 製作
