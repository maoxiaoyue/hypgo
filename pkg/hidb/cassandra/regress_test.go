package cassandra

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// noPanic runs fn and reports a panic as a test failure instead of crashing the binary.
func noPanic(t *testing.T, name string, fn func() error) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", name, r)
			err = fmt.Errorf("panic")
		}
	}()
	return fn()
}

type regressUser struct {
	ID   int    `cql:"id,pk"`
	Name string `cql:"name,omitempty"`
}

// TestCountRendersExpression 迴歸測試：COUNT(*) 不被當成欄位名加引號。
// 歷史 bug：Count() 送出 SELECT "COUNT(*)" FROM ...，Cassandra 回 Undefined column name
func TestCountRendersExpression(t *testing.T) {
	stmt, _ := (&CassandraDB{}).Select("t", "COUNT(*)").WhereEq("id", 1).CQL()
	if want := "SELECT COUNT(*) FROM t WHERE id = ?"; stmt != want {
		t.Errorf("got %q, want %q", stmt, want)
	}
}

// TestTLSInsecureSkipVerify 迴歸測試：insecure_skip_verify 搭配 server_name 或自帶 tls.Config 時仍生效。
// 歷史 bug：gocql 有 Config 時只看 Config.InsecureSkipVerify，server_name 建的 Config 沒帶這個旗標；
// 自帶 Config 的 InsecureSkipVerify 又被 EnableHostVerification=true 蓋回驗證
func TestTLSInsecureSkipVerify(t *testing.T) {
	opts, err := buildSslOptions(TLSConfig{ServerName: "db.local", InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Config == nil || !opts.Config.InsecureSkipVerify || opts.Config.ServerName != "db.local" {
		t.Errorf("server_name config = %+v, want ServerName db.local with InsecureSkipVerify", opts.Config)
	}

	opts, err = buildSslOptions(TLSConfig{Config: &tls.Config{InsecureSkipVerify: true}})
	if err != nil {
		t.Fatal(err)
	}
	if opts.EnableHostVerification {
		t.Error("supplied tls.Config with InsecureSkipVerify must not be forced back to verifying")
	}
}

// TestUnconnectedReturnsError 迴歸測試：未連線（NewWithoutConnect）或已 Close 的實例，
// 所有查詢路徑都回 ErrNotConnected 而非 panic。歷史 bug：多數 builder 直接解參考 nil session
func TestUnconnectedReturnsError(t *testing.T) {
	ctx := context.Background()
	fresh, err := NewWithoutConnect(Config{Hosts: []string{"127.0.0.1"}})
	if err != nil {
		t.Fatal(err)
	}
	closed, _ := NewWithoutConnect(Config{Hosts: []string{"127.0.0.1"}})
	closed.Close()

	for _, db := range []*CassandraDB{fresh, closed} {
		var u regressUser
		var users []regressUser
		ops := map[string]func() error{
			"Select.All":   func() error { return db.Select("t").All(ctx, &users) },
			"Select.One":   func() error { return db.Select("t").One(ctx, &u) },
			"Select.Count": func() error { _, err := db.Select("t").Count(ctx); return err },
			"Insert.Exec":  func() error { return db.Insert("t").Value("id", 1).Exec(ctx) },
			"Update.Exec":  func() error { return db.Update("t").Set("name", "x").WhereEq("id", 1).Exec(ctx) },
			"Delete.Exec":  func() error { return db.Delete("t").WhereEq("id", 1).Exec(ctx) },
			"Batch.Exec":   func() error { return db.NewLoggedBatch().AddRaw("SELECT 1").Exec(ctx) },
			"Exec":         func() error { return db.Exec(ctx, "SELECT 1") },
			"ExecScript":   func() error { return db.ExecScript(ctx, "SELECT 1; SELECT 2") },
			"Save":         func() error { return db.Save(ctx, &regressUser{ID: 1}) },
			"UpdateModel":  func() error { return db.UpdateModel(ctx, &regressUser{ID: 1, Name: "x"}) },
			"DeleteModel":  func() error { return db.DeleteModel(ctx, &regressUser{ID: 1}) },
			"Migrator.Up":  func() error { return NewMigrator(db, "", "").Up(ctx) },
			"Introspect":   func() error { _, err := db.Introspect(ctx, IntrospectOptions{}); return err },
			"DescribeTable": func() error {
				_, _, err := db.DescribeTable(ctx, "ks", "t")
				return err
			},
			"ThreadPools": func() error { _, err := db.ThreadPools(ctx); return err },
			"Ping":        func() error { return db.Ping(ctx) },
		}
		for name, op := range ops {
			if err := noPanic(t, name, op); !errors.Is(err, ErrNotConnected) {
				t.Errorf("%s: err = %v, want ErrNotConnected", name, err)
			}
		}
	}
}

type mixedPositions struct {
	A int `cql:"a,pk,position=2"`
	B int `cql:"b,pk"`
	C int `cql:"c,pk,position=1"`
}

type allPositions struct {
	A int `cql:"a,pk,position=2"`
	C int `cql:"c,pk,position=1"`
}

// TestModelKeyPositions 迴歸測試：主鍵欄位的 position 全設時依序排列，部分設定時回錯誤。
// 歷史 bug：比較函式把 0 視為與任何值都不可比，A(2)、B(-)、C(1) 排出 [a b c]
func TestModelKeyPositions(t *testing.T) {
	if info, err := ParseModel(mixedPositions{}); err == nil {
		t.Errorf("mixing positioned and unpositioned keys should fail, got %v", info.PartitionKey)
	}
	info, err := ParseModel(allPositions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info.PartitionKey, []string{"c", "a"}) {
		t.Errorf("PartitionKey = %v, want [c a]", info.PartitionKey)
	}
}

// TestSplitStatementsComments 迴歸測試：註解內的引號與分號不影響拆分。
// 歷史 bug：註解裡的 ' 開啟字串，之後所有分號都被吞掉
func TestSplitStatementsComments(t *testing.T) {
	script := `-- don't split here; really
CREATE TABLE a (id int PRIMARY KEY);
/* it's a block; comment */ CREATE TABLE b (id int PRIMARY KEY);
INSERT INTO "Mixed;Case" (id, v) VALUES (1, 'semi;colon''s') ; // trailing; note
`
	got := splitStatements(script)
	want := []string{
		"CREATE TABLE a (id int PRIMARY KEY)",
		"CREATE TABLE b (id int PRIMARY KEY)",
		`INSERT INTO "Mixed;Case" (id, v) VALUES (1, 'semi;colon''s')`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestDefaultCompactionIsServerDefault 迴歸測試：預設 compaction 為 SizeTiered（4.x / 5.0 的伺服器預設）。
// 歷史 bug：預設 UnifiedCompactionStrategy，4.x 上建表失敗、5.0 上改變行為
func TestDefaultCompactionIsServerDefault(t *testing.T) {
	got := (&CassandraDB{}).Table("t").Column("id", TypeInt).PartitionKey("id").CreateCQL()
	if strings.Contains(got, CompactionUnified) || !strings.Contains(got, CompactionSTCS) {
		t.Errorf("default compaction should be SizeTiered:\n%s", got)
	}
}

// TestOneDoesNotMutateBuilder 迴歸測試：One 的 LIMIT 1 不寫回原 builder。
// 歷史 bug：One 呼叫 s.Limit(1)，同一個 builder 之後 All 只拿到一筆
func TestOneDoesNotMutateBuilder(t *testing.T) {
	db, _ := NewWithoutConnect(Config{Hosts: []string{"127.0.0.1"}})
	q := db.Select("t")
	var u regressUser
	noPanic(t, "One", func() error { return q.One(context.Background(), &u) })
	if stmt, _ := q.CQL(); strings.Contains(stmt, "LIMIT") {
		t.Errorf("builder mutated by One: %s", stmt)
	}
}

// TestCreateCQLDoesNotMutateBuilder 迴歸測試：CreateCQL 的預設值不寫回 builder。
// 歷史 bug：先 CreateCQL 預覽，再 NoDefaults().CreateCQL() 仍帶著全部預設值
func TestCreateCQLDoesNotMutateBuilder(t *testing.T) {
	tb := (&CassandraDB{}).Table("t").Column("id", TypeInt).PartitionKey("id")
	_ = tb.CreateCQL()
	if got := tb.NoDefaults().CreateCQL(); strings.Contains(got, "compaction") || strings.Contains(got, "gc_grace_seconds") {
		t.Errorf("defaults leaked into the builder:\n%s", got)
	}
}

// TestUpdateValidation 迴歸測試：沒有 SET 的 UPDATE、IfExists 與 If 併用都在送出前回錯誤。
// 歷史 bug：產生 "SET  WHERE" 語法錯誤；IfExists 時 If 條件被靜默丟棄
func TestUpdateValidation(t *testing.T) {
	db, _ := NewWithoutConnect(Config{Hosts: []string{"127.0.0.1"}})
	ctx := context.Background()
	err := noPanic(t, "empty SET", func() error { return db.Update("t").WhereEq("id", 1).Exec(ctx) })
	if err == nil || !strings.Contains(err.Error(), "no SET") {
		t.Errorf("empty SET: err = %v", err)
	}
	err = noPanic(t, "UpdateModel", func() error { return db.UpdateModel(ctx, &regressUser{ID: 1}) })
	if err == nil || !strings.Contains(err.Error(), "no SET") {
		t.Errorf("UpdateModel with nothing to set: err = %v", err)
	}
	for name, op := range map[string]func() error{
		"update": func() error {
			return db.Update("t").Set("name", "x").WhereEq("id", 1).IfExists().If("name = ?", "y").Exec(ctx)
		},
		"delete": func() error { return db.Delete("t").WhereEq("id", 1).IfExists().If("name = ?", "y").Exec(ctx) },
	} {
		if err := noPanic(t, name, op); err == nil || !strings.Contains(err.Error(), "IfExists and If") {
			t.Errorf("%s IfExists+If: err = %v", name, err)
		}
	}
}

// TestLiteralEscaping 迴歸測試：選項值、class 名稱、DC 名稱中的單引號被跳脫。
// 歷史 bug：以 '%s' 直接拼接，含引號的輸入產生壞掉的 CQL
func TestLiteralEscaping(t *testing.T) {
	db := &CassandraDB{}
	cases := map[string]string{
		"index class":  db.Index("i").On("t", "c").Custom("x'); DROP TABLE t; --").CreateCQL(),
		"index option": db.Index("i").On("t", "c").SAI().Option("k'", "v").CreateCQL(),
		"dc name":      db.Keyspace("ks").NetworkTopology(map[string]int{"dc1'": 3}).CreateCQL(),
		"compaction":   CompactionOptions{Class: "a'b", Extra: map[string]interface{}{"k'": "v"}}.ToCQL(),
		"caching":      CachingOptions{Keys: "A'"}.ToCQL(),
	}
	wants := map[string]string{
		"index class":  "USING 'x''); DROP TABLE t; --'",
		"index option": "{'k''': 'v'}",
		"dc name":      "'dc1''': 3",
		"compaction":   "{'class': 'a''b', 'k''': 'v'}",
		"caching":      "'keys': 'A'''",
	}
	for name, got := range cases {
		if !strings.Contains(got, wants[name]) {
			t.Errorf("%s: %s\nwant it to contain %s", name, got, wants[name])
		}
	}
}

// TestIndexDropRequiresName 未命名的 index 無法 DROP，回錯誤而非送出 "DROP INDEX "
func TestIndexDropRequiresName(t *testing.T) {
	err := noPanic(t, "Drop", func() error { return (&CassandraDB{}).Index("").On("t", "c").Drop(context.Background()) })
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("err = %v, want a missing-name error", err)
	}
}

// TestSchemaDiffDeterministic 迴歸測試：SchemaDiff 輸出順序固定（keyspace → table → column）。
// 歷史 bug：依 map 迭代順序輸出，每次執行順序不同
func TestSchemaDiffDeterministic(t *testing.T) {
	cols := func(names ...string) []ColumnSchema {
		var out []ColumnSchema
		for _, n := range names {
			out = append(out, ColumnSchema{Name: n, Kind: "regular", Type: "text"})
		}
		return out
	}
	old := &Schema{Keyspaces: []KeyspaceSchema{
		{Name: "b", Tables: []TableSchema{{Name: "t2", Columns: cols("x", "y", "z")}, {Name: "t1"}}},
		{Name: "a", Tables: []TableSchema{{Name: "gone"}}},
		{Name: "c"},
	}}
	new := &Schema{Keyspaces: []KeyspaceSchema{
		{Name: "b", Tables: []TableSchema{{Name: "t2", Columns: cols("w", "y")}, {Name: "t3"}}},
		{Name: "d", Tables: []TableSchema{{Name: "n2"}, {Name: "n1"}}},
		{Name: "a"},
	}}
	first := SchemaDiff(old, new)
	var got []string
	for _, c := range first {
		got = append(got, fmt.Sprintf("%s %s.%s.%s", c.Kind, c.Keyspace, c.Table, c.Column))
	}
	want := []string{
		"drop_table a.gone.",
		"drop_table b.t1.",
		"drop_column b.t2.x",
		"drop_column b.t2.z",
		"add_column b.t2.w",
		"add_table b.t3.",
		"drop_keyspace c..",
		"add_keyspace d..",
		"add_table d.n1.",
		"add_table d.n2.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	for i := 0; i < 20; i++ {
		if !reflect.DeepEqual(SchemaDiff(old, new), first) {
			t.Fatal("SchemaDiff order changed between runs")
		}
	}
}
