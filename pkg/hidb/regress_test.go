package hidb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/schema"
)

// fakeDriver 依 DSN 模擬資料庫："ok" 正常、"refused" 連線被拒、"hang" ping 卡住
// （直到 ctx 結束，最多 3 秒，避免舊碼在測試中永久卡住）
type fakeDriver struct{}

type fakeConn struct{ hang bool }

var fakeOpened, fakeClosed atomic.Int32

func (fakeDriver) Open(dsn string) (driver.Conn, error) {
	if dsn == "refused" {
		return nil, errors.New("connection refused")
	}
	fakeOpened.Add(1)
	return &fakeConn{hang: dsn == "hang"}, nil
}

func (c *fakeConn) Ping(ctx context.Context) error {
	if !c.hang {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3 * time.Second):
		return errors.New("gave up")
	}
}

func (*fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (*fakeConn) Close() error                        { fakeClosed.Add(1); return nil }
func (*fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }

type fakeDialect struct{}

func (fakeDialect) DriverName() string         { return "hidbfake" }
func (fakeDialect) BunDialect() schema.Dialect { return pgdialect.New() }

func init() { sql.Register("hidbfake", fakeDriver{}) }

// TestReplicaFailureClosesMaster 迴歸測試：副本初始化失敗時主庫連線一併關閉。
// 歷史 bug：主庫已開好，initReplicas 失敗即回 error，呼叫端拿不到實例可 Close，主庫連線池洩漏
func TestReplicaFailureClosesMaster(t *testing.T) {
	opened, closed := fakeOpened.Load(), fakeClosed.Load()
	cfg := &config.DatabaseConfig{Driver: "postgres", DSN: "ok", Replicas: []config.ReplicaConfig{{DSN: "refused"}}}
	if _, err := NewWithInterface(cfg, WithDialect(fakeDialect{})); err == nil {
		t.Fatal("want error from the unreachable replica")
	}
	if o, c := fakeOpened.Load()-opened, fakeClosed.Load()-closed; o != c {
		t.Errorf("opened %d connections, closed %d; master pool leaked", o, c)
	}
}

// TestStartupPingTimesOut 迴歸測試：資料庫不回應時，啟動在 connectTimeout 內失敗。
// 歷史 bug：用不帶期限的 Ping()，卡到作業系統的 TCP 逾時（lib/pq 預設無限等待）
func TestStartupPingTimesOut(t *testing.T) {
	old := connectTimeout
	connectTimeout = 100 * time.Millisecond
	defer func() { connectTimeout = old }()

	for _, cfg := range []*config.DatabaseConfig{
		{Driver: "postgres", DSN: "hang"},
		{Driver: "postgres", DSN: "ok", Replicas: []config.ReplicaConfig{{DSN: "hang"}}},
	} {
		start := time.Now()
		_, err := NewWithInterface(cfg, WithDialect(fakeDialect{}))
		if d := time.Since(start); err == nil || d > time.Second {
			t.Errorf("%+v: err=%v after %v, want a timeout error within ~100ms", cfg, err, d)
		}
	}
}

// TestHealthCheckHonorsContext 迴歸測試：HealthCheck 對副本的 ping 遵守呼叫端的 ctx 期限。
// 歷史 bug：PingAll 用不帶 ctx 的 Ping()，卡住的副本讓 HealthCheck 超過期限
func TestHealthCheckHonorsContext(t *testing.T) {
	db, err := sql.Open("hidbfake", "hang")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := &Database{replicaPool: NewReplicaPool()}
	d.replicaPool.Add(ReadReplica{sqlDB: db})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = d.HealthCheck(ctx)
	if dur := time.Since(start); err == nil || dur > time.Second {
		t.Errorf("HealthCheck returned %v after %v, want an error within the 100ms deadline", err, dur)
	}
}

// TestRedisIsNilWrapped 迴歸測試：被 %w 包過的 redis.Nil 也判斷為 key 不存在
func TestRedisIsNilWrapped(t *testing.T) {
	if !RedisIsNil(fmt.Errorf("load session: %w", redis.Nil)) {
		t.Error("wrapped redis.Nil not recognized")
	}
	if RedisIsNil(errors.New("other")) || RedisIsNil(nil) {
		t.Error("non-Nil errors must not match")
	}
}
