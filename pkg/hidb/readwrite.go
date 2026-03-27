package hidb

import (
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/uptrace/bun"
)

// ReadReplica 讀取副本連接
type ReadReplica struct {
	sqlDB *sql.DB
	hypDB *bun.DB
}

// ReplicaPool 讀取副本連接池（支持輪詢負載均衡）
// GC 優化：讀路徑使用 atomic.Pointer 避免 RWMutex 競爭
// 寫操作（Add/Close）仍使用 Mutex 保護
type ReplicaPool struct {
	replicas atomic.Pointer[[]ReadReplica] // GC 優化：讀路徑無鎖
	counter  atomic.Uint64
	mu       sync.Mutex // 僅保護寫操作
}

// NewReplicaPool 創建讀取副本池
func NewReplicaPool() *ReplicaPool {
	rp := &ReplicaPool{}
	empty := make([]ReadReplica, 0)
	rp.replicas.Store(&empty)
	return rp
}

// Add 添加讀取副本
// 寫操作：使用 Mutex 保護，copy-on-write 更新 atomic.Pointer
func (rp *ReplicaPool) Add(replica ReadReplica) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	old := rp.replicas.Load()
	newSlice := make([]ReadReplica, len(*old)+1)
	copy(newSlice, *old)
	newSlice[len(*old)] = replica
	rp.replicas.Store(&newSlice)
}

// Next 獲取下一個讀取副本的 HypDB ORM 實例（輪詢）
// GC 優化：讀路徑完全無鎖，使用 atomic.Pointer 讀取
func (rp *ReplicaPool) Next() *bun.DB {
	replicas := *rp.replicas.Load()
	if len(replicas) == 0 {
		return nil
	}
	idx := rp.counter.Add(1) - 1
	return replicas[idx%uint64(len(replicas))].hypDB
}

// NextSQL 獲取下一個讀取副本的原始 SQL 連接（輪詢）
// GC 優化：讀路徑完全無鎖
func (rp *ReplicaPool) NextSQL() *sql.DB {
	replicas := *rp.replicas.Load()
	if len(replicas) == 0 {
		return nil
	}
	idx := rp.counter.Add(1) - 1
	return replicas[idx%uint64(len(replicas))].sqlDB
}

// Len 返回副本數量
func (rp *ReplicaPool) Len() int {
	return len(*rp.replicas.Load())
}

// Close 關閉所有讀取副本連接
func (rp *ReplicaPool) Close() []error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	replicas := *rp.replicas.Load()
	var errs []error
	for i, replica := range replicas {
		if replica.hypDB != nil {
			if err := replica.hypDB.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close read replica %d: %w", i, err))
			}
		}
	}
	empty := make([]ReadReplica, 0)
	rp.replicas.Store(&empty)
	return errs
}

// PingAll 對所有讀取副本進行健康檢查
func (rp *ReplicaPool) PingAll() []error {
	replicas := *rp.replicas.Load()
	var errs []error
	for i, replica := range replicas {
		if replica.sqlDB != nil {
			if err := replica.sqlDB.Ping(); err != nil {
				errs = append(errs, fmt.Errorf("read replica %d unhealthy: %w", i, err))
			}
		}
	}
	return errs
}

// initReplica 初始化單個讀取副本（使用 Dialect）
func initReplica(dialect Dialect, replicaCfg config.ReplicaConfig) (ReadReplica, error) {
	if replicaCfg.DSN == "" {
		return ReadReplica{}, fmt.Errorf("replica DSN is required")
	}

	db, err := sql.Open(dialect.DriverName(), replicaCfg.DSN)
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

	// GC 優化：Replica 也設定 ConnMaxLifetime，防止長連線持有過期狀態
	db.SetConnMaxLifetime(30 * time.Minute)

	// 測試連接
	if err := db.Ping(); err != nil {
		db.Close()
		return ReadReplica{}, fmt.Errorf("failed to ping read replica: %w", err)
	}

	// 創建 HypDB ORM 實例
	hypDB := bun.NewDB(db, dialect.BunDialect())

	return ReadReplica{
		sqlDB: db,
		hypDB: hypDB,
	}, nil
}
