# HypGo

**第一個讓 AI 成為一等開發者的 Go Web 框架。**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-0.8.1--alpha-orange.svg)](https://github.com/maoxiaoyue/hypgo/releases)

[English](README.md) | [繁體中文](README_zh-TW.md) | [简体中文](README_zh-CN.md)

---

Gin 和 Echo 是為人寫程式碼而生的。HypGo 是為你在 2026 年的工作方式而生 — **你和 AI，一起。**

```
傳統框架：
  AI 讀 20 個 handler 檔案 → 猜測 Input/Output → 生成 → 你逐行 review

HypGo：
  AI 讀 1 個 manifest 檔案 → 已知 Input/Output → 生成 → 框架自動驗證
```

### 數據說話

| AI 需要做的事 | 傳統框架 | HypGo | 節省 |
|-------------|---------|-------|------|
| 理解 API 結構 | 讀所有 handler（~5,000 tokens） | 讀 manifest（~500 tokens） | **90%** |
| 理解單一路由 | 讀 handler + service + model（~2,000 tokens） | 讀 schema metadata（~200 tokens） | **90%** |
| 驗證生成的程式碼 | 手動寫測試（~1,500 tokens） | `contract.TestAll()` 一行（~50 tokens） | **97%** |
| 評估修改影響 | 逐檔搜尋 import（~3,000 tokens） | `hyp impact`（~100 tokens） | **97%** |

一個 20 個路由的專案，**每次 AI 互動省下 4,000–8,000 tokens**。更快的回應、更準確的生成、更長的上下文窗口。

---

## HypGo 和其他框架的差異

### 1. Schema-first 路由 — AI 讀 metadata，不讀原始碼

```go
// 傳統：AI 必須讀完整個 handler 才能理解
r.POST("/api/users", createUserHandler)

// HypGo：AI 讀 6 行結構化 metadata
r.Schema(schema.Route{
    Method:  "POST",
    Path:    "/api/users",
    Summary: "Create user",
    Input:   CreateUserReq{},
    Output:  UserResp{},
}).Handle(createUserHandler)
```

### 2. Project Manifest — 一個檔案取代 20 個

```bash
hyp context -o .hyp/manifest.yaml
```

```yaml
# AI 讀這個，不需要翻你的整個 codebase
routes:
  - method: POST
    path: /api/users
    summary: "Create user"
    input_type: CreateUserReq
    output_type: UserResp
    handler_names: [controllers.CreateUser]
```

### 3. Contract Testing — AI 產出自動驗證

```go
// 一行測試所有 schema 路由
contract.TestAll(t, router)
// → 自動從 Input struct 生成測試資料
// → 驗證回應符合 Output struct
// → 檢查狀態碼（POST→201、DELETE→204）
```

### 誰負責什麼

```
你定義「什麼」（Schema） → AI 實作「怎麼做」（Handler） → 框架檢查「對不對」（Contract）
```

| | 你 | AI | 框架 |
|---|-----|-----|------|
| **定義** | API 設計、業務規則 | — | — |
| **實作** | 審核邏輯 | 生成 boilerplate + CRUD | — |
| **驗證** | — | — | 自動測試 Input/Output 合約 |

---

## 快速開始

```bash
# 安裝
go install github.com/maoxiaoyue/hypgo/cmd/hyp@latest

# 建立專案
hyp api myservice && cd myservice && go mod tidy

# 一步生成完整資源（model + controller + router）
hyp generate model user
hyp generate controller user

# ⚡ 必要步驟：生成 AI 協作檔案
hyp ai-rules
```

> **`hyp ai-rules` 不是可選的。** 它生成 `AGENTS.md`、`GEMINI.md`、`.github/copilot-instructions.md`、`.cursor/rules/hypgo.mdc`、`.windsurf/rules/hypgo.md` — 這些檔案告訴 AI 工具你的專案慣例。沒有它們，AI 每次開啟專案都從零開始，浪費數千 tokens 重新學習你的 codebase。**建立專案後跑一次，新增路由後再跑一次。**

生成的結構：

```
myservice/
├── app/
│   ├── controllers/user_controller.go   ← Handler 邏輯（AI 填這裡）
│   ├── models/user.go                   ← Struct：User、CreateUserReq、UserResp
│   ├── routers/
│   │   ├── user.go                      ← Schema 路由（你定義這裡）
│   │   ├── router.go                    ← Setup() 總入口
│   │   └── middleware.go                ← 中間件配置
│   └── services/
├── AGENTS.md                            ← Codex, Cursor, Aider, OpenHands
├── GEMINI.md                            ← Google Gemini CLI / AI Studio
├── .github/copilot-instructions.md      ← GitHub Copilot
├── .cursor/rules/hypgo.mdc              ← Cursor
├── .windsurf/rules/hypgo.md             ← Windsurf
└── go.mod
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

    r.Schema(schema.Route{
        Method:  "POST",
        Path:    "/api/users",
        Summary: "Create user",
        Input:   CreateUserReq{},
        Output:  UserResp{},
    }).Handle(func(c *context.Context) {
        c.JSON(201, UserResp{ID: 1, Name: "test", Email: "test@test.com"})
    })

    srv.Start()
}
```

---

## 完整功能

### AI 協作工具鏈

| 功能 | 做什麼 |
|------|--------|
| **Schema-first 路由** | 路由攜帶 Input/Output 型別 — AI 不讀 handler 就能理解 API |
| **Project Manifest** | `hyp context` 用 ~500 tokens 輸出整個專案結構 |
| **Contract Testing** | `contract.TestAll(t, router)` 一行驗證所有路由 |
| **AI Rules** | `hyp ai-rules` 生成 Codex、Gemini、Cursor、Copilot、Windsurf 配置檔 |
| **Typed Error Catalog** | 預定義錯誤碼（`E1001`）— AI 用它而非自行發明 |
| **Annotation Protocol** | `// @ai:constraint max=100` — AI 從註解讀業務約束 |
| **Change Impact** | `hyp impact <file>` 修改前看影響範圍 |
| **AutoSync** | Server 啟動自動生成 `.hyp/context.yaml` — 永遠同步 |
| **Migration Diff** | Model struct 變更自動生成 up/down SQL |
| **Diagnostic** | `GET /_debug/state` 一個請求取得系統快照 |

### 網路與效能

| 功能 | 說明 |
|------|------|
| HTTP/1.1 + HTTP/2 + HTTP/3 | 三協議同時運行，自動 ALPN + Alt-Svc |
| Radix Tree 路由 | O(k) 查找 + LRU 快取 + 參數池化，零 GC 壓力 |
| WebSocket | JSON / Protobuf / FlatBuffers / MessagePack + AES-256-GCM |
| Graceful Shutdown/Restart | 並行 shutdown、SIGUSR2 重啟、零停機 |
| GC 優化 | 8 個 sync.Pool、map 重建、atomic.Pointer 無鎖讀 |

### 資料庫

| 功能 | 說明 |
|------|------|
| Bun ORM | MySQL / PostgreSQL / TiDB lock-free 讀寫分離 |
| Redis / KeyDB | 35 個高階方法（KV、Hash、List、Set、ZSet、Pub/Sub、Pipeline） |
| Cassandra | 插件系統 |

---

## 安全：AI 看得到結構，看不到秘密

| 層級 | AI 可見 | AI 不可見 | 機制 |
|------|--------|----------|------|
| **Manifest** | 路由路徑、型別名稱 | 密碼、DSN、token | AutoSync 過濾敏感欄位 |
| **Diagnostic** | 連線狀態、記憶體 | 資料庫內容、使用者資料 | DSN redact（`***@host:port/db`） |
| **Schema** | 欄位名與型別 | 實際資料值 | 只存型別 metadata |
| **Impact** | import 依賴圖、測試數 | 檔案內容 | 只掃描 import 路徑 |

安全預設：JWT 未提供 Validator 一律拒絕、CSRF 用 `crypto/rand`、CircuitBreaker 用 `sync.Mutex`、RateLimiter 自動清理過期 key。

---

## 效能：AI 功能零成本

AI 協作在**啟動時或開發時**運行，不進入請求熱路徑：

```
請求 → Radix Tree O(k) → LRU cache O(1)
    → Context 從 sync.Pool 取（零分配） → Params 從 pool 取（零分配）
    → Security headers 預計算（零 Sprintf）
    → Replica 輪詢 atomic.Pointer（零鎖競爭）
    → 回應 → Context 歸還 pool
```

Schema Registry 和 Manifest 只佔啟動記憶體。每請求延遲與無 AI 功能的框架完全相同。

---

## CLI 命令

```bash
# 專案
hyp new <名稱>              # 全棧專案
hyp api <名稱>              # API 專案

# AI 協作
hyp context                 # 生成 manifest（YAML）
hyp ai-rules                # 生成 AI 工具配置檔
hyp chkcomment <file>       # 註解檢查
hyp impact <file>           # 變更影響分析

# 程式碼生成
hyp generate controller <name>   # Controller + Router + Middleware
hyp generate model <name>       # Model + Request/Response struct
hyp generate service <name>     # Service + Error Catalog

# 資料庫
hyp migrate diff            # 從 model 變更生成 SQL
hyp migrate snapshot        # 儲存 schema 基線

# 部署
hyp docker                  # 建構 Docker 映像
hyp health                  # 健康檢查
```

---

## 授權

MIT 授權。由台灣卯小月製作。
