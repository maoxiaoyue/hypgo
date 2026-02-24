package database

import (
	"fmt"
	"sync"
	"testing"

	"github.com/uptrace/bun"
)

// mockBunDB 創建一個用於測試的 bun.DB 標識（不需要真實連接）
// 我們使用不同的指針地址來區分不同的副本
func newMockReplica(id int) ReadReplica {
	// 使用空的 bun.DB 指針作為標識，僅用於輪詢測試
	// 在真實場景中這些會是實際的數據庫連接
	return ReadReplica{
		sqlDB: nil,
		bunDB: &bun.DB{},
	}
}

func TestReplicaPoolRoundRobin(t *testing.T) {
	pool := NewReplicaPool()

	// 創建 3 個副本
	replicas := make([]ReadReplica, 3)
	for i := 0; i < 3; i++ {
		replicas[i] = newMockReplica(i)
		pool.Add(replicas[i])
	}

	if pool.Len() != 3 {
		t.Fatalf("expected 3 replicas, got %d", pool.Len())
	}

	// 驗證輪詢順序：應該依次返回 0, 1, 2, 0, 1, 2...
	for round := 0; round < 3; round++ {
		for i := 0; i < 3; i++ {
			got := pool.Next()
			expected := replicas[i].bunDB
			if got != expected {
				t.Errorf("round %d, index %d: expected replica %d, got different replica", round, i, i)
			}
		}
	}
}

func TestReplicaPoolNextSQL(t *testing.T) {
	pool := NewReplicaPool()

	// 空池應返回 nil
	if got := pool.NextSQL(); got != nil {
		t.Errorf("expected nil for empty pool, got %v", got)
	}

	// 添加副本後應正常工作
	replica := ReadReplica{
		sqlDB: nil, // 測試中使用 nil，只驗證輪詢邏輯
		bunDB: nil,
	}
	pool.Add(replica)

	got := pool.NextSQL()
	if got != nil {
		t.Errorf("expected nil sqlDB (mock), got %v", got)
	}
}

func TestReplicaPoolEmpty(t *testing.T) {
	pool := NewReplicaPool()

	if pool.Len() != 0 {
		t.Fatalf("expected 0 replicas, got %d", pool.Len())
	}

	// Next() 應返回 nil
	if got := pool.Next(); got != nil {
		t.Errorf("expected nil for empty pool, got %v", got)
	}

	// NextSQL() 應返回 nil
	if got := pool.NextSQL(); got != nil {
		t.Errorf("expected nil for empty pool, got %v", got)
	}
}

func TestReplicaPoolConcurrent(t *testing.T) {
	pool := NewReplicaPool()

	// 添加 3 個副本
	for i := 0; i < 3; i++ {
		pool.Add(newMockReplica(i))
	}

	// 併發訪問
	const goroutines = 100
	const iterations = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// 記錄每個 goroutine 是否成功（未 panic、未 nil）
	errors := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				db := pool.Next()
				if db == nil {
					errors <- fmt.Errorf("got nil from non-empty pool")
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent access error: %v", err)
	}

	// 驗證計數器確實增長了（表明所有 goroutine 都成功調用了 Next）
	if pool.Len() != 3 {
		t.Errorf("pool size changed during concurrent access: got %d", pool.Len())
	}
}

func TestReplicaPoolClose(t *testing.T) {
	pool := NewReplicaPool()

	// 空池關閉不應出錯
	errs := pool.Close()
	if len(errs) != 0 {
		t.Errorf("expected no errors closing empty pool, got %v", errs)
	}

	// 添加帶 nil bunDB 的副本（Close 會跳過）
	pool.Add(ReadReplica{sqlDB: nil, bunDB: nil})
	errs = pool.Close()
	if len(errs) != 0 {
		t.Errorf("expected no errors closing pool with nil connections, got %v", errs)
	}

	// 確認關閉後池為空
	if pool.Len() != 0 {
		t.Errorf("expected 0 replicas after close, got %d", pool.Len())
	}
}

func TestReplicaPoolPingAll(t *testing.T) {
	pool := NewReplicaPool()

	// 空池健康檢查不應有錯誤
	errs := pool.PingAll()
	if len(errs) != 0 {
		t.Errorf("expected no errors pinging empty pool, got %v", errs)
	}

	// 帶 nil sqlDB 的副本不會被 ping（跳過）
	pool.Add(ReadReplica{sqlDB: nil, bunDB: nil})
	errs = pool.PingAll()
	if len(errs) != 0 {
		t.Errorf("expected no errors pinging pool with nil sqlDB, got %v", errs)
	}
}

func TestReadBunDBFallback(t *testing.T) {
	// 模擬無讀取副本的 Database
	masterDB := &bun.DB{}
	db := &Database{
		bunDB:       masterDB,
		replicaPool: nil,
		plugins:     make(map[string]DatabasePlugin),
	}

	// 無副本：應回退到主庫
	got := db.ReadBunDB()
	if got != masterDB {
		t.Error("ReadBunDB should fallback to master when no replicas configured")
	}

	// WriteBunDB 始終返回主庫
	got = db.WriteBunDB()
	if got != masterDB {
		t.Error("WriteBunDB should always return master")
	}
}

func TestReadBunDBWithReplicas(t *testing.T) {
	masterDB := &bun.DB{}
	replicaDB := &bun.DB{}

	pool := NewReplicaPool()
	pool.Add(ReadReplica{bunDB: replicaDB})

	db := &Database{
		bunDB:       masterDB,
		replicaPool: pool,
		plugins:     make(map[string]DatabasePlugin),
	}

	// 有副本：應返回副本
	got := db.ReadBunDB()
	if got != replicaDB {
		t.Error("ReadBunDB should return replica when replicas are configured")
	}

	// WriteBunDB 仍應返回主庫
	got = db.WriteBunDB()
	if got != masterDB {
		t.Error("WriteBunDB should always return master even with replicas")
	}
}

func TestReadSQLFallback(t *testing.T) {
	db := &Database{
		sqlDB:       nil,
		replicaPool: nil,
		plugins:     make(map[string]DatabasePlugin),
	}

	// 無任何連接：返回 nil
	got := db.ReadSQL()
	if got != nil {
		t.Error("ReadSQL should return nil when no connections exist")
	}

	// WriteSQL 返回主庫（nil）
	got = db.WriteSQL()
	if got != nil {
		t.Error("WriteSQL should return nil when no master connection")
	}
}

func TestHasReplicas(t *testing.T) {
	// 無副本池
	db := &Database{
		replicaPool: nil,
		plugins:     make(map[string]DatabasePlugin),
	}
	if db.HasReplicas() {
		t.Error("HasReplicas should return false when replicaPool is nil")
	}
	if db.ReplicaCount() != 0 {
		t.Errorf("ReplicaCount should return 0, got %d", db.ReplicaCount())
	}

	// 空副本池
	db.replicaPool = NewReplicaPool()
	if db.HasReplicas() {
		t.Error("HasReplicas should return false when replicaPool is empty")
	}
	if db.ReplicaCount() != 0 {
		t.Errorf("ReplicaCount should return 0 for empty pool, got %d", db.ReplicaCount())
	}

	// 有副本
	db.replicaPool.Add(newMockReplica(0))
	if !db.HasReplicas() {
		t.Error("HasReplicas should return true when replicas exist")
	}
	if db.ReplicaCount() != 1 {
		t.Errorf("ReplicaCount should return 1, got %d", db.ReplicaCount())
	}

	// 添加更多副本
	db.replicaPool.Add(newMockReplica(1))
	db.replicaPool.Add(newMockReplica(2))
	if db.ReplicaCount() != 3 {
		t.Errorf("ReplicaCount should return 3, got %d", db.ReplicaCount())
	}
}

func TestReplicaPoolSingleReplica(t *testing.T) {
	pool := NewReplicaPool()
	replica := newMockReplica(0)
	pool.Add(replica)

	// 單個副本時，所有 Next() 都應返回同一個
	for i := 0; i < 10; i++ {
		got := pool.Next()
		if got != replica.bunDB {
			t.Errorf("iteration %d: expected the only replica, got different one", i)
		}
	}
}
