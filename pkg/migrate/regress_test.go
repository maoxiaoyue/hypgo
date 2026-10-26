package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/schema"
)

// TestGenerateSQLAddTableCreatesTable 迴歸測試（CLI A5）：新增的表產生含全部欄位的 CREATE TABLE，down 依相反順序回滾。
// 歷史 bug：AddTable 的 up 為空（註解寫「由 GenerateCreateTable 處理」但該函式不存在），
// 只輸出一串對不存在的表執行的 ALTER TABLE ... ADD COLUMN；down 先 DROP TABLE 再 DROP COLUMN
func TestGenerateSQLAddTableCreatesTable(t *testing.T) {
	r := NewRegistry()
	r.Register((*TestPost)(nil))
	changes := Diff(mustScan(t, r, "postgres"), &Snapshot{Tables: map[string]TableSchema{}})
	changes = append(changes, Change{Type: AddColumn, Table: "users", Column: &ColumnSchema{Name: "bio", GoType: "string", SQLType: "VARCHAR"}})

	up, down := mustGenerate(t, changes, "postgres")
	if !strings.HasPrefix(up, "BEGIN;\n\nCREATE TABLE \"test_posts\" (") {
		t.Fatalf("up should start with CREATE TABLE, got:\n%s", up)
	}
	for _, col := range []string{`"id" BIGSERIAL NOT NULL`, `"title" VARCHAR NOT NULL`, `"user_id" BIGINT NOT NULL`, `PRIMARY KEY ("id")`} {
		if !strings.Contains(up, col) {
			t.Errorf("CREATE TABLE missing %s:\n%s", col, up)
		}
	}
	if strings.Contains(up, `ALTER TABLE "test_posts"`) {
		t.Errorf("columns of a new table must not be added via ALTER TABLE:\n%s", up)
	}
	if !strings.Contains(up, `ALTER TABLE "users" ADD COLUMN "bio"`) {
		t.Errorf("AddColumn on an existing table still needs ALTER TABLE:\n%s", up)
	}

	wantDown := "BEGIN;\n\nALTER TABLE \"users\" DROP COLUMN \"bio\";\nDROP TABLE \"test_posts\";\n\nCOMMIT;\n"
	if down != wantDown {
		t.Errorf("down = %q, want %q", down, wantDown)
	}
}

// --- bun metadata 掃描（A1–A7） ---

type RegAuthor struct {
	bun.BaseModel `bun:"table:reg_authors"`

	ID    int64         `bun:",pk,autoincrement"`
	Email string        `bun:",notnull,unique"`
	Posts []*RegArticle `bun:"rel:has-many,join:id=author_id"`
}

type RegTimestamps struct {
	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:",nullzero"`
}

// RegArticle 無 BaseModel：表名同 bun 為 reg_articles
type RegArticle struct {
	ID       int64      `bun:",pk,autoincrement"`
	AuthorID int64      `bun:",notnull"`
	Author   *RegAuthor `bun:"rel:belongs-to,join:author_id=id"`
	Title    string     `bun:",notnull,default:'untitled'"`
	Price    float64    `bun:"type:decimal(10,2)"`
	Views    uint64
	Count    int
	Small    int16
	Raw      []byte
	Tags     []string
	Attrs    map[string]any
	Nick     sql.NullString
	Score    sql.NullInt64
	Total    int64 `bun:",scanonly"`
	RegTimestamps
	Slug string `bun:",unique:reg_article_slug"`
	Lang string `bun:",unique:reg_article_slug"`
}

// RegMembership 未加 tag 的 bun.BaseModel、複合主鍵
type RegMembership struct {
	bun.BaseModel

	UserID  int64 `bun:",pk"`
	GroupID int64 `bun:",pk"`
	Role    string
}

type RegIdentity struct {
	bun.BaseModel `bun:"table:reg_identities"`

	ID   int64  `bun:",pk,identity"`
	Code string `bun:",notnull,default:'x'"`
}

// TestScanModelUsesBunMetadata 迴歸測試（A1–A5）：欄位名、表名、欄位集合與型別與 bun 的 table metadata 一致。
// 歷史 bug：自行解析 tag——ID → i_d、UserID → user_i_d；未給表名時不轉複數（reg_article）；
// relation、scanonly、未加 tag 的 bun.BaseModel 被當成 TEXT 欄位；內嵌 struct 未攤平；
// type:decimal(10,2) 被逗號切斷；uint / []byte / map / sql.Null* 一律退回 TEXT、int 對應 32 位元 INTEGER
func TestScanModelUsesBunMetadata(t *testing.T) {
	ts := ScanModel((*RegArticle)(nil))
	if ts == nil {
		t.Fatal("ScanModel returned nil")
	}
	if ts.Name != "reg_articles" {
		t.Errorf("table = %q, want reg_articles", ts.Name)
	}
	var names []string
	types := map[string]string{}
	for _, c := range ts.Columns {
		names = append(names, c.Name)
		types[c.Name] = c.SQLType
	}
	want := "id author_id title price views count small raw tags attrs nick score created_at updated_at slug lang"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("columns = %s\nwant      %s", got, want)
	}
	for col, typ := range map[string]string{
		"price": "decimal(10,2)", "views": "BIGINT", "count": "BIGINT", "small": "SMALLINT",
		"raw": "BYTEA", "tags": "JSONB", "attrs": "JSONB", "nick": "VARCHAR", "score": "BIGINT",
		"created_at": "TIMESTAMPTZ", "id": "BIGSERIAL",
	} {
		if types[col] != typ {
			t.Errorf("%s type = %q, want %q", col, types[col], typ)
		}
	}

	m := ScanModel((*RegMembership)(nil))
	if m == nil || m.Name != "reg_memberships" || len(m.Columns) != 3 || m.Columns[0].Name != "user_id" {
		t.Errorf("RegMembership = %+v, want table reg_memberships with user_id, group_id, role", m)
	}
}

// TestScanModelExplicitTypeKeepsAutoincrement 迴歸測試（B）：明確的 type: 不會丟掉 autoincrement。
// 歷史 bug：有 type: 時直接使用該型別，PostgreSQL 得到不會自動遞增的 bigint
func TestScanModelExplicitTypeKeepsAutoincrement(t *testing.T) {
	type regTyped struct {
		ID int64 `bun:"id,pk,autoincrement,type:bigint"`
	}
	ts := ScanModel((*regTyped)(nil))
	if ts == nil || len(ts.Columns) != 1 || !strings.EqualFold(ts.Columns[0].SQLType, "BIGSERIAL") {
		t.Fatalf("id should be BIGSERIAL on PostgreSQL, got %+v", ts)
	}
}

type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("no database")
}
func (fakeConnector) Driver() driver.Driver { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("no database") }

// bunCreateTable 以 bun 自己的 CreateTableQuery 產生 DDL（不需資料庫連線）
func bunCreateTable(t *testing.T, dialect string, model any) string {
	t.Helper()
	var d schema.Dialect = pgdialect.New()
	if dialect == DialectMySQL {
		d = mysqldialect.New()
	}
	db := bun.NewDB(sql.OpenDB(fakeConnector{}), d)
	return db.NewCreateTable().Model(model).String()
}

// oneLine 把 GenerateSQL 的分行 CREATE TABLE 轉成 bun 的單行格式
func oneLine(stmt string) string {
	stmt = strings.TrimSuffix(strings.TrimSpace(stmt), ";")
	stmt = strings.Replace(stmt, " (\n    ", " (", 1)
	stmt = strings.ReplaceAll(stmt, ",\n    ", ", ")
	return strings.ReplaceAll(stmt, "\n)", ")")
}

// createStatements 取出 up 中的各個 CREATE TABLE
func createStatements(up string) []string {
	var out []string
	for _, part := range strings.Split(up, "CREATE TABLE ")[1:] {
		out = append(out, oneLine("CREATE TABLE "+part[:strings.Index(part, "\n);")+3]))
	}
	return out
}

// TestCreateTableMatchesBun 迴歸測試（A1–A7）：兩種方言的 CREATE TABLE 與 bun db.NewCreateTable().Model(m) 完全一致。
// 歷史 bug：欄位名、表名、型別各自推導——MySQL 字串為 TEXT（UNIQUE 失敗 1170、TEXT DEFAULT 無效）、
// 複合主鍵每欄各寫一次 PRIMARY KEY 而建表失敗、relation 與 scanonly 變成欄位
func TestCreateTableMatchesBun(t *testing.T) {
	models := []any{(*RegAuthor)(nil), (*RegArticle)(nil), (*RegMembership)(nil), (*RegIdentity)(nil)}
	for _, dialect := range []string{DialectPostgres, DialectMySQL} {
		r := NewRegistry()
		r.Register(models...)
		up, _ := mustGenerate(t, Diff(mustScan(t, r, dialect), &Snapshot{Tables: map[string]TableSchema{}}), dialect)
		got := createStatements(up)
		if len(got) != len(models) {
			t.Fatalf("%s: got %d CREATE TABLE, want %d:\n%s", dialect, len(got), len(models), up)
		}
		for i, m := range models {
			if want := bunCreateTable(t, dialect, m); got[i] != want {
				t.Errorf("%s %T:\n got  %s\n want %s", dialect, m, got[i], want)
			}
		}
	}
}

// TestCreateTableMySQLStringsAndCompositeKey 迴歸測試（A6、A7）：MySQL 字串為 VARCHAR(255)、複合主鍵為單一表層級約束、無 IF NOT EXISTS。
// 歷史 bug：MySQL 字串為 TEXT——UNIQUE 失敗（1170）、TEXT DEFAULT 'x' 無效；複合主鍵每欄各一個 PRIMARY KEY；
// CREATE TABLE IF NOT EXISTS 在表已存在時默默不做事，down 卻會 DROP 掉它
func TestCreateTableMySQLStringsAndCompositeKey(t *testing.T) {
	r := NewRegistry()
	r.Register((*RegAuthor)(nil), (*RegMembership)(nil), (*RegIdentity)(nil))
	up, _ := mustGenerate(t, Diff(mustScan(t, r, "mysql"), &Snapshot{Tables: map[string]TableSchema{}}), "mysql")
	for _, want := range []string{"`email` VARCHAR(255) NOT NULL", "UNIQUE (`email`)", "PRIMARY KEY (`user_id`, `group_id`)",
		"`code` VARCHAR(255) NOT NULL DEFAULT 'x'"} {
		if !strings.Contains(up, want) {
			t.Errorf("missing %s:\n%s", want, up)
		}
	}
	if strings.Count(up, "PRIMARY KEY") != 3 || strings.Contains(up, "IF NOT EXISTS") {
		t.Errorf("want one table-level PRIMARY KEY per table and no IF NOT EXISTS:\n%s", up)
	}
}

type regItem struct {
	ID int64 `bun:",pk"`
}

type regOrder struct {
	ID    int64      `bun:",pk"`
	Items []*regItem `bun:"m2m:reg_order_to_items,join:Order=Item"`
}

type regOrderToItem struct {
	bun.BaseModel `bun:"table:reg_order_to_items"`

	OrderID int64     `bun:",pk"`
	Order   *regOrder `bun:"rel:belongs-to,join:order_id=id"`
	ItemID  int64     `bun:",pk"`
	Item    *regItem  `bun:"rel:belongs-to,join:item_id=id"`
}

// TestScanModelsM2MAndDuplicates 迴歸測試（B）：m2m junction 晚註冊也能掃描；同型別重複註冊只產生一張表；Register(nil) 回錯。
// 歷史 bug：重複註冊產生重複的 CREATE TABLE；Register(nil) 讓 server.Start 內 panic
func TestScanModelsM2MAndDuplicates(t *testing.T) {
	r := NewRegistry()
	r.Register((*regOrder)(nil), (*regItem)(nil), (*regOrder)(nil), (*regOrderToItem)(nil))
	tables := mustScan(t, r, "postgres")
	var names []string
	for _, ts := range tables {
		names = append(names, ts.Name)
	}
	if got := strings.Join(names, " "); got != "reg_orders reg_items reg_order_to_items" {
		t.Errorf("tables = %s", got)
	}
	if len(tables[0].Columns) != 1 {
		t.Errorf("m2m relation must not be a column: %+v", tables[0].Columns)
	}

	r.Register(nil)
	if _, err := ScanModels(r, "postgres"); err == nil {
		t.Error("Register(nil) should make ScanModels fail")
	}
}

// --- Registry（B） ---

// TestRegistryDuplicateRegistration 迴歸測試（B）：同一型別註冊多次只算一次。
// 歷史 bug：append 重複的 model，產生重複的 CREATE TABLE 與欄位
func TestRegistryDuplicateRegistration(t *testing.T) {
	r := NewRegistry()
	r.Register((*TestPost)(nil), (*TestPost)(nil))
	r.Register((*TestPost)(nil))
	if r.Len() != 1 {
		t.Errorf("Len() = %d, want 1", r.Len())
	}
}

// TestRegistryModelsReturnsCopy 迴歸測試（B）：Models() 回傳副本。
// 歷史 bug：回傳內部切片，呼叫端修改會改到註冊表
func TestRegistryModelsReturnsCopy(t *testing.T) {
	r := NewRegistry()
	r.Register((*TestPost)(nil))
	r.Models()[0] = (*TestUser)(nil)
	if _, ok := r.Models()[0].(*TestPost); !ok {
		t.Errorf("caller mutation leaked into registry: %T", r.Models()[0])
	}
}

// TestRegistryConcurrentUse 迴歸測試（B）：Register / Models / Len 可並行呼叫（以 -race 驗證）。
// 歷史 bug：registry 沒有 mutex
func TestRegistryConcurrentUse(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); r.Register((*TestPost)(nil), (*TestUser)(nil)) }()
		go func() { defer wg.Done(); _ = r.Models(); _ = r.Len() }()
	}
	wg.Wait()
	if r.Len() != 2 {
		t.Errorf("Len() = %d, want 2", r.Len())
	}
}

// TestWriteSchemaFileRegisterNil 迴歸測試（B）：GlobalRegistry().Register(nil) 時 WriteSchemaFile 回錯而非 panic。
// 歷史 bug：reflect.TypeOf(nil).Kind() 在 server.Start 內 panic
func TestWriteSchemaFileRegisterNil(t *testing.T) {
	saved := globalRegistry
	globalRegistry = NewRegistry()
	defer func() { globalRegistry = saved }()
	globalRegistry.Register(nil)

	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("WriteSchemaFile panicked: %v", r)
			}
		}()
		err = WriteSchemaFile(filepath.Join(t.TempDir(), "schema.json"))
	}()
	if err == nil {
		t.Error("WriteSchemaFile should report Register(nil)")
	}
}

// TestNormalizeDialect 迴歸測試（B）：方言名稱不分大小寫正規化，未知方言回錯。
// 歷史 bug：未驗證——pg 得到 PostgreSQL 引號搭配 MySQL 型別，oracle 也照樣產生
func TestNormalizeDialect(t *testing.T) {
	for in, want := range map[string]string{"postgres": "postgres", "PostgreSQL": "postgres", "pg": "postgres",
		"MySQL": "mysql", "mariadb": "mysql"} {
		if got, err := NormalizeDialect(in); err != nil || got != want {
			t.Errorf("NormalizeDialect(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeDialect("oracle"); err == nil {
		t.Error("oracle should be rejected")
	}
	if _, _, err := GenerateSQL([]Change{{Type: DropTable, Table: "x"}}, "sqlite"); err == nil {
		t.Error("GenerateSQL should reject an unknown dialect")
	}
	up, _ := mustGenerate(t, []Change{{Type: AddColumn, Table: "t", Column: &ColumnSchema{Name: "c", SQLType: "BIGINT"}}}, "PG")
	if !strings.Contains(up, `ALTER TABLE "t" ADD COLUMN "c" BIGINT;`) {
		t.Errorf("pg should be PostgreSQL:\n%s", up)
	}
}

// --- Diff / 產生 SQL（A8–A12、B） ---

// TestDiffDropTableOrderDeterministic 迴歸測試（B）：被刪除的表依名稱排序。
// 歷史 bug：依 map 走訪，每次產生的 DROP TABLE 順序不同
func TestDiffDropTableOrderDeterministic(t *testing.T) {
	snap := &Snapshot{Tables: map[string]TableSchema{}}
	for _, n := range []string{"j", "c", "h", "a", "e", "b", "i", "d", "g", "f"} {
		snap.Tables[n] = TableSchema{Name: n}
	}
	var got []string
	for _, c := range Diff(nil, snap) {
		got = append(got, c.Table)
	}
	if s := strings.Join(got, ""); s != "abcdefghij" {
		t.Errorf("drop order = %s, want abcdefghij", s)
	}
}

// TestDiffIgnoresGoTypeOnlyChange 迴歸測試（B）：Go 型別改變但 SQL 型別相同時不產生變更。
// 歷史 bug：columnChanged 比對 GoType，產生 SQL 為空的 AlterColumn
func TestDiffIgnoresGoTypeOnlyChange(t *testing.T) {
	snap := &Snapshot{Tables: map[string]TableSchema{"t": {Name: "t", Columns: []ColumnSchema{
		{Name: "n", GoType: "int64", SQLType: "BIGINT"}}}}}
	cur := []TableSchema{{Name: "t", Columns: []ColumnSchema{{Name: "n", GoType: "int", SQLType: "BIGINT"}}}}
	if changes := Diff(cur, snap); len(changes) != 0 {
		t.Errorf("expected no changes, got %v", changes)
	}
}

func alterChange(table string, old, cur ColumnSchema) []Change {
	snap := &Snapshot{Tables: map[string]TableSchema{table: {Name: table, Columns: []ColumnSchema{old}}}}
	return Diff([]TableSchema{{Name: table, Columns: []ColumnSchema{cur}}}, snap)
}

// TestAlterColumnTypeSQL 迴歸測試（A8）：型別變更產生可執行的 ALTER。
// 歷史 bug：PostgreSQL 產生 TYPE BIGSERIAL 且沒有 USING；MySQL MODIFY 重複 PRIMARY KEY / UNIQUE
func TestAlterColumnTypeSQL(t *testing.T) {
	pg := alterChange("t",
		ColumnSchema{Name: "id", SQLType: "SERIAL", PrimaryKey: true, AutoIncrement: true, NotNull: true},
		ColumnSchema{Name: "id", SQLType: "BIGSERIAL", PrimaryKey: true, AutoIncrement: true, NotNull: true})
	up, down := mustGenerate(t, pg, "postgres")
	if !strings.Contains(up, `ALTER TABLE "t" ALTER COLUMN "id" TYPE BIGINT USING "id"::BIGINT;`) ||
		!strings.Contains(down, `ALTER TABLE "t" ALTER COLUMN "id" TYPE INTEGER USING "id"::INTEGER;`) || strings.Contains(up, "SERIAL") {
		t.Errorf("postgres type change:\nup:\n%s\ndown:\n%s", up, down)
	}

	my := alterChange("t",
		ColumnSchema{Name: "email", SQLType: "VARCHAR(100)", NotNull: true, Unique: true, PrimaryKey: true},
		ColumnSchema{Name: "email", SQLType: "VARCHAR(255)", NotNull: true, Unique: true, PrimaryKey: true})
	up, down = mustGenerate(t, my, "mysql")
	if up != "ALTER TABLE `t` MODIFY COLUMN `email` VARCHAR(255) NOT NULL;\n" ||
		down != "ALTER TABLE `t` MODIFY COLUMN `email` VARCHAR(100) NOT NULL;\n" {
		t.Errorf("mysql type change:\nup:\n%s\ndown:\n%s", up, down)
	}
}

// TestAlterChangesAlwaysProduceSQL 迴歸測試（A9）：MySQL 的 NOT NULL / DEFAULT、UNIQUE、自動遞增變更產生 SQL；
// 無法表達的變更（主鍵、PostgreSQL 加減 SERIAL）回 *UnsupportedError 而不是默默略過。
// 歷史 bug：這些變更不產生任何 SQL，快照卻照樣更新，變更就此遺失
func TestAlterChangesAlwaysProduceSQL(t *testing.T) {
	up, _ := mustGenerate(t, alterChange("t",
		ColumnSchema{Name: "n", SQLType: "BIGINT"},
		ColumnSchema{Name: "n", SQLType: "BIGINT", NotNull: true, Default: "0", AutoIncrement: true}), "mysql")
	if up != "ALTER TABLE `t` MODIFY COLUMN `n` BIGINT NOT NULL AUTO_INCREMENT DEFAULT 0;\n" {
		t.Errorf("mysql NOT NULL / DEFAULT / AUTO_INCREMENT change:\n%s", up)
	}

	snap := &Snapshot{Tables: map[string]TableSchema{"t": {Name: "t",
		Columns: []ColumnSchema{{Name: "a", SQLType: "BIGINT"}, {Name: "b", SQLType: "BIGINT"}}, PrimaryKey: []string{"a"}}}}
	cur := []TableSchema{{Name: "t", Columns: []ColumnSchema{{Name: "a", SQLType: "BIGINT", Unique: true}, {Name: "b", SQLType: "BIGINT"}},
		PrimaryKey: []string{"a"}, Uniques: []UniqueSchema{{Columns: []string{"a"}}, {Name: "t_ab", Columns: []string{"a", "b"}}}}}
	pgUp, pgDown := mustGenerate(t, Diff(cur, snap), "postgres")
	for _, want := range []string{`ALTER TABLE "t" ADD CONSTRAINT "t_a_key" UNIQUE ("a");`, `ALTER TABLE "t" ADD CONSTRAINT "t_ab" UNIQUE ("a", "b");`} {
		if !strings.Contains(pgUp, want) {
			t.Errorf("missing %s:\n%s", want, pgUp)
		}
	}
	if !strings.Contains(pgDown, `ALTER TABLE "t" DROP CONSTRAINT "t_a_key";`) {
		t.Errorf("down should drop the unique constraint:\n%s", pgDown)
	}
	myUp, _ := mustGenerate(t, Diff(cur, snap), "mysql")
	if !strings.Contains(myUp, "ALTER TABLE `t` ADD CONSTRAINT `a` UNIQUE (`a`);") {
		t.Errorf("mysql unique:\n%s", myUp)
	}

	cur[0].PrimaryKey = []string{"a", "b"}
	_, _, err := GenerateSQL(Diff(cur, snap), "postgres")
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "ALTER PRIMARY KEY t (a) -> (a, b)") {
		t.Errorf("primary key change should be reported as unsupported, got %v", err)
	}
	_, _, err = GenerateSQL(alterChange("t", ColumnSchema{Name: "n", SQLType: "BIGINT"},
		ColumnSchema{Name: "n", SQLType: "BIGSERIAL", AutoIncrement: true}), "postgres")
	if !errors.As(err, &unsupported) {
		t.Errorf("adding SERIAL on PostgreSQL should be unsupported, got %v", err)
	}
}

type RegMember struct {
	bun.BaseModel `bun:"table:reg_members" migrate:"renamed_from=reg_users"`

	ID       int64  `bun:",pk,autoincrement"`
	FullName string `bun:",notnull,unique" migrate:"renamed_from=name"`
}

// TestRenameHints 迴歸測試（A10）：migrate:"renamed_from=..." 產生 RENAME TO / RENAME COLUMN（連同未命名 UNIQUE 的名稱），
// 未加提示而同型別的一刪一增時給出提示。
// 歷史 bug：更名一律變成 DROP + ADD，資料遺失（PostgreSQL ADD NOT NULL 無 DEFAULT 在有資料的表上還會失敗）
func TestRenameHints(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectMySQL} {
		idType, nameType := "BIGSERIAL", "VARCHAR"
		if dialect == DialectMySQL {
			idType, nameType = "BIGINT", "VARCHAR(255)"
		}
		old := TableSchema{Name: "reg_users", PrimaryKey: []string{"id"}, Uniques: []UniqueSchema{{Columns: []string{"name"}}},
			Columns: []ColumnSchema{
				{Name: "id", SQLType: idType, PrimaryKey: true, AutoIncrement: true, NotNull: true},
				{Name: "name", SQLType: nameType, NotNull: true, Unique: true},
			}}
		snap := &Snapshot{Tables: map[string]TableSchema{"reg_users": old}}

		r := NewRegistry()
		r.Register((*RegMember)(nil))
		changes := Diff(mustScan(t, r, dialect), snap)
		var kinds []string
		for _, c := range changes {
			kinds = append(kinds, c.String())
			if c.Destructive() != "" {
				t.Errorf("rename should not be destructive: %s", c.Destructive())
			}
		}
		if got := strings.Join(kinds, "; "); got != "RENAME TABLE reg_users -> reg_members; RENAME COLUMN reg_members.name -> full_name" {
			t.Fatalf("%s changes = %s", dialect, got)
		}
		up, down := mustGenerate(t, changes, dialect)
		q := func(s string) string {
			if dialect == DialectMySQL {
				return strings.ReplaceAll(s, `"`, "`")
			}
			return s
		}
		wantUp := []string{q(`ALTER TABLE "reg_users" RENAME TO "reg_members";`),
			q(`ALTER TABLE "reg_members" RENAME COLUMN "name" TO "full_name";`)}
		if dialect == DialectPostgres {
			wantUp = append(wantUp, `ALTER TABLE "reg_members" RENAME CONSTRAINT "reg_users_name_key" TO "reg_members_name_key";`,
				`ALTER TABLE "reg_members" RENAME CONSTRAINT "reg_members_name_key" TO "reg_members_full_name_key";`)
		} else {
			wantUp = append(wantUp, "ALTER TABLE `reg_members` RENAME INDEX `name` TO `full_name`;")
		}
		for _, w := range wantUp {
			if !strings.Contains(up, w) {
				t.Errorf("%s up missing %s:\n%s", dialect, w, up)
			}
		}
		if !strings.Contains(down, q(`ALTER TABLE "reg_members" RENAME TO "reg_users";`)) || strings.Contains(up, "DROP") {
			t.Errorf("%s down should rename back:\n%s", dialect, down)
		}
	}

	// 沒有提示：同型別一刪一增 → 提示
	snap := &Snapshot{Tables: map[string]TableSchema{"t": {Name: "t", Columns: []ColumnSchema{{Name: "name", SQLType: "VARCHAR"}}}}}
	changes := Diff([]TableSchema{{Name: "t", Columns: []ColumnSchema{{Name: "full_name", SQLType: "VARCHAR"}}}}, snap)
	hints := RenameHints(changes)
	if len(hints) != 1 || !strings.Contains(hints[0], `migrate:"renamed_from=name"`) {
		t.Errorf("hints = %q", hints)
	}
}

// TestDestructiveChangesAndDropTableDown 迴歸測試（A11）：DROP TABLE / DROP COLUMN / 縮窄或不相容的型別 / 既有表加 NOT NULL 無 DEFAULT
// 標示為破壞性，安全的變更不標示；DROP TABLE 的 down 依快照重建整張表。
// 歷史 bug：破壞性變更沒有任何警告；DropTable 的 down 為空
func TestDestructiveChangesAndDropTableDown(t *testing.T) {
	old := TableSchema{Name: "gone", PrimaryKey: []string{"id"}, Columns: []ColumnSchema{
		{Name: "id", SQLType: "BIGSERIAL", PrimaryKey: true, AutoIncrement: true, NotNull: true},
		{Name: "title", SQLType: "VARCHAR(50)", Default: "''"}}}
	dropTable := Diff(nil, &Snapshot{Tables: map[string]TableSchema{"gone": old}})
	_, down := mustGenerate(t, dropTable, "postgres")
	if !strings.Contains(down, "CREATE TABLE \"gone\" (\n    \"id\" BIGSERIAL NOT NULL,\n    \"title\" VARCHAR(50) DEFAULT '',\n    PRIMARY KEY (\"id\")\n);") {
		t.Errorf("DROP TABLE down should recreate the table:\n%s", down)
	}

	cases := []struct {
		changes     []Change
		destructive bool
	}{
		{dropTable, true},
		{alterChange("t", ColumnSchema{Name: "c", SQLType: "VARCHAR(50)"}, ColumnSchema{Name: "c", SQLType: "VARCHAR(20)"}), true},
		{alterChange("t", ColumnSchema{Name: "c", SQLType: "BIGINT"}, ColumnSchema{Name: "c", SQLType: "INTEGER"}), true},
		{alterChange("t", ColumnSchema{Name: "c", SQLType: "VARCHAR"}, ColumnSchema{Name: "c", SQLType: "BIGINT"}), true},
		{alterChange("t", ColumnSchema{Name: "c", SQLType: "VARCHAR(20)"}, ColumnSchema{Name: "c", SQLType: "TEXT"}), false},
		{alterChange("t", ColumnSchema{Name: "c", SQLType: "INTEGER"}, ColumnSchema{Name: "c", SQLType: "BIGINT"}), false},
		{alterChange("t", ColumnSchema{Name: "c", SQLType: "BIGINT"}, ColumnSchema{Name: "c", SQLType: "BIGINT", NotNull: true}), false},
		{[]Change{{Type: DropColumn, Table: "t", Column: &ColumnSchema{Name: "c", SQLType: "BIGINT"}}}, true},
		{[]Change{{Type: AddColumn, Table: "t", Column: &ColumnSchema{Name: "c", SQLType: "BIGINT", NotNull: true}}}, true},
		{[]Change{{Type: AddColumn, Table: "t", Column: &ColumnSchema{Name: "c", SQLType: "BIGINT", NotNull: true, Default: "0"}}}, false},
	}
	for i, tc := range cases {
		got := false
		for _, c := range tc.changes {
			if c.Destructive() != "" {
				got = true
			}
		}
		if got != tc.destructive {
			t.Errorf("case %d (%v): destructive = %v, want %v", i, tc.changes, got, tc.destructive)
		}
	}
}

// TestMigrationFilesGolangMigrate 迴歸測試（A12）：檔名為 golang-migrate 的 <14 位版本>_<name>.up/.down.sql，同一秒內不撞號。
// 歷史 bug：20261026_143000_auto.up.sql 被 golang-migrate 解析為版本 20261026，同一天再產生必定撞號
func TestMigrationFilesGolangMigrate(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 26, 14, 30, 0, 0, time.Local)
	up, down, err := MigrationFiles(dir, "Add User Bio", now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(up) != "20261026143000_add_user_bio.up.sql" || filepath.Base(down) != "20261026143000_add_user_bio.down.sql" {
		t.Fatalf("files = %s, %s", up, down)
	}
	os.WriteFile(up, nil, 0644)
	up2, _, _ := MigrationFiles(dir, "", now)
	if filepath.Base(up2) != "20261026143001_auto.up.sql" {
		t.Errorf("second migration in the same second = %s", up2)
	}
}

// TestSnapshotRecordsDialect 迴歸測試（B）：快照記錄方言與格式版本，換方言或舊格式快照時 CheckDialect 回錯。
// 歷史 bug：快照不含方言，以 MySQL 比對 PostgreSQL 的快照會把每一欄都當成型別變更
func TestSnapshotRecordsDialect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := SaveSnapshot(path, []TableSchema{{Name: "t", RenamedFrom: "old",
		Columns: []ColumnSchema{{Name: "c", SQLType: "BIGINT", RenamedFrom: "x"}}}}, "postgres"); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.CheckDialect("postgres"); err != nil {
		t.Errorf("same dialect: %v", err)
	}
	if err := snap.CheckDialect("mysql"); err == nil {
		t.Error("another dialect should be rejected")
	}
	if snap.Tables["t"].RenamedFrom != "" || snap.Tables["t"].Columns[0].RenamedFrom != "" {
		t.Error("rename hints must not be stored in the snapshot")
	}
	legacy := &Snapshot{Tables: map[string]TableSchema{"t": {Name: "t"}}}
	if err := legacy.CheckDialect("postgres"); err == nil {
		t.Error("a snapshot without version should be rejected")
	}
}
