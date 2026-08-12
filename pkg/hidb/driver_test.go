package hidb

import (
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// mockDBConfig 最小 DatabaseConfigInterface 實作
type mockDBConfig struct {
	driver string
}

func (m *mockDBConfig) GetDriver() string                           { return m.driver }
func (m *mockDBConfig) GetDSN() string                              { return "" }
func (m *mockDBConfig) GetMaxIdleConns() int                        { return 0 }
func (m *mockDBConfig) GetMaxOpenConns() int                        { return 0 }
func (m *mockDBConfig) GetRedisConfig() config.RedisConfigInterface { return nil }

// TestRedisRequiresWithRedis 回歸測試：v0.8.11 起 Redis 與 mysql/pg 同規格，
// core 不再依 driver 字串特判自動初始化——driver: "redis" 未傳 WithRedis
// 時應得到與 SQL 路徑同格式的提示錯誤，而非靜默或 panic
func TestRedisRequiresWithRedis(t *testing.T) {
	_, err := NewWithInterface(&mockDBConfig{driver: "redis"})
	if err == nil {
		t.Fatal("driver=redis without WithRedis should error")
	}
	if !strings.Contains(err.Error(), "WithRedis") {
		t.Errorf("error should hint at WithRedis, got: %v", err)
	}
}

// TestEmptyDriverAllowed 無數據庫配置仍可建立（維持既有行為）
func TestEmptyDriverAllowed(t *testing.T) {
	db, err := NewWithInterface(&mockDBConfig{driver: ""})
	if err != nil {
		t.Fatalf("empty driver should be allowed, got: %v", err)
	}
	if db == nil {
		t.Fatal("db should not be nil")
	}
}
