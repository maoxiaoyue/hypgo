package database

import (
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// ReadReplica 讀取副本連接
type ReadReplica struct {
	sqlDB *sql.DB
	bunDB *bun.DB
}

// ReplicaPool 讀取副本連接池（支持輪詢負載均衡）
type ReplicaPool struct {
	replicas []ReadReplica
	counter  atomic.Uint64
	mu       sync.RWMutex
}

// NewReplicaPool 創建讀取副本池
func NewReplicaPool() *ReplicaPool {
	return &ReplicaPool{
		replicas: make([]ReadReplica, 0),
	}
}

// Add 添加讀取副本
func (rp *ReplicaPool) Add(replica ReadReplica) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.replicas = append(rp.replicas, replica)
}

// Next 獲取下一個讀取副本的 Bun ORM 實例（輪詢）
// 如果沒有可用的讀取副本，返回 nil
func (rp *ReplicaPool) Next() *bun.DB {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	if len(rp.replicas) == 0 {
		return nil
	}

	idx := rp.counter.Add(1) - 1
	return rp.replicas[idx%uint64(len(rp.replicas))].bunDB
}

// NextSQL 獲取下一個讀取副本的原始 SQL 連接（輪詢）
// 如果沒有可用的讀取副本，返回 nil
func (rp *ReplicaPool) NextSQL() *sql.DB {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	if len(rp.replicas) == 0 {
		return nil
	}

	idx := rp.counter.Add(1) - 1
	return rp.replicas[idx%uint64(len(rp.replicas))].sqlDB
}

// Len 返回副本數量
func (rp *ReplicaPool) Len() int {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	return len(rp.replicas)
}

// Close 關閉所有讀取副本連接
func (rp *ReplicaPool) Close() []error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	var errs []error
	for i, replica := range rp.replicas {
		if replica.bunDB != nil {
			if err := replica.bunDB.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close read replica %d: %w", i, err))
			}
		}
	}
	rp.replicas = nil
	return errs
}

// PingAll 對所有讀取副本進行健康檢查
func (rp *ReplicaPool) PingAll() []error {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	var errs []error
	for i, replica := range rp.replicas {
		if replica.sqlDB != nil {
			if err := replica.sqlDB.Ping(); err != nil {
				errs = append(errs, fmt.Errorf("read replica %d unhealthy: %w", i, err))
			}
		}
	}
	return errs
}

// initReplica 初始化單個讀取副本
func initReplica(driver string, replicaCfg config.ReplicaConfig) (ReadReplica, error) {
	if replicaCfg.DSN == "" {
		return ReadReplica{}, fmt.Errorf("replica DSN is required")
	}

	var driverName string
	switch driver {
	case "mysql", "tidb":
		driverName = "mysql"
	case "postgres":
		driverName = "postgres"
	default:
		return ReadReplica{}, fmt.Errorf("unsupported driver for read replica: %s", driver)
	}

	db, err := sql.Open(driverName, replicaCfg.DSN)
	if err != nil {
		return ReadReplica{}, fmt.Errorf("failed to open read replica: %w", err)
	}

	// 設置連接池參數
	if replicaCfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(replicaCfg.MaxIdleConns)
	}
	if replicaCfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(replicaCfg.MaxOpenConns)
	}

	// 測試連接
	if err := db.Ping(); err != nil {
		db.Close()
		return ReadReplica{}, fmt.Errorf("failed to ping read replica: %w", err)
	}

	// 創建 Bun ORM 實例
	var bunDB *bun.DB
	switch driver {
	case "mysql", "tidb":
		bunDB = bun.NewDB(db, mysqldialect.New())
	case "postgres":
		bunDB = bun.NewDB(db, pgdialect.New())
	}

	return ReadReplica{
		sqlDB: db,
		bunDB: bunDB,
	}, nil
}
