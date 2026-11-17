package hidb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/resource"
	"github.com/uptrace/bun"
)

// ---- 查詢 hook：計數、慢查詢、寫入標記 ----

// queryHook 掛在主庫與讀取用的 bun.DB 上。
// 先前資源計數算的是「取用 ReadHypDB / WriteHypDB 的次數」：取一次跑 5 個查詢算 1 次，
// HypDB() 完全不計，N+1 查詢看起來和 1 次查詢一樣
type queryHook struct{ d *Database }

func (queryHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context { return ctx }

func (h queryHook) AfterQuery(ctx context.Context, e *bun.QueryEvent) {
	resource.MarkDB()
	d := h.d
	if took := time.Since(e.StartTime); d.set.slowQuery > 0 && took >= d.set.slowQuery {
		d.log.Warning("slow query",
			"ms", took.Milliseconds(), "operation", e.Operation(), "query", truncateSQL(e.Query), "err", errText(e.Err))
	}
	if e.Err == nil && !bun.IsReadOnlyQuery(e.IQuery) {
		d.markWrite(ctx)
	}
}

// maxLoggedSQL 慢查詢日誌裡 SQL 的長度上限（批次 INSERT 可能有數 MB）
const maxLoggedSQL = 1000

func truncateSQL(q string) string {
	if len(q) <= maxLoggedSQL {
		return q
	}
	return q[:maxLoggedSQL] + "…"
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// AddQueryHook 把 hook 掛到主庫與讀取用的連線上（日誌、追蹤、指標）。
// 先前只能 db.HypDB().AddQueryHook(h)：走副本的讀取完全不經過它
func (d *Database) AddQueryHook(hook bun.QueryHook) {
	if d.hypDB != nil {
		d.hypDB.AddQueryHook(hook)
	}
	if d.readDB != nil && d.readDB != d.hypDB {
		d.readDB.AddQueryHook(hook)
	}
	if d.replicaPool != nil { // 直接經 ReplicaPool.Next() 取用的副本實例
		for _, r := range *d.replicaPool.replicas.Load() {
			if r.hypDB != nil {
				r.hypDB.AddQueryHook(hook)
			}
		}
	}
}

// ---- 讀取用連線的選擇 ----

// readResolver 為 ReadHypDB 回傳的 bun.DB 選擇每個查詢要用的連線（bun.ConnResolver）：
// 唯讀查詢輪詢健康的副本；其餘（寫入、需要主庫的讀取、沒有健康的副本）回 nil，bun 改用主庫
type readResolver struct{ d *Database }

func (r readResolver) ResolveConn(ctx context.Context, query bun.Query) bun.IConn {
	d := r.d
	if d.replicaPool == nil || !bun.IsReadOnlyQuery(query) || d.needsMaster(ctx) {
		return nil
	}
	replica := d.replicaPool.next()
	if replica == nil {
		return nil
	}
	return &failoverConn{d: d, replica: replica}
}

// Close 不做事：副本由 ReplicaPool 關閉
func (readResolver) Close() error { return nil }

// failoverConn 在副本上執行查詢；遇到連線層的錯誤時立刻把該副本標成不健康，
// 並改用下一個健康的副本（沒有時用主庫）重試一次。
// 先前要等背景探測連續失敗 3 次（約 15 秒）才摘除，期間輪到壞副本的查詢直接失敗
type failoverConn struct {
	d       *Database
	replica *ReadReplica
}

// fallback 回傳重試用的連線
func (c *failoverConn) fallback() *sql.DB {
	if next := c.d.replicaPool.next(); next != nil && next != c.replica && next.sqlDB != nil {
		return next.sqlDB
	}
	return c.d.sqlDB
}

func (c *failoverConn) failed(err error) bool {
	if !isConnError(err) {
		return false
	}
	if c.replica.health.markDown(err) {
		c.d.log.Warning("read replica removed from rotation", "replica", c.replica.index, "err", err.Error())
	}
	return true
}

func (c *failoverConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := c.replica.sqlDB.QueryContext(ctx, query, args...)
	if err != nil && c.failed(err) {
		return c.fallback().QueryContext(ctx, query, args...)
	}
	return rows, err
}

func (c *failoverConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := c.replica.sqlDB.ExecContext(ctx, query, args...)
	if err != nil && c.failed(err) {
		return c.fallback().ExecContext(ctx, query, args...)
	}
	return res, err
}

func (c *failoverConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.replica.sqlDB.QueryRowContext(ctx, query, args...) // *sql.Row 的錯誤要到 Scan 才知道，無法重試
}

// isConnError 回報 err 是否為連線層的錯誤（連不上、連線中斷），而不是查詢本身的錯誤。
// 呼叫端取消或逾時不算：那不是副本的問題
func isConnError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	return errors.Is(err, driver.ErrBadConn) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.As(err, &netErr)
}
