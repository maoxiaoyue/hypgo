package hidb

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/resource"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/schema"
)

// ---- 假的 SQL driver：DSN 就是節點名稱；記錄每個節點收到的查詢，可讓個別節點斷線 ----

type nodeState struct {
	mu      sync.Mutex
	queries map[string][]string
	down    map[string]bool          // 節點斷線：ping 與查詢都回連線層錯誤
	lag     map[string]time.Duration // 節點的複寫延遲
	lagErr  error                    // 量延遲時回的錯誤
	columns []string                 // SELECT 回傳的欄位
	delay   time.Duration            // 每個查詢的耗時
	failing map[string]error         // 節點上的查詢回這個（非連線層的）錯誤
}

var nodes = &nodeState{}

func (n *nodeState) reset() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.queries, n.down, n.lag = map[string][]string{}, map[string]bool{}, map[string]time.Duration{}
	n.failing = map[string]error{}
	n.lagErr, n.columns, n.delay = nil, []string{"id", "name"}, 0
}

func (n *nodeState) set(f func()) { n.mu.Lock(); f(); n.mu.Unlock() }

func (n *nodeState) count(dsn string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.queries[dsn])
}

// connErr 模擬真實 driver 連不上時回的錯誤型別
func connErr(dsn string) error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("%s: connection refused", dsn)}
}

type nodeDriver struct{}
type nodeConn struct{ dsn string }
type nodeRows struct {
	cols []string
	left int
}
type nodeTx struct{}

func (nodeDriver) Open(dsn string) (driver.Conn, error) { return &nodeConn{dsn}, nil }
func (*nodeConn) Prepare(string) (driver.Stmt, error)   { return nil, errors.New("not supported") }
func (*nodeConn) Close() error                          { return nil }
func (*nodeConn) Begin() (driver.Tx, error)             { return nodeTx{}, nil }
func (nodeTx) Commit() error                            { return nil }
func (nodeTx) Rollback() error                          { return nil }

func (c *nodeConn) check(query string) error {
	nodes.mu.Lock()
	defer nodes.mu.Unlock()
	if query != "" && nodes.delay > 0 {
		time.Sleep(nodes.delay)
	}
	if nodes.down[c.dsn] {
		return connErr(c.dsn)
	}
	if query != "" {
		nodes.queries[c.dsn] = append(nodes.queries[c.dsn], query)
		return nodes.failing[c.dsn]
	}
	return nil
}

func (c *nodeConn) Ping(context.Context) error { return c.check("") }

func (c *nodeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := c.check(q); err != nil {
		return nil, err
	}
	nodes.mu.Lock()
	defer nodes.mu.Unlock()
	return &nodeRows{cols: nodes.columns, left: 1}, nil
}

func (c *nodeConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := c.check(q); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

func (r *nodeRows) Columns() []string { return r.cols }
func (r *nodeRows) Close() error      { return nil }
func (r *nodeRows) Next(dest []driver.Value) error {
	if r.left == 0 {
		return io.EOF
	}
	r.left--
	for i := range dest {
		if r.cols[i] == "id" {
			dest[i] = int64(1)
		} else {
			dest[i] = "x"
		}
	}
	return nil
}

func init() { sql.Register("hidbnodes", nodeDriver{}) }

// nodeDialect 也實作 LagProber：延遲由 nodes.lag 控制
type nodeDialect struct{}

func (nodeDialect) DriverName() string         { return "hidbnodes" }
func (nodeDialect) BunDialect() schema.Dialect { return pgdialect.New() }
func (nodeDialect) ReplicaLag(_ context.Context, db *sql.DB) (time.Duration, error) {
	var dsn string
	// 以連線所屬的節點查表：測試裡每個 *sql.DB 只連一個節點
	for name, d := range lagDBs {
		if d == db {
			dsn = name
		}
	}
	nodes.mu.Lock()
	defer nodes.mu.Unlock()
	return nodes.lag[dsn], nodes.lagErr
}

var lagDBs = map[string]*sql.DB{}

// openNodes 建立主庫 "master" 加上指定的副本；日誌寫進回傳的緩衝區
func openNodes(t *testing.T, replicas []string, opts ...Option) (*Database, *bytes.Buffer) {
	t.Helper()
	nodes.reset()
	cfg := &config.DatabaseConfig{Driver: "postgres", DSN: "master"}
	for _, r := range replicas {
		cfg.Replicas = append(cfg.Replicas, config.ReplicaConfig{DSN: r})
	}
	var buf bytes.Buffer
	log, err := logger.New("info", "", &buf, false)
	if err != nil {
		t.Fatal(err)
	}
	db, err := NewWithInterface(cfg, append([]Option{WithDialect(nodeDialect{}), WithLogger(log)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for i, r := range *replicaList(db) {
		lagDBs[replicas[i]] = r.sqlDB
	}
	return db, &buf
}

func replicaList(db *Database) *[]ReadReplica {
	if db.replicaPool == nil {
		return &[]ReadReplica{}
	}
	return db.replicaPool.replicas.Load()
}

type r2User struct {
	bun.BaseModel `bun:"table:users"`
	ID            int64  `bun:"id,pk"`
	Name          string `bun:"name"`
}

func selectUsers(ctx context.Context, db *bun.DB) error {
	var users []r2User
	return db.NewSelect().Model(&users).Scan(ctx)
}

type r2Order struct {
	bun.BaseModel `bun:"table:orders"`
	ID            int64    `bun:"id,pk"`
	Items         []r2Item `bun:"m2m:order_to_items,join:Order=Item"`
}
type r2Item struct {
	bun.BaseModel `bun:"table:items"`
	ID            int64 `bun:"id,pk"`
}
type r2OrderToItem struct {
	bun.BaseModel `bun:"table:order_to_items"`
	OrderID       int64    `bun:"order_id,pk"`
	Order         *r2Order `bun:"rel:belongs-to,join:order_id=id"`
	ItemID        int64    `bun:"item_id,pk"`
	Item          *r2Item  `bun:"rel:belongs-to,join:item_id=id"`
}

// m2m 關聯在主庫註冊一次，走副本的查詢也看得到。
// 歷史 bug：每個副本各有一個 bun 方言實例，ReadHypDB() 的 m2m 查詢 panic
// "can't find m2m order_to_items table (use db.RegisterModel)"
func TestRegisteredModelsAreSharedWithReplicas(t *testing.T) {
	db, _ := openNodes(t, []string{"r1"})
	db.HypDB().RegisterModel((*r2OrderToItem)(nil))
	for name, handle := range map[string]*bun.DB{"read": db.ReadHypDB(), "pool": db.replicaPool.Next()} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s: m2m query panicked: %v", name, p)
				}
			}()
			var orders []r2Order
			// 假 driver 不回關聯資料，查詢本身可能回錯；這裡只要求不因為找不到中介表而 panic
			_ = handle.NewSelect().Model(&orders).Relation("Items").Scan(context.Background())
		}()
	}
}

type countHook struct{ n atomic.Int32 }

func (*countHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context { return ctx }
func (h *countHook) AfterQuery(context.Context, *bun.QueryEvent)                      { h.n.Add(1) }

// 查詢 hook 與資源計數涵蓋主庫與副本上的每一個查詢。
// 歷史 bug：hook 只能掛在主庫的 bun.DB；資源計數算的是取用 ReadHypDB / WriteHypDB 的次數
// （取一次跑 5 個查詢算 1 次，HypDB() 完全不計）
func TestHooksAndCountingCoverEveryQuery(t *testing.T) {
	db, _ := openNodes(t, []string{"r1"})
	hook := &countHook{}
	db.AddQueryHook(hook)
	ctx := context.Background()
	before := resource.Snapshot()

	read := db.ReadHypDB()
	for i := 0; i < 5; i++ {
		if err := selectUsers(ctx, read); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		selectUsers(ctx, db.HypDB())
	}
	db.WriteHypDB().NewInsert().Model(&r2User{ID: 1}).Exec(ctx)

	if got := resource.Snapshot().Sub(before).DB; got != 9 {
		t.Errorf("resource counted %d queries, want 9", got)
	}
	if got := hook.n.Load(); got != 9 {
		t.Errorf("hook saw %d queries, want 9", got)
	}
	if r, m := nodes.count("r1"), nodes.count("master"); r != 5 || m != 4 {
		t.Errorf("replica ran %d queries and master %d, want 5 and 4", r, m)
	}
}

// ReadHypDB：唯讀查詢走副本，寫入與 UseMaster 走主庫。
func TestReadHandleRouting(t *testing.T) {
	db, _ := openNodes(t, []string{"r1"})
	ctx := context.Background()
	read := db.ReadHypDB()
	if read != db.ReadHypDB() || read == db.HypDB() {
		t.Fatal("ReadHypDB should return one stable instance distinct from the master handle")
	}

	selectUsers(ctx, read)
	if nodes.count("r1") != 1 || nodes.count("master") != 0 {
		t.Errorf("select: r1 = %d, master = %d", nodes.count("r1"), nodes.count("master"))
	}
	read.NewInsert().Model(&r2User{ID: 1}).Exec(ctx)
	selectUsers(UseMaster(ctx), read)
	if nodes.count("r1") != 1 || nodes.count("master") != 2 {
		t.Errorf("write + UseMaster: r1 = %d, master = %d, want 1 and 2", nodes.count("r1"), nodes.count("master"))
	}

	// 沒有副本時就是主庫
	single, _ := openNodes(t, nil)
	if single.ReadHypDB() != single.HypDB() {
		t.Error("without replicas ReadHypDB should be the master handle")
	}
}

// 副本連不上：這次查詢改用其他節點，副本立刻移出輪詢；HealthCheck 不因此失敗；恢復後加回。
// 歷史 bug：要等背景探測連續失敗 3 次（約 15 秒）才摘除，期間輪到壞副本的查詢直接失敗；
// 一個副本不通 HealthCheck 就回錯，接到 /health 會讓健康的實例被重啟
func TestReplicaFailoverAndHealth(t *testing.T) {
	db, logs := openNodes(t, []string{"r1", "r2"})
	ctx := context.Background()
	nodes.set(func() { nodes.down["r2"] = true })

	for i := 0; i < 10; i++ {
		if err := selectUsers(ctx, db.ReadHypDB()); err != nil {
			t.Fatalf("read %d failed although another node was available: %v", i, err)
		}
	}
	status := db.ReplicaStatus()
	if len(status) != 2 || !status[0].Healthy || status[1].Healthy || !strings.Contains(status[1].LastError, "refused") {
		t.Errorf("status = %+v", status)
	}
	if !strings.Contains(logs.String(), "removed from rotation") {
		t.Errorf("no log about the removed replica:\n%s", logs)
	}
	if err := db.HealthCheck(ctx); err != nil {
		t.Errorf("HealthCheck with one replica down: %v", err)
	}

	// 全部副本都掛：讀取回退主庫
	nodes.set(func() { nodes.down["r1"] = true })
	before := nodes.count("master")
	for i := 0; i < 4; i++ {
		if err := selectUsers(ctx, db.ReadHypDB()); err != nil {
			t.Fatalf("read with every replica down: %v", err)
		}
	}
	if got := nodes.count("master") - before; got != 4 {
		t.Errorf("master served %d of 4 reads", got)
	}

	// 恢復：探測成功即加回
	nodes.set(func() { nodes.down = map[string]bool{} })
	db.replicaPool.checkAll()
	if s := db.ReplicaStatus(); !s[0].Healthy || !s[1].Healthy || s[1].LastError != "" {
		t.Errorf("after recovery: %+v", s)
	}

	// 主庫不通才算不健康
	nodes.set(func() { nodes.down["master"] = true })
	if err := db.HealthCheck(ctx); err == nil {
		t.Error("HealthCheck should fail when the master is unreachable")
	}
}

// 查詢本身的錯誤與呼叫端取消不算副本故障
func TestOnlyConnectionErrorsEjectReplicas(t *testing.T) {
	db, _ := openNodes(t, []string{"r1"})
	syntax := errors.New("syntax error at or near FORM")
	nodes.set(func() { nodes.failing["r1"] = syntax })
	if err := selectUsers(context.Background(), db.ReadHypDB()); !errors.Is(err, syntax) {
		t.Errorf("a query error should be returned as is, got %v", err)
	}
	if s := db.ReplicaStatus(); !s[0].Healthy || nodes.count("master") != 0 {
		t.Errorf("a query error removed the replica or was retried on the master: %+v, master = %d", s, nodes.count("master"))
	}

	for err, want := range map[error]bool{
		connErr("r1"):                   true,
		driver.ErrBadConn:               true,
		io.ErrUnexpectedEOF:             true,
		errors.New("syntax error"):      false,
		context.Canceled:                false,
		context.DeadlineExceeded:        false,
		fmt.Errorf("x: %w", io.EOF):     true,
		sql.ErrNoRows:                   false,
		fmt.Errorf("pq: duplicate key"): false,
	} {
		if got := isConnError(err); got != want {
			t.Errorf("isConnError(%v) = %v, want %v", err, got, want)
		}
	}
}

// 寫後讀：同一個請求寫入之後的讀取走主庫；時間內同一位使用者的下一個請求也是；其他使用者與過期後走副本。
// 歷史 bug：寫入後立刻 ReadHypDB() 讀到副本，複寫有延遲時讀不到剛寫的資料
func TestReadYourWrites(t *testing.T) {
	db, _ := openNodes(t, []string{"r1"}, WithReadYourWrites(150*time.Millisecond))
	r := router.New()
	r.Use(db.Middleware(func(c *hypcontext.Context) string { return c.GetHeader("X-User") }))
	r.POST("/write-then-read", func(c *hypcontext.Context) {
		selectUsers(c, db.ReadHypDB()) // 寫入前：副本
		db.WriteHypDB().NewInsert().Model(&r2User{ID: 1}).Exec(c)
		selectUsers(c, db.ReadHypDB())                   // 寫入後：主庫
		selectUsers(c.Request.Context(), db.ReadHypDB()) // 用 Request.Context() 也一樣
	})
	r.GET("/read", func(c *hypcontext.Context) { selectUsers(c, db.ReadHypDB()) })
	r.GET("/read-twice", func(c *hypcontext.Context) {
		selectUsers(c, db.ReadHypDB())
		selectUsers(c, db.ReadHypDB())
	})
	call := func(method, path, user string) (master, replica int) {
		m, rp := nodes.count("master"), nodes.count("r1")
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-User", user)
		r.ServeHTTP(httptest.NewRecorder(), req)
		return nodes.count("master") - m, nodes.count("r1") - rp
	}

	// 讀取不算寫入：同一個請求的第二次讀取仍走副本
	if m, rp := call("GET", "/read-twice", "carol"); m != 0 || rp != 2 {
		t.Errorf("two reads: master = %d, replica = %d, want both on the replica", m, rp)
	}
	if m, rp := call("POST", "/write-then-read", "alice"); m != 3 || rp != 1 {
		t.Errorf("write-then-read: master = %d, replica = %d, want 3 and 1", m, rp)
	}
	if m, rp := call("GET", "/read", "alice"); m != 1 || rp != 0 {
		t.Errorf("alice right after her write: master = %d, replica = %d, want the master", m, rp)
	}
	if m, rp := call("GET", "/read", "bob"); m != 0 || rp != 1 {
		t.Errorf("bob: master = %d, replica = %d, want the replica", m, rp)
	}
	time.Sleep(200 * time.Millisecond)
	if m, rp := call("GET", "/read", "alice"); m != 0 || rp != 1 {
		t.Errorf("alice after the window: master = %d, replica = %d, want the replica", m, rp)
	}

	// 關閉跨請求的部分：同一個請求內仍然有效
	off, _ := openNodes(t, []string{"r1"}, WithReadYourWrites(0))
	r2 := router.New()
	r2.Use(off.Middleware())
	r2.POST("/w", func(c *hypcontext.Context) {
		off.WriteHypDB().NewInsert().Model(&r2User{ID: 1}).Exec(c)
		selectUsers(c, off.ReadHypDB())
	})
	r2.GET("/r", func(c *hypcontext.Context) { selectUsers(c, off.ReadHypDB()) })
	r2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/w", nil))
	if nodes.count("master") != 2 || nodes.count("r1") != 0 {
		t.Errorf("same request with the window off: master = %d, replica = %d", nodes.count("master"), nodes.count("r1"))
	}
	r2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/r", nil))
	if nodes.count("r1") != 1 {
		t.Error("with the window off the next request should read from the replica")
	}
}

// 複寫延遲超過上限的副本移出輪詢，追上後加回；量不到延遲時不摘除。
func TestLaggingReplicaIsSkipped(t *testing.T) {
	db, logs := openNodes(t, []string{"r1", "r2"}, WithMaxReplicaLag(time.Second))
	nodes.set(func() { nodes.lag["r2"] = 5 * time.Second })
	db.replicaPool.checkAll()
	s := db.ReplicaStatus()
	if !s[0].Healthy || s[1].Healthy || s[1].Lag != 5*time.Second || !strings.Contains(s[1].LastError, "replication lag") {
		t.Errorf("status = %+v", s)
	}
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		selectUsers(ctx, db.ReadHypDB())
	}
	if nodes.count("r2") != 0 || nodes.count("r1") != 6 {
		t.Errorf("lagging replica served %d reads, healthy one %d", nodes.count("r2"), nodes.count("r1"))
	}

	nodes.set(func() { nodes.lag["r2"] = 200 * time.Millisecond })
	db.replicaPool.checkAll()
	if s := db.ReplicaStatus(); !s[1].Healthy || s[1].Lag != 200*time.Millisecond {
		t.Errorf("after catching up: %+v", s)
	}

	nodes.set(func() { nodes.lagErr = errors.New("access denied; need REPLICATION CLIENT") })
	db.replicaPool.checkAll()
	if s := db.ReplicaStatus(); !s[0].Healthy || !s[1].Healthy {
		t.Errorf("an unmeasurable lag must not remove replicas: %+v", s)
	}
	if !strings.Contains(logs.String(), "cannot measure replication lag") {
		t.Errorf("no log about the failed measurement:\n%s", logs)
	}

	// 沒設定上限時不檢查
	plain, _ := openNodes(t, []string{"r1"})
	nodes.set(func() { nodes.lag["r1"] = time.Hour })
	plain.replicaPool.checkAll()
	if s := plain.ReplicaStatus(); !s[0].Healthy || s[0].Lag != -1 {
		t.Errorf("without max_replica_lag: %+v", s)
	}
}

type fakeDBError struct{ code string }

func (e *fakeDBError) Error() string { return "db error " + e.code }

func init() {
	RegisterErrorClassifier(func(err error) error {
		var fe *fakeDBError
		if !errors.As(err, &fe) {
			return nil
		}
		switch fe.code {
		case "dup":
			return ErrDuplicate
		case "fk":
			return ErrForeignKey
		case "deadlock":
			return ErrDeadlock
		}
		return nil
	})
}

// Classify 把 driver 的錯誤對應到分類，並保留原本的錯誤
func TestClassify(t *testing.T) {
	dup := &fakeDBError{"dup"}
	got := Classify(fmt.Errorf("insert user: %w", dup))
	var fe *fakeDBError
	if !errors.Is(got, ErrDuplicate) || !errors.As(got, &fe) || fe != dup || got.Error() != "insert user: db error dup" {
		t.Errorf("Classify(dup) = %v", got)
	}
	if errors.Is(got, ErrForeignKey) || errors.Is(got, ErrNotFound) {
		t.Error("a duplicate error matched another kind")
	}
	if !errors.Is(Classify(sql.ErrNoRows), ErrNotFound) || !errors.Is(Classify(sql.ErrNoRows), sql.ErrNoRows) {
		t.Error("sql.ErrNoRows should classify as ErrNotFound and stay itself")
	}
	if !errors.Is(Classify(&fakeDBError{"fk"}), ErrForeignKey) || !errors.Is(Classify(&fakeDBError{"deadlock"}), ErrDeadlock) {
		t.Error("fk / deadlock not classified")
	}
	other := errors.New("syntax error")
	if Classify(other) != other || Classify(nil) != nil || Classify(got) != got {
		t.Error("unknown, nil and already classified errors should come back unchanged")
	}
}

// 交易遇到死結時重試（預設 1 次）；其他錯誤不重試。
func TestTransactionRetriesDeadlocks(t *testing.T) {
	deadlock := &fakeDBError{"deadlock"}
	run := func(db *Database, failures int, err error) (calls int, result error) {
		result = db.Transaction(context.Background(), func(*sql.Tx) error {
			calls++
			if calls <= failures {
				return err
			}
			return nil
		})
		return calls, result
	}
	db, _ := openNodes(t, nil)
	if calls, err := run(db, 1, deadlock); calls != 2 || err != nil {
		t.Errorf("one deadlock: %d call(s), err = %v, want 2 calls and success", calls, err)
	}
	if calls, err := run(db, 5, deadlock); calls != 2 || !errors.Is(err, deadlock) {
		t.Errorf("persistent deadlock: %d call(s), err = %v, want 2 calls and the error", calls, err)
	}
	if calls, err := run(db, 1, errors.New("constraint")); calls != 1 || err == nil {
		t.Errorf("other error: %d call(s), err = %v, want no retry", calls, err)
	}

	bunCalls := 0
	err := db.HypDBTransaction(context.Background(), func(context.Context, bun.Tx) error {
		if bunCalls++; bunCalls == 1 {
			return deadlock
		}
		return nil
	})
	if bunCalls != 2 || err != nil {
		t.Errorf("HypDBTransaction: %d call(s), err = %v", bunCalls, err)
	}

	noRetry, _ := openNodes(t, nil, WithTxRetries(0))
	if calls, err := run(noRetry, 1, deadlock); calls != 1 || err == nil {
		t.Errorf("retries off: %d call(s), err = %v", calls, err)
	}
	three, _ := openNodes(t, nil, WithTxRetries(3))
	if calls, err := run(three, 3, deadlock); calls != 4 || err != nil {
		t.Errorf("three retries: %d call(s), err = %v", calls, err)
	}
}

// 查詢結果多出 model 沒有的欄位預設忽略；strict_columns 時回錯。
// 歷史 bug：bun 預設回錯而 hidb 不接受任何 bun 選項——先加欄位再滾動更新的期間，舊版服務的 SELECT * 全部失敗
func TestUnknownColumnsAreIgnoredByDefault(t *testing.T) {
	query := func(db *Database) error {
		var users []r2User
		return db.HypDB().NewRaw("SELECT * FROM users").Scan(context.Background(), &users)
	}
	db, _ := openNodes(t, nil)
	nodes.set(func() { nodes.columns = []string{"id", "name", "added_later"} })
	if err := query(db); err != nil {
		t.Errorf("default: %v", err)
	}

	nodes.reset()
	strict, err := NewWithInterface(&config.DatabaseConfig{Driver: "postgres", DSN: "master", StrictColumns: true}, WithDialect(nodeDialect{}))
	if err != nil {
		t.Fatal(err)
	}
	defer strict.Close()
	nodes.set(func() { nodes.columns = []string{"id", "name", "added_later"} })
	if err := query(strict); err == nil || !strings.Contains(err.Error(), "added_later") {
		t.Errorf("strict_columns: err = %v", err)
	}

	// WithBunOptions 的選項會套用（這裡用它把忽略未知欄位開回來）
	withOpt, err := NewWithInterface(&config.DatabaseConfig{Driver: "postgres", DSN: "master", StrictColumns: true},
		WithDialect(nodeDialect{}), WithBunOptions(bun.WithDiscardUnknownColumns()))
	if err != nil {
		t.Fatal(err)
	}
	defer withOpt.Close()
	if err := query(withOpt); err != nil {
		t.Errorf("WithBunOptions was not applied: %v", err)
	}
}

// 慢查詢寫進日誌；關閉時不寫
func TestSlowQueryLog(t *testing.T) {
	db, logs := openNodes(t, []string{"r1"}, WithSlowQuery(5*time.Millisecond))
	selectUsers(context.Background(), db.ReadHypDB()) // 快的查詢不記
	if strings.Contains(logs.String(), "slow query") {
		t.Fatalf("a fast query was logged: %q", logs)
	}
	nodes.set(func() { nodes.delay = 30 * time.Millisecond })
	selectUsers(context.Background(), db.ReadHypDB())
	if out := logs.String(); !strings.Contains(out, "slow query") || !strings.Contains(out, "operation=SELECT") || !strings.Contains(out, "users") {
		t.Errorf("log = %q", out)
	}
	quiet, qlogs := openNodes(t, nil, WithSlowQuery(0))
	nodes.set(func() { nodes.delay = 30 * time.Millisecond })
	selectUsers(context.Background(), quiet.HypDB())
	if strings.Contains(qlogs.String(), "slow query") {
		t.Errorf("slow query logged although it is disabled: %q", qlogs)
	}
	if got := truncateSQL(strings.Repeat("x", maxLoggedSQL*2)); len(got) > maxLoggedSQL+len("…") {
		t.Errorf("logged SQL is %d bytes", len(got))
	}
}

// 行為設定：設定檔的 0 是沒設定（用預設值）、負數是關閉；Option 優先
func TestSettingsResolution(t *testing.T) {
	resolve := func(cfg *config.DatabaseConfig, opts ...Option) settings {
		d := &Database{config: cfg}
		for _, o := range opts {
			o(d)
		}
		d.resolveSettings()
		return d.set
	}
	def := resolve(&config.DatabaseConfig{})
	if def.slowQuery != 500*time.Millisecond || def.readYourWrites != time.Second || def.txRetries != 1 ||
		def.maxReplicaLag != 0 || def.connMaxLifetime != 30*time.Minute || def.connMaxIdleTime != 0 || def.strictColumns {
		t.Errorf("defaults = %+v", def)
	}
	off := resolve(&config.DatabaseConfig{SlowQuery: -1, ReadYourWrites: -1, TxRetries: -1, ConnMaxLifetime: -1})
	if off.slowQuery != 0 || off.readYourWrites != 0 || off.txRetries != 0 || off.connMaxLifetime != 0 {
		t.Errorf("disabled = %+v", off)
	}
	custom := resolve(&config.DatabaseConfig{SlowQuery: 200, ReadYourWrites: 300, MaxReplicaLag: 2000, TxRetries: 4, ConnMaxLifetime: 60, ConnMaxIdleTime: 30})
	if custom.slowQuery != 200*time.Millisecond || custom.readYourWrites != 300*time.Millisecond || custom.maxReplicaLag != 2*time.Second ||
		custom.txRetries != 4 || custom.connMaxLifetime != time.Minute || custom.connMaxIdleTime != 30*time.Second {
		t.Errorf("custom = %+v", custom)
	}
	over := resolve(&config.DatabaseConfig{SlowQuery: 200, TxRetries: 4}, WithSlowQuery(time.Second), WithTxRetries(0), WithMaxReplicaLag(-time.Second))
	if over.slowQuery != time.Second || over.txRetries != 0 || over.maxReplicaLag != 0 {
		t.Errorf("options should win: %+v", over)
	}
}

// 其餘的設定與狀態檢查
func TestConstructionAndConnectivityChecks(t *testing.T) {
	// driver 空而給了 WithDialect：先前回傳沒有連線的實例，HypDB() 是 nil
	if _, err := NewWithInterface(&config.DatabaseConfig{DSN: "master"}, WithDialect(nodeDialect{})); err == nil {
		t.Error("an empty driver with WithDialect should be an error")
	}

	// 副本沒設定連線數上限時沿用主庫的（先前不設上限）
	db, _ := openNodes(t, []string{"r1"})
	if got := (*replicaList(db))[0].sqlDB.Stats().MaxOpenConnections; got != db.SQL().Stats().MaxOpenConnections || got == 0 {
		t.Errorf("replica MaxOpenConnections = %d, master = %d", got, db.SQL().Stats().MaxOpenConnections)
	}

	// 主庫不回應時 IsConnected 在期限內返回（先前永遠不返回）
	old := connectTimeout
	connectTimeout = 100 * time.Millisecond
	defer func() { connectTimeout = old }()
	hang, err := sql.Open("hidbfake", "hang")
	if err != nil {
		t.Fatal(err)
	}
	defer hang.Close()
	done := make(chan bool, 1)
	go func() { done <- (&Database{sqlDB: hang}).IsConnected() }()
	select {
	case ok := <-done:
		if ok {
			t.Error("IsConnected = true for a master that does not answer")
		}
	case <-time.After(2 * time.Second):
		t.Error("IsConnected did not return")
	}
}
