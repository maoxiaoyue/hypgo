# HypGo

**The first Go web framework where AI is a first-class developer.**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-0.8.1-orange.svg)](https://github.com/maoxiaoyue/hypgo/releases)

[English](README.md) | [繁體中文](README_zh-TW.md) | [简体中文](README_zh-CN.md)

---

Gin and Echo were built for humans writing code. HypGo is built for the way you actually work in 2026 — **you and AI, together**.

```
Traditional framework:
  AI reads 20 handler files → guesses Input/Output → generates → you review line by line

HypGo:
  AI reads 1 manifest file → knows Input/Output → generates → framework auto-validates
```

### The numbers

| What AI needs to do | Traditional framework | HypGo | Savings |
|---------------------|----------------------|-------|---------|
| Understand your API structure | Read all handlers (~5,000 tokens) | Read manifest (~500 tokens) | **90%** |
| Understand a single route | Read handler + service + model (~2,000 tokens) | Read schema metadata (~200 tokens) | **90%** |
| Validate generated code | Write tests manually (~1,500 tokens) | `contract.TestAll()` one line (~50 tokens) | **97%** |
| Assess change impact | Search imports file by file (~3,000 tokens) | `hyp impact` (~100 tokens) | **97%** |

For a 20-route project, **each AI interaction saves 4,000–8,000 tokens**. That's faster responses, more accurate generation, and longer context for complex reasoning.

---

## What makes HypGo different

### 1. Schema-first routes — AI reads metadata, not source code

```go
// Traditional: AI must read the entire handler to understand what this does
r.POST("/api/users", createUserHandler)

// HypGo: AI reads 6 lines of structured metadata
r.Schema(schema.Route{
    Method:  "POST",
    Path:    "/api/users",
    Summary: "Create user",
    Input:   CreateUserReq{},
    Output:  UserResp{},
}).Handle(createUserHandler)
```

### 2. Project Manifest — one file replaces 20

```bash
hyp context -o .hyp/manifest.yaml
```

```yaml
# AI reads this instead of your entire codebase
routes:
  - method: POST
    path: /api/users
    summary: "Create user"
    input_type: CreateUserReq
    output_type: UserResp
    handler_names: [controllers.CreateUser]
```

### 3. Contract Testing — AI output validated automatically

```go
// One line tests every schema-registered route
contract.TestAll(t, router)
// → auto-generates test data from Input struct
// → validates response matches Output struct
// → checks status codes (POST→201, DELETE→204)
```

### Who does what

```
You define "what" (Schema)  →  AI implements "how" (Handler)  →  Framework checks "correct?" (Contract)
```

| | You | AI | Framework |
|---|-----|-----|-----------|
| **Define** | API design, business rules | — | — |
| **Implement** | Review logic | Generate boilerplate + CRUD | — |
| **Validate** | — | — | Auto-test Input/Output contracts |

---

## Quick Start

```bash
# Install
go install github.com/maoxiaoyue/hypgo/cmd/hyp@latest

# Create project
hyp api myservice && cd myservice && go mod tidy

# Generate a resource (model + controller + router in one step)
hyp generate model user
hyp generate controller user
```

This creates:

```
myservice/
├── app/
│   ├── controllers/user_controller.go   ← Handler logic (AI fills this in)
│   ├── models/user.go                   ← Structs: User, CreateUserReq, UserResp
│   ├── routers/
│   │   ├── user.go                      ← Schema routes (you define this)
│   │   ├── router.go                    ← Setup() entry point
│   │   └── middleware.go                ← Middleware config
│   └── services/
└── go.mod
```

### Minimal working example

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

## Full feature set

### AI Collaboration Toolchain

| Feature | What it does |
|---------|-------------|
| **Schema-first Routes** | Routes carry Input/Output types — AI understands API without reading handler code |
| **Project Manifest** | `hyp context` outputs your entire project structure in ~500 tokens |
| **Contract Testing** | `contract.TestAll(t, router)` validates all routes in one line |
| **AI Rules** | `hyp ai-rules` generates config for Codex, Gemini, Cursor, Copilot, Windsurf |
| **Typed Error Catalog** | Predefined error codes (`E1001`) — AI uses them instead of inventing ad-hoc errors |
| **Annotation Protocol** | `// @ai:constraint max=100` — AI reads business constraints from comments |
| **Change Impact** | `hyp impact <file>` shows blast radius before modifying shared packages |
| **AutoSync** | Server startup auto-generates `.hyp/context.yaml` — always in sync |
| **Migration Diff** | Model struct changes auto-generate up/down SQL |
| **Diagnostic** | `GET /_debug/state` returns full system snapshot in one request |

### Network & Performance

| Feature | Description |
|---------|-------------|
| HTTP/1.1 + HTTP/2 + HTTP/3 | Three protocols simultaneously, automatic ALPN + Alt-Svc |
| Radix Tree Router | O(k) lookup + LRU cache + parameter pooling, zero GC pressure |
| WebSocket | JSON / Protobuf / FlatBuffers / MessagePack + AES-256-GCM |
| Graceful Shutdown/Restart | Parallel shutdown, SIGUSR2 restart, zero downtime |
| GC Optimized | 8 sync.Pools, map rebuild, atomic.Pointer replica reads |

### Database

| Feature | Description |
|---------|-------------|
| Bun ORM | MySQL / PostgreSQL / TiDB with lock-free read-write splitting |
| Redis / KeyDB | 35 high-level methods (KV, Hash, List, Set, ZSet, Pub/Sub, Pipeline) |
| Cassandra | Plugin system |

---

## Security: AI sees structure, not secrets

| Layer | AI can see | AI cannot see | How |
|-------|-----------|---------------|-----|
| **Manifest** | Route paths, type names | Passwords, DSN, tokens | AutoSync filters sensitive fields |
| **Diagnostic** | Connection status, memory | Database contents, user data | DSN redact (`***@host:port/db`) |
| **Schema** | Field names and types | Actual values | Type metadata only |
| **Impact** | Import graph, test counts | File contents | Scans imports only |

Every security middleware uses **secure defaults**: JWT rejects without Validator, CSRF uses `crypto/rand`, CircuitBreaker uses `sync.Mutex`, RateLimiter auto-cleans expired keys.

---

## Performance: zero cost for AI features

AI collaboration runs at **startup or dev time**, never on the request hot path:

```
Request → Radix Tree O(k) → LRU cache O(1)
       → Context from sync.Pool (zero alloc) → Params from pool (zero alloc)
       → Security headers precomputed (zero Sprintf)
       → Replica round-robin via atomic.Pointer (zero lock)
       → Response → Context returned to pool
```

Schema Registry and Manifest consume only startup memory. Per-request latency is identical to frameworks without AI features.

---

## CLI Commands

```bash
# Project
hyp new <name>              # Full-stack project
hyp api <name>              # API-only project

# AI Collaboration
hyp context                 # Generate manifest (YAML)
hyp ai-rules                # Generate AI tool config files
hyp chkcomment <file>       # Check annotation completeness
hyp impact <file>           # Change impact analysis

# Code Generation
hyp generate controller <name>   # Controller + Router + Middleware
hyp generate model <name>       # Model + Request/Response structs
hyp generate service <name>     # Service with Error Catalog

# Database
hyp migrate diff            # Generate SQL from model changes
hyp migrate snapshot        # Save schema baseline

# Deployment
hyp docker                  # Build Docker image
hyp health                  # Health check
```

---

## License

MIT License. Made from Taiwan.
