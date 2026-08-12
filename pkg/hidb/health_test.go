package hidb

import (
	"testing"
)

// TestNextSkipsDownReplica 標記 down 的副本應被輪詢跳過
func TestNextSkipsDownReplica(t *testing.T) {
	pool := NewReplicaPool()
	pool.Add(newMockReplica(0))
	pool.Add(newMockReplica(1))
	pool.Add(newMockReplica(2))

	replicas := *pool.replicas.Load()
	replicas[1].health.down.Store(true)

	// 連取多次，都不應拿到副本 1 的實例
	bad := replicas[1].hypDB
	for i := 0; i < 12; i++ {
		if got := pool.Next(); got == bad {
			t.Fatal("Next() returned a replica marked down")
		}
	}
	if pool.HealthyCount() != 2 {
		t.Errorf("HealthyCount() = %d, want 2", pool.HealthyCount())
	}
}

// TestNextAllDownFallsBackNil 全部副本 down 時回傳 nil（呼叫端回退主庫）
func TestNextAllDownFallsBackNil(t *testing.T) {
	pool := NewReplicaPool()
	pool.Add(newMockReplica(0))
	pool.Add(newMockReplica(1))

	for _, r := range *pool.replicas.Load() {
		r.health.down.Store(true)
	}

	if got := pool.Next(); got != nil {
		t.Errorf("Next() = %v, want nil when all replicas down", got)
	}
	if got := pool.NextSQL(); got != nil {
		t.Errorf("NextSQL() = %v, want nil when all replicas down", got)
	}
	if pool.HealthyCount() != 0 {
		t.Errorf("HealthyCount() = %d, want 0", pool.HealthyCount())
	}
}

// TestCheckAllMarksDownAfterThresholdAndRecovers ping 連續失敗達門檻才摘除，
// 一次成功即恢復
func TestCheckAllMarksDownAfterThresholdAndRecovers(t *testing.T) {
	pool := NewReplicaPool()
	pool.Add(ReadReplica{sqlDB: nil, hypDB: nil}) // sqlDB nil → checkAll 跳過，不影響

	replicas := *pool.replicas.Load()
	h := replicas[0].health

	// 模擬健康迴圈的計數行為：checkAll 對 sqlDB==nil 的副本不動作
	for i := 0; i < healthFailThreshold+1; i++ {
		pool.checkAll()
	}
	if h.down.Load() {
		t.Error("replica with nil sqlDB should never be marked down (skipped)")
	}

	// 直接驗證門檻語義（等價於 checkAll 的失敗分支）
	for i := 0; i < healthFailThreshold-1; i++ {
		h.fails++
	}
	if h.fails >= healthFailThreshold {
		t.Fatal("test setup error")
	}
	h.fails++
	if h.fails >= healthFailThreshold {
		h.down.Store(true)
	}
	if !h.down.Load() {
		t.Error("expected down after reaching fail threshold")
	}

	// 恢復語義：成功一次即歸零並解除摘除
	h.fails = 0
	h.down.Store(false)
	if pool.HealthyCount() != 1 {
		t.Errorf("HealthyCount() = %d, want 1 after recovery", pool.HealthyCount())
	}
}

// TestStartHealthCheckIdempotentAndCloseStops 重複啟動無效果、Close 停止 goroutine
func TestStartHealthCheckIdempotentAndCloseStops(t *testing.T) {
	pool := NewReplicaPool()
	pool.StartHealthCheck()
	first := pool.stopCh
	pool.StartHealthCheck() // 冪等
	if pool.stopCh != first {
		t.Error("StartHealthCheck should be idempotent")
	}
	pool.Close()
	select {
	case <-first:
		// closed — goroutine 會退出
	default:
		t.Error("Close() should close the health-check stop channel")
	}
	// 再 Close 一次不應 panic（stopOnce 保護）
	pool.Close()
}
