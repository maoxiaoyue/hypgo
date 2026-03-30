# HypGo

**專為 AI-人機協作設計的現代 Go Web 框架**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-0.7.0-orange.svg)](https://github.com/maoxiaoyue/hypgo/releases)

[English](README.md) | [繁體中文](README_zh-TW.md) | [简体中文](README_zh-CN.md)

## 什麼是 HypGo？

HypGo 是一個現代 Go Web 框架，原生支援 HTTP/1.1、HTTP/2、HTTP/3（QUIC），同時內建 **AI-人機協作開發** 工具鏈。框架不僅優化了人寫程式碼的效率，更讓 AI 能夠快速理解、正確生成、立刻驗證程式碼。

### 為什麼需要 HypGo？

傳統框架只解決「人寫程式碼」的問題。但在 AI 輔助開發的時代，框架還需要解決：

1. **可發現性** — AI 用最少的 token 理解專案全貌
2. **可預測性** — 嚴格慣例讓 AI 生成的程式碼位置和風格一致
3. **可驗證性** — 生成後立刻能驗對錯，不需人工逐行 review

HypGo 的每個功能都圍繞這三個原則設計。

## 核心功能

### 高效能網路層

| 功能                         | 說明                                                           |
| -------------------------- | ------------------------------------------------------------ |
| HTTP/1.1 + HTTP/2 + HTTP/3 | 三協議同時運行，自動 ALPN 協商與 Alt-Svc 升級                               |
| 0-RTT Session Cache        | TLS 1.3 快速恢復，帶 LRU + TTL + replay attack 防護                  |
| Radix Tree 路由              | O(k) 路徑查找 + LRU 快取 + 參數池化，零 GC 壓力                            |
| WebSocket 多協議              | JSON / Protobuf / FlatBuffers / MessagePack + AES-256-GCM 加密 |
| Graceful Shutdown          | HTTP/1+2 與 HTTP/3 並行 shutdown，atomic 競態防護                    |
| Graceful Restart           | Unix SIGUSR2 觸發，FD 傳遞 + poll 等待，零停機                          |

### AI 協作工具鏈

| 功能                      | 說明                                             |
| ----------------------- | ---------------------------------------------- |
| **Schema-first 路由**     | 路由攜帶 Input/Output 型別、描述、標籤，AI 直接理解 API 行為      |
| **Project Manifest**    | `hyp context` 一鍵產出 YAML/JSON 專案描述，AI 一次掌握全貌    |
| **Contract Testing**    | `contract.TestAll(t, router)` 一行驗證所有 schema 路由 |
| **Typed Error Catalog** | 預定義結構化錯誤碼（`E1001`），統一 handler 錯誤格式             |
| **Migration Diff**      | Model struct 變更後自動產生 up/down SQL migration     |
| **Annotation Protocol** | `// @ai:constraint` 結構化標註，AI 從註解理解業務約束         |
| **Change Impact**       | `hyp impact <file>` 分析修改影響的路由、測試、下游模組          |
| **AutoSync**            | Server 啟動時自動更新 `.hyp/context.yaml`，永遠與程式碼同步    |
| **Diagnostic Endpoint** | `GET /_debug/state` 一個請求取得完整系統快照               |

### 開發體驗

| 功能                   | 說明                                                                       |
| -------------------- | ------------------------------------------------------------------------ |
| Smart Scaffold       | `hyp gen` 生成整合 Schema + Error Catalog 的程式碼                               |
| Test Fixture Builder | `fixture.Request(router).POST("/api").WithJSON(body).Expect(201).Run(t)` |
| Hot Reload           | 結構化檔案監控 + debounce + 分類變更摘要                                              |
| BodyLimit            | 限制請求 body 大小，防止 DoS                                                      |
| MethodOverride       | 支援 `X-HTTP-Method-Override` 和 `_method` 參數                               |

### 資料庫

| 功能              | 說明                                                    |
| --------------- | ----------------------------------------------------- |
| Bun ORM         | MySQL / PostgreSQL / TiDB 讀寫分離（lock-free round-robin） |
| Redis / KeyDB   | 35 個高階方法（KV、Hash、List、Set、ZSet、Pub/Sub、Pipeline）      |
| Cassandra       | 插件系統動態載入                                              |
| ConnMaxLifetime | 統一 30 分鐘，防止長連線持有過期狀態                                  |

## 系統要求

- Go 1.24 或以上
- Docker（可選，用於容器化）

## 安裝

```bash
# 安裝框架
go get -u github.com/maoxiaoyue/hypgo

# 安裝 CLI 工具（確保 $GOPATH/bin 在 $PATH 中）
go install github.com/maoxiaoyue/hypgo/cmd/hyp@latest
```

## 快速開始

### 建立專案

```bash
# 全棧專案（含前端模板）
hyp new myapp && cd myapp && go mod tidy

# 純 API 專案
hyp api myapi && cd myapi && go mod tidy
```

### 最小可執行範例

```go
package main

import (
    "github.com/maoxiaoyue/hypgo/pkg/config"
    "github.com/maoxiaoyue/hypgo/pkg/context"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
    "github.com/maoxiaoyue/hypgo/pkg/schema"
    "github.com/maoxiaoyue/hypgo/pkg/server"
)

type CreateUserReq struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

type UserResp struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func main() {
    cfg := &config.Config{}
    cfg.ApplyDefaults()
    log := logger.NewLogger()

    srv := server.New(cfg, log)
    r := srv.Router()

    // Schema-first 路由 — AI 直接從 metadata 理解 API
    r.Schema(schema.Route{
        Method:  "POST",
        Path:    "/api/users",
        Summary: "建立使用者",
        Tags:    []string{"users"},
        Input:   CreateUserReq{},
        Output:  UserResp{},
    }).Handle(func(c *context.Context) {
        c.JSON(201, UserResp{ID: 1, Name: "test", Email: "test@test.com"})
    })

    // 傳統路由也可以
    r.GET("/health", func(c *context.Context) {
        c.JSON(200, map[string]string{"status": "ok"})
    })

    srv.Start()
}
```

### 配置範例

```yaml
# app/config/config.yaml
server:
  addr: ":8080"
  protocol: "auto"          # http1, http2, http3, auto
  tls:
    enabled: true
    cert_file: "cert.pem"
    key_file: "key.pem"

database:
  driver: postgres
  dsn: "postgres://user:pass@localhost:5432/mydb"
  max_idle_conns: 10
  max_open_conns: 100
  redis:
    addr: "localhost:6379"
    password: ""
    db: 0

logger:
  level: info
  output: stdout
  colors: true
```

## AI 協作開發方式

HypGo 的 AI 協作不是「AI 幫你補全程式碼」，而是完整的 **理解 → 生成 → 驗證** 迴圈：

### 1. AI 理解專案

```bash
# 生成專案 manifest（AI 讀取這個就夠了）
hyp context -o .hyp/manifest.yaml
```

```yaml
# .hyp/manifest.yaml — AI 的「地圖」
routes:
  - method: POST
    path: /api/users
    summary: "建立使用者"
    input_type: CreateUserReq
    output_type: UserResp
    handler_names: [main.createUser]
```

### 2. AI 生成程式碼

AI 根據 manifest + schema 生成 handler：

```go
// AI 知道 Input 是 CreateUserReq，Output 是 UserResp
// AI 知道要用 errors.Define 定義錯誤碼
// AI 知道要在 router.Schema() 註冊
```

### 3. 自動驗證

```go
// 一行測試所有 schema 路由
contract.TestAll(t, router)

// 或手動測試特定路由
contract.Test(t, router, contract.TestCase{
    Route:        "POST /api/users",
    Input:        `{"name":"test","email":"test@test.com"}`,
    ExpectStatus: 201,
    ExpectSchema: true,  // 自動驗證 response 符合 UserResp schema
})
```

### 4. 變更前影響分析

```bash
hyp impact pkg/errors/catalog.go
# → 3 packages depend on this, 43 tests affected, Risk: MEDIUM
```

### 5. 註解檢查

```bash
hyp chkcomment controllers/user.go
# → 2/4 blocks have comments (50%)
# → run with --fix to add suggestions
```

## CLI 命令

```bash
# 專案管理
hyp new <名稱>          # 建立全棧專案
hyp api <名稱>          # 建立 API 專案
hyp run                 # 啟動（熱重載）
hyp restart             # 零停機重啟

# AI 協作
hyp context             # 生成專案 manifest（YAML）
hyp context -f json     # JSON 格式
hyp chkcomment <file>   # 檢查註解完整性
hyp impact <file>       # 變更影響分析

# 程式碼生成
hyp generate controller <名稱>
hyp generate model <名稱>
hyp generate service <名稱>

# 資料庫
hyp migrate diff        # 比對 model 與快照，產生 SQL
hyp migrate snapshot    # 儲存當前 schema 快照

# 部署
hyp docker              # 建構 Docker 映像
hyp health              # 健康檢查
```

## Token 效率：為什麼 HypGo 能大幅降低 AI 開發成本

在 AI 輔助開發中，**token 就是成本**。每次 AI 需要理解你的專案，都要消耗大量 token 閱讀原始碼。HypGo 從架構層面解決這個問題：

### 傳統框架 vs HypGo

| 場景           | 傳統框架                                       | HypGo                               | Token 節省 |
| ------------ | ------------------------------------------ | ----------------------------------- | -------- |
| AI 理解 API 結構 | 讀所有 handler 檔案（~5,000 tokens）              | 讀 manifest.yaml（~500 tokens）        | **90%**  |
| AI 理解單一路由行為  | 讀 handler + service + model（~2,000 tokens） | 讀 schema metadata（~200 tokens）      | **90%**  |
| AI 驗證生成的程式碼  | 手動編寫測試（~1,500 tokens）                      | `contract.TestAll()` 一行（~50 tokens） | **97%**  |
| AI 評估修改影響    | 逐檔搜尋 import（~3,000 tokens）                 | `hyp impact`（~100 tokens）           | **97%**  |

### 原理

傳統框架要求 AI 讀取大量原始碼才能理解專案。HypGo 將關鍵資訊前置到 **結構化 metadata** 中：

```
傳統方式：AI 讀 handler → 推斷 Input/Output → 猜測約束 → 生成 → 手動測試
HypGo：  AI 讀 manifest → 已知 Input/Output → 已知約束 → 生成 → 自動驗證
```

一個 20 個路由的專案，每次 AI 互動可節省 **4,000-8,000 tokens**。累積下來，這是顯著的成本差異。

### 這不只是成本問題

Token 節省帶來的連鎖效益：

- **更快的回應** — AI 讀更少的程式碼，回應更快
- **更準確的生成** — 結構化資訊比自由文本更不容易被誤解
- **更長的上下文** — 節省的 token 可用於更複雜的對話和推理
- **更低的錯誤率** — 自動驗證取代人工 review，減少遺漏

## 安全與效能：人機協作的隱藏代價與 HypGo 的解法

讓 AI 存取專案資訊可以提升效率，但也帶來風險。HypGo 的設計原則是：**讓 AI 看得到結構，看不到秘密；讓系統跑得快，不因協作機制拖慢。**

### 安全：AI 能看到什麼、不能看到什麼

| 層級             | AI 可見                              | AI 不可見               | 機制                             |
| -------------- | ---------------------------------- | -------------------- | ------------------------------ |
| **Manifest**   | 路由路徑、方法、型別名稱                       | DSN、密碼、token、API key | AutoSync 自動過濾敏感欄位              |
| **Diagnostic** | 路由數量、連線狀態、記憶體用量                    | 資料庫內容、使用者資料          | DSN redact（`***@host:port/db`） |
| **Schema**     | Input/Output struct 欄位名與型別         | 實際資料值                | 只存型別 metadata，不存實例             |
| **Annotation** | `@ai:constraint`、`@ai:security` 標註 | 業務邏輯實作細節             | 純 AST 分析，不執行程式碼                |
| **Impact**     | import 依賴圖、測試數量                    | 檔案內容、變數值             | 只掃描 import 路徑，不讀函式內容           |

**設計原則**：AI 協作工具只暴露「結構」（路由、型別、依賴關係），從不暴露「資料」（密碼、token、使用者內容）。

### 安全：框架層級的防線

```
請求進入
  │
  ├─ BodyLimit ────────── 限制 payload 大小，防止大 body DoS
  ├─ RateLimiter ─────── Token bucket 限流 + 定期清理（防記憶體洩漏）
  ├─ Security Headers ── HSTS / XSS / nosniff / CSP / Referrer-Policy（預計算，零 GC）
  ├─ CORS ─────────────── Origin 白名單 + preflight 快取
  ├─ CSRF ─────────────── crypto/rand 生成 token，constant-time 驗證
  ├─ JWT ──────────────── 強制提供 Validator，未設定一律 401（安全預設）
  ├─ BasicAuth ────────── constant-time 密碼比對，防時序攻擊
  └─ CircuitBreaker ──── sync.Mutex 保護狀態，防止競態條件
```

每個安全中間件都經過以下考量：

- **安全預設** — JWT 未提供 Validator 一律拒絕，不會意外放行
- **密碼學安全** — CSRF token 和 Request ID 使用 `crypto/rand`，不用可預測的時間戳
- **競態安全** — Server shutdown 用 `atomic.Bool`，CircuitBreaker 用 `sync.Mutex`
- **資源防護** — RateLimiter 定期清理過期 key，SessionCache 帶 LRU + TTL 上限

### 效能：協作機制不拖慢系統

AI 協作功能在運行時的效能影響幾乎為零，因為大部分工作發生在 **啟動時** 或 **開發時**，而非每個請求的熱路徑上：

| 機制                  | 何時執行                                 | 熱路徑影響                       |
| ------------------- | ------------------------------------ | --------------------------- |
| Schema Registry     | 路由註冊時（啟動）                            | 零 — 只是記憶體中的 map 查找          |
| Manifest 生成         | `hyp context` 或 `Server.Start()`（啟動） | 零 — 不在請求路徑上                 |
| AutoSync            | Server 啟動時寫一次檔案                      | 零 — 原子寫入，不阻塞請求              |
| Contract Testing    | `go test` 時（開發）                      | 零 — 不進入生產環境                 |
| Impact Analysis     | `hyp impact` 時（開發）                   | 零 — CLI 工具，不進入生產環境          |
| Annotation Check    | `hyp chkcomment` 時（開發）               | 零 — CLI 工具，不進入生產環境          |
| Diagnostic Endpoint | 被請求時（按需）                             | 極低 — Rate limited（每分鐘 10 次） |

### 效能：請求熱路徑上的優化

框架核心的每個請求處理路徑都經過 GC 優化：

```
請求進入 → Radix Tree 查找（O(k)）
         → LRU 快取命中（O(1)，cacheItem pool 回收）
         → Context 從 sync.Pool 取出（零分配）
         → Params 從 pool 取出（零分配）
         → Security headers 預計算字串（零 Sprintf）
         → Replica 輪詢（atomic.Pointer，零鎖競爭）
         → 請求完成 → Context 歸還 pool
```

**結果**：AI 協作功能帶來的 metadata 儲存（Schema Registry、Manifest）只佔用啟動時的微量記憶體，不影響每請求延遲。你同時得到了 AI 友善的開發體驗，和與純手工框架相同的生產效能。

## 授權

HypGo 採用 [MIT 授權](LICENSE) 發布。

---

由台灣卯小月 用❤️製作
