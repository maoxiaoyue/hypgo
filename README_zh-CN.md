# HypGo

**专为 AI-人机协作设计的现代 Go Web 框架**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-0.7.0-orange.svg)](https://github.com/maoxiaoyue/hypgo/releases)

[English](README.md) | [繁體中文](README_zh-TW.md) | [简体中文](README_zh-CN.md)

## 什么是 HypGo？

HypGo 是一个现代 Go Web 框架，原生支持 HTTP/1.1、HTTP/2、HTTP/3（QUIC），同时内建 **AI-人机协作开发** 工具链。框架不仅优化了人写代码的效率，更让 AI 能够快速理解、正确生成、立刻验证代码。

### 为什么需要 HypGo？

传统框架只解决「人写代码」的问题。但在 AI 辅助开发的时代，框架还需要解决：

1. **可发现性** — AI 用最少的 token 理解项目全貌
2. **可预测性** — 严格惯例让 AI 生成的代码位置和风格一致
3. **可验证性** — 生成后立刻能验对错，不需人工逐行 review

HypGo 的每个功能都围绕这三个原则设计。

## 核心功能

### 高性能网络层

| 功能 | 说明 |
|------|------|
| HTTP/1.1 + HTTP/2 + HTTP/3 | 三协议同时运行，自动 ALPN 协商与 Alt-Svc 升级 |
| 0-RTT Session Cache | TLS 1.3 快速恢复，带 LRU + TTL + replay attack 防护 |
| Radix Tree 路由 | O(k) 路径查找 + LRU 缓存 + 参数池化，零 GC 压力 |
| WebSocket 多协议 | JSON / Protobuf / FlatBuffers / MessagePack + AES-256-GCM 加密 |
| Graceful Shutdown | HTTP/1+2 与 HTTP/3 并行 shutdown，atomic 竞态防护 |
| Graceful Restart | Unix SIGUSR2 触发，FD 传递 + poll 等待，零停机 |

### AI 协作工具链

| 功能 | 说明 |
|------|------|
| **Schema-first 路由** | 路由携带 Input/Output 类型、描述、标签，AI 直接理解 API 行为 |
| **Project Manifest** | `hyp context` 一键产出 YAML/JSON 项目描述，AI 一次掌握全貌 |
| **Contract Testing** | `contract.TestAll(t, router)` 一行验证所有 schema 路由 |
| **Typed Error Catalog** | 预定义结构化错误码（`E1001`），统一 handler 错误格式 |
| **Migration Diff** | Model struct 变更后自动产生 up/down SQL migration |
| **Annotation Protocol** | `// @ai:constraint` 结构化标注，AI 从注解理解业务约束 |
| **Change Impact** | `hyp impact <file>` 分析修改影响的路由、测试、下游模块 |
| **AutoSync** | Server 启动时自动更新 `.hyp/context.yaml`，永远与代码同步 |
| **Diagnostic Endpoint** | `GET /_debug/state` 一个请求取得完整系统快照 |

### 开发体验

| 功能 | 说明 |
|------|------|
| Smart Scaffold | `hyp gen` 生成整合 Schema + Error Catalog 的代码 |
| Test Fixture Builder | `fixture.Request(router).POST("/api").WithJSON(body).Expect(201).Run(t)` |
| Hot Reload | 结构化文件监控 + debounce + 分类变更摘要 |
| BodyLimit | 限制请求 body 大小，防止 DoS |
| MethodOverride | 支持 `X-HTTP-Method-Override` 和 `_method` 参数 |

### 数据库

| 功能 | 说明 |
|------|------|
| Bun ORM | MySQL / PostgreSQL / TiDB 读写分离（lock-free round-robin） |
| Redis / KeyDB | 35 个高阶方法（KV、Hash、List、Set、ZSet、Pub/Sub、Pipeline） |
| Cassandra | 插件系统动态加载 |
| ConnMaxLifetime | 统一 30 分钟，防止长连接持有过期状态 |

## 系统要求

- Go 1.24 或以上
- Docker（可选，用于容器化）

## 安装

```bash
# 安装框架
go get -u github.com/maoxiaoyue/hypgo

# 安装 CLI 工具（确保 $GOPATH/bin 在 $PATH 中）
go install github.com/maoxiaoyue/hypgo/cmd/hyp@latest
```

## 快速开始

### 创建项目

```bash
# 全栈项目（含前端模板）
hyp new myapp && cd myapp && go mod tidy && hyp run

# 纯 API 项目
hyp api myapi && cd myapi && go mod tidy && hyp run
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

    // Schema-first 路由 — AI 直接从 metadata 理解 API
    r.Schema(schema.Route{
        Method:  "POST",
        Path:    "/api/users",
        Summary: "创建用户",
        Tags:    []string{"users"},
        Input:   CreateUserReq{},
        Output:  UserResp{},
    }).Handle(func(c *context.Context) {
        c.JSON(201, UserResp{ID: 1, Name: "test", Email: "test@test.com"})
    })

    // 传统路由也可以
    r.GET("/health", func(c *context.Context) {
        c.JSON(200, map[string]string{"status": "ok"})
    })

    srv.Start()
}
```

### 配置示例

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

## AI 协作开发方式

HypGo 的 AI 协作不是「AI 帮你补全代码」，而是完整的 **理解 → 生成 → 验证** 循环：

### 1. AI 理解项目

```bash
# 生成项目 manifest（AI 读取这个就够了）
hyp context -o .hyp/manifest.yaml
```

```yaml
# .hyp/manifest.yaml — AI 的「地图」
routes:
  - method: POST
    path: /api/users
    summary: "创建用户"
    input_type: CreateUserReq
    output_type: UserResp
    handler_names: [main.createUser]
```

### 2. AI 生成代码

AI 根据 manifest + schema 生成 handler：

```go
// AI 知道 Input 是 CreateUserReq，Output 是 UserResp
// AI 知道要用 errors.Define 定义错误码
// AI 知道要在 router.Schema() 注册
```

### 3. 自动验证

```go
// 一行测试所有 schema 路由
contract.TestAll(t, router)

// 或手动测试特定路由
contract.Test(t, router, contract.TestCase{
    Route:        "POST /api/users",
    Input:        `{"name":"test","email":"test@test.com"}`,
    ExpectStatus: 201,
    ExpectSchema: true,  // 自动验证 response 符合 UserResp schema
})
```

### 4. 变更前影响分析

```bash
hyp impact pkg/errors/catalog.go
# → 3 packages depend on this, 43 tests affected, Risk: MEDIUM
```

### 5. 注解检查

```bash
hyp chkcomment controllers/user.go
# → 2/4 blocks have comments (50%)
# → run with --fix to add suggestions
```

## CLI 命令

```bash
# 项目管理
hyp new <名称>          # 创建全栈项目
hyp api <名称>          # 创建 API 项目
hyp run                 # 启动（热重载）
hyp restart             # 零停机重启

# AI 协作
hyp context             # 生成项目 manifest（YAML）
hyp context -f json     # JSON 格式
hyp chkcomment <file>   # 检查注解完整性
hyp impact <file>       # 变更影响分析

# 代码生成
hyp generate controller <名称>
hyp generate model <名称>
hyp generate service <名称>

# 数据库
hyp migrate diff        # 比对 model 与快照，产生 SQL
hyp migrate snapshot    # 保存当前 schema 快照

# 部署
hyp docker              # 构建 Docker 镜像
hyp health              # 健康检查
```

## Token 效率：为什么 HypGo 能大幅降低 AI 开发成本

在 AI 辅助开发中，**token 就是成本**。每次 AI 需要理解你的项目，都要消耗大量 token 阅读源代码。HypGo 从架构层面解决这个问题：

### 传统框架 vs HypGo

| 场景 | 传统框架 | HypGo | Token 节省 |
|------|---------|-------|-----------|
| AI 理解 API 结构 | 读所有 handler 文件（~5,000 tokens） | 读 manifest.yaml（~500 tokens） | **90%** |
| AI 理解单一路由行为 | 读 handler + service + model（~2,000 tokens） | 读 schema metadata（~200 tokens） | **90%** |
| AI 验证生成的代码 | 手动编写测试（~1,500 tokens） | `contract.TestAll()` 一行（~50 tokens） | **97%** |
| AI 评估修改影响 | 逐文件搜索 import（~3,000 tokens） | `hyp impact`（~100 tokens） | **97%** |

### 原理

传统框架要求 AI 读取大量源代码才能理解项目。HypGo 将关键信息前置到 **结构化 metadata** 中：

```
传统方式：AI 读 handler → 推断 Input/Output → 猜测约束 → 生成 → 手动测试
HypGo：  AI 读 manifest → 已知 Input/Output → 已知约束 → 生成 → 自动验证
```

一个 20 个路由的项目，每次 AI 交互可节省 **4,000-8,000 tokens**。累积下来，这是显著的成本差异。

### 这不只是成本问题

Token 节省带来的连锁效益：

- **更快的响应** — AI 读更少的代码，响应更快
- **更准确的生成** — 结构化信息比自由文本更不容易被误解
- **更长的上下文** — 节省的 token 可用于更复杂的对话和推理
- **更低的错误率** — 自动验证取代人工 review，减少遗漏

## 安全与性能：人机协作的隐藏代价与 HypGo 的解法

让 AI 存取项目信息可以提升效率，但也带来风险。HypGo 的设计原则是：**让 AI 看得到结构，看不到秘密；让系统跑得快，不因协作机制拖慢。**

### 安全：AI 能看到什么、不能看到什么

| 层级 | AI 可见 | AI 不可见 | 机制 |
|------|--------|----------|------|
| **Manifest** | 路由路径、方法、类型名称 | DSN、密码、token、API key | AutoSync 自动过滤敏感字段 |
| **Diagnostic** | 路由数量、连接状态、内存用量 | 数据库内容、用户数据 | DSN redact（`***@host:port/db`） |
| **Schema** | Input/Output struct 字段名与类型 | 实际数据值 | 只存类型 metadata，不存实例 |
| **Annotation** | `@ai:constraint`、`@ai:security` 标注 | 业务逻辑实现细节 | 纯 AST 分析，不执行代码 |
| **Impact** | import 依赖图、测试数量 | 文件内容、变量值 | 只扫描 import 路径，不读函数内容 |

**设计原则**：AI 协作工具只暴露「结构」（路由、类型、依赖关系），从不暴露「数据」（密码、token、用户内容）。

### 安全：框架层级的防线

```
请求进入
  │
  ├─ BodyLimit ────────── 限制 payload 大小，防止大 body DoS
  ├─ RateLimiter ─────── Token bucket 限流 + 定期清理（防内存泄漏）
  ├─ Security Headers ── HSTS / XSS / nosniff / CSP / Referrer-Policy（预计算，零 GC）
  ├─ CORS ─────────────── Origin 白名单 + preflight 缓存
  ├─ CSRF ─────────────── crypto/rand 生成 token，constant-time 验证
  ├─ JWT ──────────────── 强制提供 Validator，未设定一律 401（安全默认）
  ├─ BasicAuth ────────── constant-time 密码比对，防时序攻击
  └─ CircuitBreaker ──── sync.Mutex 保护状态，防止竞态条件
```

每个安全中间件都经过以下考量：

- **安全默认** — JWT 未提供 Validator 一律拒绝，不会意外放行
- **密码学安全** — CSRF token 和 Request ID 使用 `crypto/rand`，不用可预测的时间戳
- **竞态安全** — Server shutdown 用 `atomic.Bool`，CircuitBreaker 用 `sync.Mutex`
- **资源防护** — RateLimiter 定期清理过期 key，SessionCache 带 LRU + TTL 上限

### 性能：协作机制不拖慢系统

AI 协作功能在运行时的性能影响几乎为零，因为大部分工作发生在 **启动时** 或 **开发时**，而非每个请求的热路径上：

| 机制 | 何时执行 | 热路径影响 |
|------|---------|-----------|
| Schema Registry | 路由注册时（启动） | 零 — 只是内存中的 map 查找 |
| Manifest 生成 | `hyp context` 或 `Server.Start()`（启动） | 零 — 不在请求路径上 |
| AutoSync | Server 启动时写一次文件 | 零 — 原子写入，不阻塞请求 |
| Contract Testing | `go test` 时（开发） | 零 — 不进入生产环境 |
| Impact Analysis | `hyp impact` 时（开发） | 零 — CLI 工具，不进入生产环境 |
| Annotation Check | `hyp chkcomment` 时（开发） | 零 — CLI 工具，不进入生产环境 |
| Diagnostic Endpoint | 被请求时（按需） | 极低 — Rate limited（每分钟 10 次） |

### 性能：请求热路径上的优化

框架核心的每个请求处理路径都经过 GC 优化：

```
请求进入 → Radix Tree 查找（O(k)）
         → LRU 缓存命中（O(1)，cacheItem pool 回收）
         → Context 从 sync.Pool 取出（零分配）
         → Params 从 pool 取出（零分配）
         → Security headers 预计算字符串（零 Sprintf）
         → Replica 轮询（atomic.Pointer，零锁竞争）
         → 请求完成 → Context 归还 pool
```

**结果**：AI 协作功能带来的 metadata 存储（Schema Registry、Manifest）只占用启动时的微量内存，不影响每请求延迟。你同时得到了 AI 友善的开发体验，和与纯手工框架相同的生产性能。

## 授权

HypGo 采用 [MIT 授权](LICENSE) 发布。

---

由台湾卯小月 用❤️制作
