# HypGo

**A Modern Go Web Framework Designed for AI-Human Collaborative Development**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-0.7.0-orange.svg)](https://github.com/maoxiaoyue/hypgo/releases)

[English](README.md) | [繁體中文](README_zh-TW.md) | [简体中文](README_zh-CN.md)

## What is HypGo?

HypGo is a modern Go web framework with native HTTP/1.1, HTTP/2, and HTTP/3 (QUIC) support, featuring a built-in **AI-human collaborative development** toolchain. The framework optimizes not just human coding efficiency, but also enables AI to quickly understand, correctly generate, and immediately validate code.

### Why HypGo?

Traditional frameworks only solve the "human writes code" problem. In the age of AI-assisted development, frameworks also need to solve:

1. **Discoverability** — AI understands the entire project with minimal tokens
2. **Predictability** — Strict conventions ensure AI-generated code is placed and styled consistently
3. **Verifiability** — Generated code can be validated immediately without manual line-by-line review

Every feature in HypGo is designed around these three principles.

## Core Features

### High-Performance Network Layer

| Feature | Description |
|---------|-------------|
| HTTP/1.1 + HTTP/2 + HTTP/3 | Three protocols running simultaneously with automatic ALPN negotiation and Alt-Svc upgrade |
| 0-RTT Session Cache | TLS 1.3 fast resumption with LRU + TTL + replay attack protection |
| Radix Tree Router | O(k) path lookup + LRU cache + parameter pooling, zero GC pressure |
| WebSocket Multi-Protocol | JSON / Protobuf / FlatBuffers / MessagePack + AES-256-GCM encryption |
| Graceful Shutdown | Parallel HTTP/1+2 and HTTP/3 shutdown with atomic race protection |
| Graceful Restart | Unix SIGUSR2 triggered, FD passing + poll wait, zero downtime |

### AI Collaboration Toolchain

| Feature | Description |
|---------|-------------|
| **Schema-first Routes** | Routes carry Input/Output types, descriptions, tags — AI understands API behavior directly |
| **Project Manifest** | `hyp context` generates YAML/JSON project description — AI grasps everything at once |
| **Contract Testing** | `contract.TestAll(t, router)` validates all schema routes in one line |
| **Typed Error Catalog** | Predefined structured error codes (`E1001`), unified handler error format |
| **Migration Diff** | Auto-generates up/down SQL migrations from Model struct changes |
| **Annotation Protocol** | `// @ai:constraint` structured annotations — AI reads business constraints from comments |
| **Change Impact** | `hyp impact <file>` analyzes affected routes, tests, and downstream modules |
| **AutoSync** | Automatically updates `.hyp/context.yaml` on Server start — always in sync with code |
| **Diagnostic Endpoint** | `GET /_debug/state` returns complete system snapshot in one request |

### Developer Experience

| Feature | Description |
|---------|-------------|
| Smart Scaffold | `hyp gen` generates code integrated with Schema + Error Catalog |
| Test Fixture Builder | `fixture.Request(router).POST("/api").WithJSON(body).Expect(201).Run(t)` |
| Hot Reload | Structured file monitoring + debounce + categorized change summary |
| BodyLimit | Limits request body size to prevent DoS |
| MethodOverride | Supports `X-HTTP-Method-Override` header and `_method` parameter |

### Database

| Feature | Description |
|---------|-------------|
| Bun ORM | MySQL / PostgreSQL / TiDB read-write splitting (lock-free round-robin) |
| Redis / KeyDB | 35 high-level methods (KV, Hash, List, Set, ZSet, Pub/Sub, Pipeline) |
| Cassandra | Plugin system for dynamic loading |
| ConnMaxLifetime | Unified 30-minute limit to prevent stale connections |

## Requirements

- Go 1.24 or above
- Docker (optional, for containerization)

## Installation

```bash
# Install framework
go get -u github.com/maoxiaoyue/hypgo

# Install CLI tool (ensure $GOPATH/bin is in your $PATH)
go install github.com/maoxiaoyue/hypgo/cmd/hyp@latest
```

## Quick Start

### Create a Project

```bash
# Full-stack project (with frontend templates)
hyp new myapp && cd myapp && go mod tidy && hyp run

# API-only project
hyp api myapi && cd myapi && go mod tidy && hyp run
```

### Minimal Working Example

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

    // Schema-first route — AI understands API directly from metadata
    r.Schema(schema.Route{
        Method:  "POST",
        Path:    "/api/users",
        Summary: "Create user",
        Tags:    []string{"users"},
        Input:   CreateUserReq{},
        Output:  UserResp{},
    }).Handle(func(c *context.Context) {
        c.JSON(201, UserResp{ID: 1, Name: "test", Email: "test@test.com"})
    })

    // Traditional routes work too
    r.GET("/health", func(c *context.Context) {
        c.JSON(200, map[string]string{"status": "ok"})
    })

    srv.Start()
}
```

### Configuration Example

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

## AI Collaborative Development Workflow

HypGo's AI collaboration is not "AI autocompletes your code" — it is a complete **Understand → Generate → Validate** loop:

### 1. AI Understands the Project

```bash
# Generate project manifest (this is all AI needs to read)
hyp context -o .hyp/manifest.yaml
```

```yaml
# .hyp/manifest.yaml — AI's "map"
routes:
  - method: POST
    path: /api/users
    summary: "Create user"
    input_type: CreateUserReq
    output_type: UserResp
    handler_names: [main.createUser]
```

### 2. AI Generates Code

AI generates handlers based on manifest + schema:

```go
// AI knows Input is CreateUserReq, Output is UserResp
// AI knows to use errors.Define for error codes
// AI knows to register with router.Schema()
```

### 3. Automatic Validation

```go
// Test all schema routes in one line
contract.TestAll(t, router)

// Or test a specific route manually
contract.Test(t, router, contract.TestCase{
    Route:        "POST /api/users",
    Input:        `{"name":"test","email":"test@test.com"}`,
    ExpectStatus: 201,
    ExpectSchema: true,  // auto-validates response matches UserResp schema
})
```

### 4. Pre-Change Impact Analysis

```bash
hyp impact pkg/errors/catalog.go
# → 3 packages depend on this, 43 tests affected, Risk: MEDIUM
```

### 5. Comment Checking

```bash
hyp chkcomment controllers/user.go
# → 2/4 blocks have comments (50%)
# → run with --fix to add suggestions
```

## CLI Commands

```bash
# Project Management
hyp new <name>          # Create full-stack project
hyp api <name>          # Create API-only project
hyp run                 # Start with hot reload
hyp restart             # Zero-downtime restart

# AI Collaboration
hyp context             # Generate project manifest (YAML)
hyp context -f json     # JSON format
hyp chkcomment <file>   # Check comment completeness
hyp impact <file>       # Change impact analysis

# Code Generation
hyp generate controller <name>
hyp generate model <name>
hyp generate service <name>

# Database
hyp migrate diff        # Compare models with snapshot, generate SQL
hyp migrate snapshot    # Save current schema snapshot

# Deployment
hyp docker              # Build Docker image
hyp health              # Health check
```

## Token Efficiency: Why HypGo Dramatically Reduces AI Development Costs

In AI-assisted development, **tokens are cost**. Every time AI needs to understand your project, it consumes massive tokens reading source code. HypGo solves this at the architecture level:

### Traditional Framework vs HypGo

| Scenario | Traditional | HypGo | Token Savings |
|----------|------------|-------|---------------|
| AI understands API structure | Read all handler files (~5,000 tokens) | Read manifest.yaml (~500 tokens) | **90%** |
| AI understands single route | Read handler + service + model (~2,000 tokens) | Read schema metadata (~200 tokens) | **90%** |
| AI validates generated code | Manually write tests (~1,500 tokens) | `contract.TestAll()` one line (~50 tokens) | **97%** |
| AI assesses change impact | Search imports file-by-file (~3,000 tokens) | `hyp impact` (~100 tokens) | **97%** |

### How It Works

Traditional frameworks require AI to read massive source code to understand a project. HypGo front-loads critical information into **structured metadata**:

```
Traditional: AI reads handler → infers Input/Output → guesses constraints → generates → manual test
HypGo:       AI reads manifest → knows Input/Output → knows constraints → generates → auto-validates
```

For a 20-route project, each AI interaction saves **4,000-8,000 tokens**. Over time, this is a significant cost difference.

### Beyond Cost Savings

Token savings create cascading benefits:

- **Faster responses** — AI reads less code, responds faster
- **More accurate generation** — Structured information is harder to misinterpret than free text
- **Longer context** — Saved tokens can be used for more complex reasoning
- **Lower error rates** — Automated validation replaces manual review, reducing oversights

## Security & Performance: The Hidden Cost of AI Collaboration and HypGo's Solution

Giving AI access to project information improves efficiency but introduces risk. HypGo's design principle: **Let AI see structure, not secrets. Keep the system fast — collaboration mechanisms must not slow things down.**

### Security: What AI Can and Cannot See

| Layer | AI Can See | AI Cannot See | Mechanism |
|-------|-----------|---------------|-----------|
| **Manifest** | Route paths, methods, type names | DSN, passwords, tokens, API keys | AutoSync auto-filters sensitive fields |
| **Diagnostic** | Route count, connection status, memory usage | Database contents, user data | DSN redact (`***@host:port/db`) |
| **Schema** | Input/Output struct field names and types | Actual data values | Stores type metadata only, never instances |
| **Annotation** | `@ai:constraint`, `@ai:security` tags | Business logic implementation details | Pure AST analysis, never executes code |
| **Impact** | Import dependency graph, test counts | File contents, variable values | Scans import paths only, never reads function bodies |

**Design principle**: AI collaboration tools expose only "structure" (routes, types, dependencies), never "data" (passwords, tokens, user content).

### Security: Framework-Level Defense Chain

```
Request enters
  │
  ├─ BodyLimit ────────── Limits payload size, prevents large-body DoS
  ├─ RateLimiter ─────── Token bucket throttling + periodic cleanup (prevents memory leak)
  ├─ Security Headers ── HSTS / XSS / nosniff / CSP / Referrer-Policy (precomputed, zero GC)
  ├─ CORS ─────────────── Origin whitelist + preflight caching
  ├─ CSRF ─────────────── crypto/rand token generation, constant-time validation
  ├─ JWT ──────────────── Mandatory Validator function, rejects by default if unconfigured (secure default)
  ├─ BasicAuth ────────── Constant-time password comparison, prevents timing attacks
  └─ CircuitBreaker ──── sync.Mutex state protection, prevents race conditions
```

Every security middleware follows these considerations:

- **Secure defaults** — JWT rejects all requests if no Validator is provided
- **Cryptographic security** — CSRF tokens and Request IDs use `crypto/rand`, not predictable timestamps
- **Race safety** — Server shutdown uses `atomic.Bool`, CircuitBreaker uses `sync.Mutex`
- **Resource protection** — RateLimiter periodically cleans expired keys, SessionCache has LRU + TTL limits

### Performance: Collaboration Mechanisms Don't Slow the System

AI collaboration features have near-zero runtime performance impact because most work happens at **startup** or **development time**, not on the per-request hot path:

| Mechanism | When It Runs | Hot Path Impact |
|-----------|-------------|-----------------|
| Schema Registry | Route registration (startup) | Zero — just an in-memory map lookup |
| Manifest Generation | `hyp context` or `Server.Start()` (startup) | Zero — not on request path |
| AutoSync | Writes file once on Server start | Zero — atomic write, doesn't block requests |
| Contract Testing | During `go test` (development) | Zero — doesn't enter production |
| Impact Analysis | During `hyp impact` (development) | Zero — CLI tool, doesn't enter production |
| Annotation Check | During `hyp chkcomment` (development) | Zero — CLI tool, doesn't enter production |
| Diagnostic Endpoint | When requested (on-demand) | Very low — rate limited (10/min) |

### Performance: Hot Path Optimizations

Every request processing path in the framework core is GC-optimized:

```
Request enters → Radix Tree lookup (O(k))
              → LRU cache hit (O(1), cacheItem pool recycled)
              → Context acquired from sync.Pool (zero allocation)
              → Params acquired from pool (zero allocation)
              → Security headers precomputed strings (zero Sprintf)
              → Replica round-robin (atomic.Pointer, zero lock contention)
              → Request complete → Context returned to pool
```

**Result**: AI collaboration metadata storage (Schema Registry, Manifest) only consumes minimal memory at startup, with zero impact on per-request latency. You get both an AI-friendly development experience and production performance identical to hand-crafted frameworks.

## License

HypGo is released under the [MIT License](LICENSE).

---

Made from Taiwan
