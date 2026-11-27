package hidb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strings"
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
	// BEGIN / COMMIT / ROLLBACK / SAVEPOINT 不是應用程式寫的查詢：不計數，也不算寫入。
	// 先前交易裡 2 個 SELECT 計成 4 次，唯讀交易結束後還讓該使用者的讀取改走主庫
	control := isTxControl(e)
	if !control {
		resource.MarkDB()
	}
	d := h.d
	if took := time.Since(e.StartTime); d.set.slowQuery > 0 && took >= d.set.slowQuery {
		// SQL 內嵌著參數值（bun 送出前已把值填進去）：預設只記操作與表名，
		// 先前整句寫進日誌，密碼、token、個資都在裡面。slow_query_sql: true 才記 SQL
		fields := []interface{}{"ms", took.Milliseconds(), "operation", e.Operation()}
		if e.IQuery != nil {
			if table := e.IQuery.GetTableName(); table != "" {
				fields = append(fields, "table", table)
			}
		}
		if d.set.slowQuerySQL {
			fields = append(fields, "query", truncateSQL(e.Query))
		}
		d.log.Warning("slow query", append(fields, "err", errText(e.Err))...)
	}
	if e.Err == nil && !control && !readOnly(e.IQuery, e.Query) {
		d.markWrite(ctx)
	}
}

// readOnly 回報查詢是否唯讀。bun.IsReadOnlyQuery 只認 NewSelect：NewRaw("SELECT ...") 與
// db.QueryContext("SELECT ...") 都不算，先前因此不走副本，還被當成寫入（觸發寫後讀）
func readOnly(q bun.Query, sql string) bool {
	switch q := q.(type) {
	case nil:
		return readOnlySQL(sql)
	case *bun.RawQuery:
		if sql == "" {
			sql = q.String()
		}
		return readOnlySQL(sql)
	}
	return bun.IsReadOnlyQuery(q)
}

// readOnlySQL 以開頭的關鍵字判斷原始 SQL 是否唯讀。看不出來的一律當成寫入（走主庫）：
// WITH 可以帶 INSERT / UPDATE，SELECT ... FOR UPDATE / FOR SHARE 要在主庫上鎖
func readOnlySQL(sql string) bool {
	sql = strings.TrimLeft(sql, " \t\r\n(")
	end := strings.IndexAny(sql, " \t\r\n(")
	if end < 0 {
		end = len(sql)
	}
	switch strings.ToUpper(sql[:end]) {
	case "SHOW", "EXPLAIN", "DESCRIBE", "DESC":
		return true
	case "SELECT":
		upper := strings.ToUpper(sql)
		return !strings.Contains(upper, "FOR UPDATE") && !strings.Contains(upper, "FOR SHARE") &&
			!strings.Contains(upper, "FOR NO KEY UPDATE") && !strings.Contains(upper, "FOR KEY SHARE")
	}
	return false
}

// isTxControl 回報事件是否為 bun 自己送出的交易控制語句（沒有對應的 query 物件）
func isTxControl(e *bun.QueryEvent) bool {
	if e.IQuery != nil {
		return false
	}
	switch strings.ToUpper(e.Operation()) {
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE":
		return true
	}
	return false
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
	if d.replicaPool == nil || !readOnly(query, "") || d.needsMaster(ctx) {
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

func (c *failoverConn) failed(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false // 呼叫端取消或逾時：不是副本的問題
	}
	if ce, ok := c.d.dialect.(ConnErrorChecker); !isConnError(err) && !(ok && ce.IsConnError(err)) {
		return false
	}
	if c.replica.health.markDown(err) {
		c.d.log.Warning("read replica removed from rotation", "replica", c.replica.index, "err", err.Error())
	}
	return true
}

func (c *failoverConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := c.replica.sqlDB.QueryContext(ctx, query, args...)
	if err != nil && c.failed(ctx, err) {
		return c.fallback().QueryContext(ctx, query, args...)
	}
	return rows, err
}

func (c *failoverConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := c.replica.sqlDB.ExecContext(ctx, query, args...)
	if err != nil && c.failed(ctx, err) {
		return c.fallback().ExecContext(ctx, query, args...)
	}
	return res, err
}

func (c *failoverConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.replica.sqlDB.QueryRowContext(ctx, query, args...) // *sql.Row 的錯誤要到 Scan 才知道，無法重試
}

// ConnErrorChecker 由方言選擇性實作：辨認驅動自己的連線層錯誤
// （go-sql-driver 查詢中斷時回 mysql.ErrInvalidConn，既不是 driver.ErrBadConn 也不是 net.Error）
type ConnErrorChecker interface {
	IsConnError(err error) bool
}

// isConnError 回報 err 是否為連線層的錯誤（連不上、連線中斷），而不是查詢本身的錯誤。
// 呼叫端取消或逾時由 failed 以查詢的 ctx 判斷，不看 err：驅動以自己的逾時撥號
// （go-sql-driver 的 timeout=、lib/pq 的 connect_timeout），連不上主機時的 "dial tcp: i/o timeout"
// 也符合 context.DeadlineExceeded。先前只要符合就不算連線錯誤，最常見的故障反而要等背景探測（約 15 秒）
func isConnError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	return errors.Is(err, driver.ErrBadConn) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.As(err, &netErr)
}
