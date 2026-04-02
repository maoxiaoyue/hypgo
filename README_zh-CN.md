# HypGo

**第一个让 AI 成为一等开发者的 Go Web 框架。**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-0.8.1--alpha-orange.svg)](https://github.com/maoxiaoyue/hypgo/releases)

[English](README.md) | [繁體中文](README_zh-TW.md) | [简体中文](README_zh-CN.md)

---

Gin 和 Echo 是为人写代码而生的。HypGo 是为你在 2026 年的工作方式而生 — **你和 AI，一起。**

```
传统框架：
  AI 读 20 个 handler 文件 → 猜测 Input/Output → 生成 → 你逐行 review

HypGo：
  AI 读 1 个 manifest 文件 → 已知 Input/Output → 生成 → 框架自动验证
```

### 数据说话

| AI 需要做的事 | 传统框架 | HypGo | 节省 |
|-------------|---------|-------|------|
| 理解 API 结构 | 读所有 handler（~5,000 tokens） | 读 manifest（~500 tokens） | **90%** |
| 理解单一路由 | 读 handler + service + model（~2,000 tokens） | 读 schema metadata（~200 tokens） | **90%** |
| 验证生成的代码 | 手动写测试（~1,500 tokens） | `contract.TestAll()` 一行（~50 tokens） | **97%** |
| 评估修改影响 | 逐文件搜索 import（~3,000 tokens） | `hyp impact`（~100 tokens） | **97%** |

一个 20 个路由的项目，**每次 AI 交互省下 4,000–8,000 tokens**。更快的响应、更准确的生成、更长的上下文窗口。

---

## HypGo 和其他框架的差异

### 1. Schema-first 路由 — AI 读 metadata，不读源代码

```go
// 传统：AI 必须读完整个 handler 才能理解
r.POST("/api/users", createUserHandler)

// HypGo：AI 读 6 行结构化 metadata
r.Schema(schema.Route{
    Method:  "POST",
    Path:    "/api/users",
    Summary: "Create user",
    Input:   CreateUserReq{},
    Output:  UserResp{},
}).Handle(createUserHandler)
```

### 2. Project Manifest — 一个文件取代 20 个

```bash
hyp context -o .hyp/manifest.yaml
```

```yaml
# AI 读这个，不需要翻你的整个 codebase
routes:
  - method: POST
    path: /api/users
    summary: "Create user"
    input_type: CreateUserReq
    output_type: UserResp
    handler_names: [controllers.CreateUser]
```

### 3. Contract Testing — AI 产出自动验证

```go
// 一行测试所有 schema 路由
contract.TestAll(t, router)
// → 自动从 Input struct 生成测试数据
// → 验证响应符合 Output struct
// → 检查状态码（POST→201、DELETE→204）
```

### 谁负责什么

```
你定义「什么」（Schema） → AI 实现「怎么做」（Handler） → 框架检查「对不对」（Contract）
```

| | 你 | AI | 框架 |
|---|-----|-----|------|
| **定义** | API 设计、业务规则 | — | — |
| **实现** | 审核逻辑 | 生成 boilerplate + CRUD | — |
| **验证** | — | — | 自动测试 Input/Output 合约 |

---

## 快速开始

```bash
# 安装
go install github.com/maoxiaoyue/hypgo/cmd/hyp@latest

# 创建项目
hyp api myservice && cd myservice && go mod tidy

# 一步生成完整资源（model + controller + router）
hyp generate model user
hyp generate controller user
```

生成的结构：

```
myservice/
├── app/
│   ├── controllers/user_controller.go   ← Handler 逻辑（AI 填这里）
│   ├── models/user.go                   ← Struct：User、CreateUserReq、UserResp
│   ├── routers/
│   │   ├── user.go                      ← Schema 路由（你定义这里）
│   │   ├── router.go                    ← Setup() 总入口
│   │   └── middleware.go                ← 中间件配置
│   └── services/
└── go.mod
```

### 最小可执行示例

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

### AI 协作工具链

| 功能 | 做什么 |
|------|--------|
| **Schema-first 路由** | 路由携带 Input/Output 类型 — AI 不读 handler 就能理解 API |
| **Project Manifest** | `hyp context` 用 ~500 tokens 输出整个项目结构 |
| **Contract Testing** | `contract.TestAll(t, router)` 一行验证所有路由 |
| **AI Rules** | `hyp ai-rules` 生成 Codex、Gemini、Cursor、Copilot、Windsurf 配置文件 |
| **Typed Error Catalog** | 预定义错误码（`E1001`）— AI 用它而非自行发明 |
| **Annotation Protocol** | `// @ai:constraint max=100` — AI 从注解读业务约束 |
| **Change Impact** | `hyp impact <file>` 修改前看影响范围 |
| **AutoSync** | Server 启动自动生成 `.hyp/context.yaml` — 永远同步 |
| **Migration Diff** | Model struct 变更自动生成 up/down SQL |
| **Diagnostic** | `GET /_debug/state` 一个请求取得系统快照 |

### 网络与性能

| 功能 | 说明 |
|------|------|
| HTTP/1.1 + HTTP/2 + HTTP/3 | 三协议同时运行，自动 ALPN + Alt-Svc |
| Radix Tree 路由 | O(k) 查找 + LRU 缓存 + 参数池化，零 GC 压力 |
| WebSocket | JSON / Protobuf / FlatBuffers / MessagePack + AES-256-GCM |
| Graceful Shutdown/Restart | 并行 shutdown、SIGUSR2 重启、零停机 |
| GC 优化 | 8 个 sync.Pool、map 重建、atomic.Pointer 无锁读 |

### 数据库

| 功能 | 说明 |
|------|------|
| Bun ORM | MySQL / PostgreSQL / TiDB lock-free 读写分离 |
| Redis / KeyDB | 35 个高阶方法（KV、Hash、List、Set、ZSet、Pub/Sub、Pipeline） |
| Cassandra | 插件系统 |

---

## 安全：AI 看得到结构，看不到秘密

| 层级 | AI 可见 | AI 不可见 | 机制 |
|------|--------|----------|------|
| **Manifest** | 路由路径、类型名称 | 密码、DSN、token | AutoSync 过滤敏感字段 |
| **Diagnostic** | 连接状态、内存 | 数据库内容、用户数据 | DSN redact（`***@host:port/db`） |
| **Schema** | 字段名与类型 | 实际数据值 | 只存类型 metadata |
| **Impact** | import 依赖图、测试数 | 文件内容 | 只扫描 import 路径 |

安全默认：JWT 未提供 Validator 一律拒绝、CSRF 用 `crypto/rand`、CircuitBreaker 用 `sync.Mutex`、RateLimiter 自动清理过期 key。

---

## 性能：AI 功能零成本

AI 协作在**启动时或开发时**运行，不进入请求热路径：

```
请求 → Radix Tree O(k) → LRU cache O(1)
    → Context 从 sync.Pool 取（零分配） → Params 从 pool 取（零分配）
    → Security headers 预计算（零 Sprintf）
    → Replica 轮询 atomic.Pointer（零锁竞争）
    → 响应 → Context 归还 pool
```

Schema Registry 和 Manifest 只占启动内存。每请求延迟与无 AI 功能的框架完全相同。

---

## CLI 命令

```bash
# 项目
hyp new <名称>              # 全栈项目
hyp api <名称>              # API 项目

# AI 协作
hyp context                 # 生成 manifest（YAML）
hyp ai-rules                # 生成 AI 工具配置文件
hyp chkcomment <file>       # 注解检查
hyp impact <file>           # 变更影响分析

# 代码生成
hyp generate controller <name>   # Controller + Router + Middleware
hyp generate model <name>       # Model + Request/Response struct
hyp generate service <name>     # Service + Error Catalog

# 数据库
hyp migrate diff            # 从 model 变更生成 SQL
hyp migrate snapshot        # 保存 schema 基线

# 部署
hyp docker                  # 构建 Docker 镜像
hyp health                  # 健康检查
```

---

## 授权

MIT 授权。由台湾卯小月制作。
