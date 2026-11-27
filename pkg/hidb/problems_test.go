package hidb

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// 測試用的「驅動自己的連線錯誤」（相當於 mysql.ErrInvalidConn）
var errDriverInvalidConn = errors.New("invalid connection")

func (nodeDialect) IsConnError(err error) bool { return errors.Is(err, errDriverInvalidConn) }

// ReadHypDB 的原始 SELECT 走副本，而且不算寫入。
// 歷史 bug：bun.IsReadOnlyQuery 只認 NewSelect，NewRaw 的 SELECT 走主庫，還觸發寫後讀，
// 同一位使用者接下來的讀取全部改走主庫
func TestRawSelectIsARead(t *testing.T) {
	db, _ := openNodes(t, []string{"r1"})
	r := router.New()
	r.Use(db.Middleware(func(c *hypcontext.Context) string { return "u1" }))
	var rows []r2User
	r.GET("/raw", func(c *hypcontext.Context) {
		if err := db.ReadHypDB().NewRaw("SELECT id, name FROM users WHERE name = ?", "amy").Scan(c, &rows); err != nil {
			t.Errorf("raw select: %v", err)
		}
		selectUsers(c, db.ReadHypDB())
	})
	r.GET("/read", func(c *hypcontext.Context) { selectUsers(c, db.ReadHypDB()) })
	r.GET("/lock", func(c *hypcontext.Context) {
		db.ReadHypDB().NewRaw("SELECT id, name FROM users WHERE id = 1 FOR UPDATE").Scan(c, &rows)
	})
	r.GET("/rawwrite", func(c *hypcontext.Context) {
		db.ReadHypDB().NewRaw("UPDATE users SET name = ?", "x").Exec(c)
		selectUsers(c, db.ReadHypDB())
	})
	call := func(path string) (master, replica int) {
		m, rp := nodes.count("master"), nodes.count("r1")
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
		return nodes.count("master") - m, nodes.count("r1") - rp
	}
	if m, rp := call("/raw"); m != 0 || rp != 2 {
		t.Errorf("raw SELECT then NewSelect: master = %d, replica = %d, want both on the replica", m, rp)
	}
	if m, rp := call("/read"); m != 0 || rp != 1 {
		t.Errorf("next request: master = %d, replica = %d; a raw SELECT was taken for a write", m, rp)
	}
	if m, rp := call("/lock"); m != 1 || rp != 0 {
		t.Errorf("SELECT ... FOR UPDATE: master = %d, replica = %d, want the master", m, rp)
	}
	if m, rp := call("/rawwrite"); m != 2 || rp != 0 {
		t.Errorf("raw UPDATE then read: master = %d, replica = %d, want 2 and 0", m, rp)
	}

	for sql, want := range map[string]bool{
		"SELECT 1":                               true,
		"  select * from t":                      true,
		"(SELECT 1) UNION (SELECT 2)":            true,
		"SHOW TABLES":                            true,
		"EXPLAIN SELECT 1":                       true,
		"SELECT * FROM t FOR SHARE":              false,
		"select * from t for update skip locked": false,
		"WITH x AS (DELETE FROM t RETURNING 1) SELECT * FROM x": false,
		"INSERT INTO t VALUES (1)":                              false,
		"SELECTED":                                              false,
		"":                                                      false,
	} {
		if got := readOnlySQL(sql); got != want {
			t.Errorf("readOnlySQL(%q) = %v, want %v", sql, got, want)
		}
	}
}

// 驅動的撥號逾時、驅動自己的連線錯誤都要即時摘除副本；呼叫端取消或逾時不算。
// 歷史 bug：凡是符合 context.DeadlineExceeded 的錯誤都被濾掉，連不上主機（dial tcp: i/o timeout）
// 要等背景探測約 15 秒；mysql.ErrInvalidConn 不被當成連線錯誤
func TestFailoverCatchesDialTimeoutAndDriverErrors(t *testing.T) {
	dialTimeout := fmt.Errorf("dial tcp 10.0.0.9:3306: %w", context.DeadlineExceeded)
	for name, failure := range map[string]error{"dial timeout": dialTimeout, "driver invalid connection": errDriverInvalidConn} {
		db, _ := openNodes(t, []string{"r1"})
		nodes.set(func() { nodes.failing["r1"] = failure })
		if err := selectUsers(context.Background(), db.ReadHypDB()); err != nil {
			t.Errorf("%s: the query was not retried on the master: %v", name, err)
		}
		if s := db.ReplicaStatus(); s[0].Healthy {
			t.Errorf("%s: the replica stayed in rotation", name)
		}
	}

	// 呼叫端的 ctx 已經取消：不是副本的問題
	db, _ := openNodes(t, []string{"r1"})
	ctx, cancel := context.WithCancel(context.Background())
	c := &failoverConn{d: db, replica: &(*replicaList(db))[0]}
	cancel()
	if c.failed(ctx, dialTimeout) || !db.ReplicaStatus()[0].Healthy {
		t.Error("a cancelled caller removed the replica")
	}
}

// 複寫停止的副本移出輪詢；量不到延遲的原因每個副本各記一次。
// 歷史 bug：MySQL 的 Seconds_Behind_Source 為 NULL 被當成「量不到」，副本留在輪詢裡；
// 量不到的原因整個 pool 只記一次
func TestStoppedReplicationRemovesReplica(t *testing.T) {
	db, logs := openNodes(t, []string{"r1", "r2"}, WithMaxReplicaLag(time.Second))
	nodes.set(func() { nodes.lagErr = fmt.Errorf("%w (Seconds_Behind_Source is NULL)", ErrReplicationStopped) })
	db.replicaPool.checkAll()
	for _, s := range db.ReplicaStatus() {
		if s.Healthy || !strings.Contains(s.LastError, "replication is not running") {
			t.Errorf("replica with stopped replication: %+v", s)
		}
	}
	nodes.set(func() { nodes.lagErr = nil })
	db.replicaPool.checkAll()
	for _, s := range db.ReplicaStatus() {
		if !s.Healthy {
			t.Errorf("replica did not come back: %+v", s)
		}
	}

	logs.Reset()
	nodes.set(func() { nodes.lagErr = errors.New("access denied") })
	db.replicaPool.checkAll()
	db.replicaPool.checkAll()
	if got := strings.Count(logs.String(), "cannot measure replication lag"); got != 2 {
		t.Errorf("%d log lines for 2 replicas over 2 rounds, want one per replica:\n%s", got, logs)
	}
}

// 沒有登入的請求只做同一個請求內的寫後讀。
// 歷史 bug：以客戶端 IP 當鍵，負載平衡器後的匿名訪客共用一個鍵，一人寫入後其他人的讀取全走主庫
func TestAnonymousRequestsDoNotShareReadYourWrites(t *testing.T) {
	db, _ := openNodes(t, []string{"r1"})
	r := router.New()
	r.Use(db.Middleware())
	r.POST("/w", func(c *hypcontext.Context) {
		db.WriteHypDB().NewInsert().Model(&r2User{ID: 1}).Exec(c)
		selectUsers(c, db.ReadHypDB()) // 同一個請求：主庫
	})
	r.GET("/r", func(c *hypcontext.Context) { selectUsers(c, db.ReadHypDB()) })
	send := func(method, path string) {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "10.0.0.5:4000" // 同一個負載平衡器
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	send("POST", "/w")
	if nodes.count("master") != 2 || nodes.count("r1") != 0 {
		t.Errorf("same request: master = %d, replica = %d", nodes.count("master"), nodes.count("r1"))
	}
	send("GET", "/r")
	if nodes.count("r1") != 1 {
		t.Error("another anonymous visitor behind the same address was sent to the master")
	}
}

// handler 返回後才執行的查詢不再碰 Context（它已經回到池子）
func TestSessionKeyIsFixedWhenTheHandlerReturns(t *testing.T) {
	db, _ := openNodes(t, []string{"r1"})
	calls := 0
	var ctx context.Context
	r := router.New()
	r.Use(db.Middleware(func(c *hypcontext.Context) string { calls++; return c.GetHeader("X-User") }))
	r.GET("/", func(c *hypcontext.Context) { ctx = c.Request.Context() })
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User", "amy")
	r.ServeHTTP(httptest.NewRecorder(), req)
	if calls != 1 {
		t.Fatalf("key computed %d times by the time the handler returned, want 1", calls)
	}
	db.WriteHypDB().NewInsert().Model(&r2User{ID: 1}).Exec(ctx) // handler 返回後（goroutine 裡）的寫入
	if calls != 1 {
		t.Error("a query after the handler returned asked the pooled Context for the key")
	}
	if _, ok := db.recent.m.Load("amy"); !ok {
		t.Error("the late write was not recorded under the request's key")
	}
}

// 慢查詢日誌預設不帶 SQL（內嵌參數值）。歷史 bug：INSERT 的密碼、token 原樣寫進日誌
func TestSlowQueryLogHasNoValuesByDefault(t *testing.T) {
	db, logs := openNodes(t, nil, WithSlowQuery(time.Millisecond))
	nodes.set(func() { nodes.delay = 10 * time.Millisecond })
	db.HypDB().NewInsert().Model(&r2User{ID: 1, Name: "hunter2-secret-token"}).Exec(context.Background())
	out := logs.String()
	if !strings.Contains(out, "slow query") || !strings.Contains(out, "operation=INSERT") || !strings.Contains(out, "table=users") {
		t.Errorf("log = %q", out)
	}
	if strings.Contains(out, "hunter2") {
		t.Errorf("the slow query log has parameter values: %q", out)
	}

	db, logs = openNodes(t, nil, WithSlowQuery(time.Millisecond), WithSlowQuerySQL(true))
	nodes.set(func() { nodes.delay = 10 * time.Millisecond })
	db.HypDB().NewInsert().Model(&r2User{ID: 1, Name: "hunter2-secret-token"}).Exec(context.Background())
	if !strings.Contains(logs.String(), "hunter2") {
		t.Errorf("slow_query_sql is on but the SQL is missing: %q", logs)
	}
}
