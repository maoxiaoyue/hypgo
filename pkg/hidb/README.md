# HiDB Package (`pkg/hidb`)

`hidb` 套件為 HypGo 框架提供資料庫抽象層與連線管理器，支援多資料庫引擎、讀寫分離（Master-Replica）、連線池、交易管理、Redis 快取，以及可擴展的插件系統。

## 主要特色

- **多資料庫支援**: 透過 `Dialect` 介面抽象 MySQL / PostgreSQL 的驅動差異，使用 [Bun ORM](https://bun.uptrace.dev/) 作為查詢建構層。
- **讀寫分離**: 寫入操作始終路由至 Master，讀取操作透過 `ReplicaPool` 以 Round-Robin 分配至 Replica，無 Replica 時自動回退至 Master。
- **Lock-Free Round-Robin**: 使用 `atomic.Uint64` 實現無鎖輪詢，高併發場景下零競爭。
- **連線池管理**: 支援 `MaxIdleConns` / `MaxOpenConns` 配置，Master 與每個 Replica 獨立管理連線池。
- **交易管理**: 同時提供原生 `sql.Tx` 與 Bun ORM `bun.Tx` 兩種交易介面。
- **Redis 整合**: 內建 `go-redis/v9` 客戶端，與 SQL 資料庫統一管理生命週期。
- **插件系統**: 透過 `DatabasePlugin` 介面支援非 SQL 資料庫（如 Cassandra）的動態註冊與載入。
- **優雅關閉**: 按順序關閉 Replica → Master → Redis → Plugins，累積錯誤統一報告。
- **向後兼容**: `HypDB()` / `SQL()` 始終返回 Master，舊有程式碼無需修改。

## 基礎使用

```go
package main

import (
	"context"
	"log"

	"github.com/maoxiaoyue/hypgo/pkg/hidb"
	"github.com/maoxiaoyue/hypgo/pkg/hidb/pg"
)

func main() {
	// 1. 建立資料庫管理器（PostgreSQL）
	db, err := hidb.NewWithInterface(
		appConfig.Database,           // 實作 config.DatabaseConfigInterface
		hidb.WithDialect(pg.New()),   // 指定 SQL 方言
	)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 2. 健康檢查
	if err := db.HealthCheck(context.Background()); err != nil {
		log.Fatal(err)
	}

	// 3. 讀取操作 → Replica（無 Replica 時回退至 Master）
	readDB := db.ReadHypDB()
	var users []User
	readDB.NewSelect().Model(&users).Scan(context.Background())

	// 4. 寫入操作 → 始終使用 Master
	writeDB := db.WriteHypDB()
	writeDB.NewInsert().Model(&newUser).Exec(context.Background())
}
```

## 讀寫分離 (Master-Replica)

### 配置

透過實作 `config.ReplicaConfigProvider` 介面啟用讀寫分離：

```go
type ReplicaConfigProvider interface {
	GetReplicas() []ReplicaConfig
}

type ReplicaConfig struct {
	DSN          string // Replica 連線字串
	MaxIdleConns int    // 最大閒置連線數
	MaxOpenConns int    // 最大連線數
}
```

### 查詢路由

| 方法 | 目標 | 說明 |
|------|------|------|
| `ReadHypDB()` | Replica (ORM) | Round-Robin 分配，無 Replica 回退 Master |
| `ReadSQL()` | Replica (Raw SQL) | Round-Robin 分配，無 Replica 回退 Master |
| `WriteHypDB()` | Master (ORM) | 始終使用 Master |
| `WriteSQL()` | Master (Raw SQL) | 始終使用 Master |
| `HypDB()` | Master (ORM) | 向後兼容，等同 `WriteHypDB()` |
| `SQL()` | Master (Raw SQL) | 向後兼容，等同 `WriteSQL()` |

### Round-Robin 負載均衡

```go
// Lock-free 實作，高併發零競爭
counter atomic.Uint64
idx := counter.Add(1) - 1
replica := replicas[idx % uint64(len(replicas))]
```

### 狀態查詢

```go
db.HasReplicas()   // bool — 是否配置了 Replica
db.ReplicaCount()  // int  — Replica 數量
```

## 交易管理

### 原生 SQL 交易

```go
err := db.Transaction(ctx, func(tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")
	if err != nil {
		return err // 自動 Rollback
	}
	return nil // 自動 Commit
})
```

### Bun ORM 交易

```go
err := db.HypDBTransaction(ctx, func(ctx context.Context, tx bun.Tx) error {
	_, err := tx.NewInsert().Model(&user).Exec(ctx)
	if err != nil {
		return err // 自動 Rollback
	}
	return nil // 自動 Commit
})
```

## Redis 整合

```go
// 取得 Redis 客戶端
redisClient := db.Redis()

// Redis 操作
redisClient.Set(ctx, "key", "value", 0)
val, err := redisClient.Get(ctx, "key").Result()
```

Redis 配置透過 `config.RedisConfigInterface` 提供：

```go
type RedisConfigInterface interface {
	GetAddr() string
	GetPassword() string
	GetDB() int
}
```

## Dialect 方言系統

### 介面

```go
type Dialect interface {
	DriverName() string          // "mysql" 或 "postgres"
	BunDialect() schema.Dialect  // Bun ORM 方言實例
}
```

### 內建方言

**MySQL / TiDB：**

```go
import "github.com/maoxiaoyue/hypgo/pkg/hidb/mysql"

db, err := hidb.NewWithInterface(cfg, hidb.WithDialect(mysql.New()))
```

**PostgreSQL：**

```go
import "github.com/maoxiaoyue/hypgo/pkg/hidb/pg"

db, err := hidb.NewWithInterface(cfg, hidb.WithDialect(pg.New()))
```

## 插件系統

### DatabasePlugin 介面

```go
type DatabasePlugin interface {
	Name() string
	Init(config map[string]interface{}) error
	Connect() error
	Close() error
	Ping(ctx context.Context) error
}
```

### 註冊與使用

```go
// 註冊插件
cassandraPlugin := cassandra.NewPlugin()
db.RegisterPlugin(cassandraPlugin)

// 動態載入（Init + Connect）
db.LoadPlugin("cassandra", map[string]interface{}{
	"hosts":    []string{"127.0.0.1"},
	"keyspace": "my_keyspace",
})

// 取得插件
if plugin, ok := db.GetPlugin("cassandra"); ok {
	plugin.Ping(ctx)
}
```

### Cassandra 插件

`pkg/hidb/cassandra` 提供內建的 Cassandra/ScyllaDB 插件：

```go
import "github.com/maoxiaoyue/hypgo/pkg/hidb/cassandra"

cdb, err := cassandra.New(cassandra.Config{
	Hosts:       []string{"127.0.0.1", "127.0.0.2"},
	Keyspace:    "my_keyspace",
	Port:        9042,
	Consistency: "quorum",
	Username:    "admin",
	Password:    "secret",
	NumConns:    2,
})
defer cdb.Close()

// 查詢
query := cdb.Query("SELECT * FROM users WHERE id = ?", userID)

// 批次操作
batch := cdb.Batch(gocql.LoggedBatch)
```

**Cassandra 配置選項：**

| 欄位 | 說明 |
|------|------|
| `Hosts` | 叢集節點位址 |
| `Keyspace` | 鍵空間名稱 |
| `Port` | 連接埠（預設 9042） |
| `Consistency` | 一致性等級（one, quorum, all, local_quorum 等） |
| `Username` / `Password` | 認證資訊 |
| `ConnectTimeout` / `Timeout` | 連線與查詢逾時 |
| `NumConns` | 每節點連線數 |
| `MaxPreparedStmts` | 預備語句快取上限 |
| `ProtoVersion` | CQL 協議版本 |

## 健康檢查與生命週期

```go
// 連線狀態
db.IsConnected()  // bool

// 完整健康檢查（Master + Replicas + Redis + Plugins）
err := db.HealthCheck(ctx)

// 資料庫驅動類型
db.Type()  // "mysql", "postgres", "redis", etc.

// 優雅關閉（按順序：Replicas → Master → Redis → Plugins）
err := db.Close()
```

## 向後兼容工廠

當配置物件未實作 `config.DatabaseConfigInterface` 時，`New()` 透過反射自動適配：

```go
// 傳統結構體（使用反射適配）
db, err := hidb.New(legacyConfig, hidb.WithDialect(pg.New()))

// 推薦：實作 DatabaseConfigInterface
db, err := hidb.NewWithInterface(modernConfig, hidb.WithDialect(pg.New()))
```

`DatabaseConfigAdapter` 透過反射提取 `Driver`、`DSN`、`MaxIdleConns`、`MaxOpenConns`、`Redis` 等欄位，確保舊有程式碼無需修改。

## 檔案結構

```
pkg/hidb/
├── hidb.go              # 核心：Database, Dialect, Option, Plugin 系統,
│                         #   交易管理, 健康檢查, 優雅關閉
├── readwrite.go         # ReplicaPool: Round-Robin 負載均衡, 讀寫分離
├── readwrite_test.go    # 11 項測試：Round-Robin, 併發, 回退, 關閉
├── mysql/
│   └── mysql.go         # MySQL / TiDB 方言實作
├── pg/
│   └── pg.go            # PostgreSQL 方言實作
└── cassandra/
    └── cassandra.go     # Cassandra 插件實作
```

## 依賴

| 套件 | 用途 |
|------|------|
| `database/sql` | Go 標準 SQL 介面 |
| `github.com/uptrace/bun` | ORM 查詢建構 |
| `github.com/redis/go-redis/v9` | Redis 客戶端 |
| `github.com/go-sql-driver/mysql` | MySQL 驅動 |
| `github.com/lib/pq` | PostgreSQL 驅動 |
| `github.com/gocql/gocql` | Cassandra 驅動 |
