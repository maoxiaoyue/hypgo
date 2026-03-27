package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- 測試用 Model ---

type testBaseModel struct {
	tableName struct{} `bun:"table:users,alias:u"`
}

type TestUser struct {
	testBaseModel
	ID        int64     `bun:"id,pk,autoincrement"`
	Name      string    `bun:"name,notnull"`
	Email     string    `bun:"email,notnull,unique"`
	Bio       string    `bun:"bio,type:text"`
	Score     float64   `bun:"score,default:0"`
	Active    bool      `bun:"active,notnull,default:true"`
	CreatedAt time.Time `bun:"created_at,notnull,default:current_timestamp"`
}

type TestPost struct {
	ID      int64  `bun:"id,pk,autoincrement"`
	Title   string `bun:"title,notnull"`
	Content string `bun:"content,type:text"`
	UserID  int64  `bun:"user_id,notnull"`
}

// --- Registry ---

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register((*TestUser)(nil), (*TestPost)(nil))

	if r.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", r.Len())
	}
}

// --- Scanner ---

func TestScanModelUser(t *testing.T) {
	ts := ScanModel((*TestUser)(nil))
	if ts == nil {
		t.Fatal("ScanModel returned nil")
	}

	// testBaseModel 的 bun tag 中沒有直接的 BaseModel 欄位名
	// 所以表名會從 struct name 推導
	// 檢查有合理的欄位數
	if len(ts.Columns) < 5 {
		t.Fatalf("expected at least 5 columns, got %d", len(ts.Columns))
	}

	// 找 id 欄位
	idCol := findColumn(ts, "id")
	if idCol == nil {
		t.Fatal("id column not found")
	}
	if !idCol.PrimaryKey {
		t.Error("id should be primary key")
	}
	if !idCol.AutoIncrement {
		t.Error("id should be auto increment")
	}

	// 找 name 欄位
	nameCol := findColumn(ts, "name")
	if nameCol == nil {
		t.Fatal("name column not found")
	}
	if !nameCol.NotNull {
		t.Error("name should be notnull")
	}

	// 找 email 欄位
	emailCol := findColumn(ts, "email")
	if emailCol == nil {
		t.Fatal("email column not found")
	}
	if !emailCol.Unique {
		t.Error("email should be unique")
	}

	// 找 bio 欄位
	bioCol := findColumn(ts, "bio")
	if bioCol == nil {
		t.Fatal("bio column not found")
	}
	if bioCol.SQLType != "text" {
		t.Errorf("bio.SQLType = %q, want %q", bioCol.SQLType, "text")
	}

	// 找 score 欄位
	scoreCol := findColumn(ts, "score")
	if scoreCol == nil {
		t.Fatal("score column not found")
	}
	if scoreCol.Default != "0" {
		t.Errorf("score.Default = %q, want %q", scoreCol.Default, "0")
	}
}

func TestScanModelPost(t *testing.T) {
	ts := ScanModel((*TestPost)(nil))
	if ts == nil {
		t.Fatal("ScanModel returned nil")
	}

	// TestPost 無 BaseModel，表名從 struct name 推導
	if ts.Name != "test_post" {
		t.Errorf("Name = %q, want %q", ts.Name, "test_post")
	}

	if len(ts.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(ts.Columns))
	}
}

func TestScanModelNonStruct(t *testing.T) {
	i := 42
	ts := ScanModel(&i)
	if ts != nil {
		t.Error("non-struct should return nil")
	}
}

func TestScanModels(t *testing.T) {
	r := NewRegistry()
	r.Register((*TestUser)(nil), (*TestPost)(nil))

	tables := ScanModels(r)
	if len(tables) != 2 {
		t.Fatalf("got %d tables, want 2", len(tables))
	}
}

// --- toSnakeCase ---

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"UserID", "user_i_d"},
		{"Name", "name"},
		{"CreatedAt", "created_at"},
		{"ID", "i_d"},
		{"simple", "simple"},
	}
	for _, tt := range tests {
		got := toSnakeCase(tt.input)
		if got != tt.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- Diff ---

func TestDiffAddTable(t *testing.T) {
	current := []TableSchema{
		{Name: "users", Columns: []ColumnSchema{
			{Name: "id", GoType: "int64", PrimaryKey: true},
			{Name: "name", GoType: "string"},
		}},
	}
	snapshot := &Snapshot{Tables: make(map[string]TableSchema)}

	changes := Diff(current, snapshot)

	// 1 AddTable + 2 AddColumn
	if len(changes) != 3 {
		t.Fatalf("got %d changes, want 3", len(changes))
	}
	if changes[0].Type != AddTable {
		t.Errorf("changes[0].Type = %v, want AddTable", changes[0].Type)
	}
}

func TestDiffDropTable(t *testing.T) {
	current := []TableSchema{}
	snapshot := &Snapshot{Tables: map[string]TableSchema{
		"old_table": {Name: "old_table"},
	}}

	changes := Diff(current, snapshot)

	if len(changes) != 1 || changes[0].Type != DropTable {
		t.Errorf("expected 1 DropTable, got %v", changes)
	}
}

func TestDiffAddColumn(t *testing.T) {
	current := []TableSchema{
		{Name: "users", Columns: []ColumnSchema{
			{Name: "id", GoType: "int64"},
			{Name: "name", GoType: "string"},
			{Name: "bio", GoType: "string"}, // 新欄位
		}},
	}
	snapshot := &Snapshot{Tables: map[string]TableSchema{
		"users": {Name: "users", Columns: []ColumnSchema{
			{Name: "id", GoType: "int64"},
			{Name: "name", GoType: "string"},
		}},
	}}

	changes := Diff(current, snapshot)

	if len(changes) != 1 || changes[0].Type != AddColumn {
		t.Fatalf("expected 1 AddColumn, got %d changes", len(changes))
	}
	if changes[0].Column.Name != "bio" {
		t.Errorf("Column.Name = %q, want %q", changes[0].Column.Name, "bio")
	}
}

func TestDiffDropColumn(t *testing.T) {
	current := []TableSchema{
		{Name: "users", Columns: []ColumnSchema{
			{Name: "id", GoType: "int64"},
		}},
	}
	snapshot := &Snapshot{Tables: map[string]TableSchema{
		"users": {Name: "users", Columns: []ColumnSchema{
			{Name: "id", GoType: "int64"},
			{Name: "old_col", GoType: "string"},
		}},
	}}

	changes := Diff(current, snapshot)

	if len(changes) != 1 || changes[0].Type != DropColumn {
		t.Fatalf("expected 1 DropColumn, got %v", changes)
	}
}

func TestDiffAlterColumn(t *testing.T) {
	current := []TableSchema{
		{Name: "users", Columns: []ColumnSchema{
			{Name: "name", GoType: "string", NotNull: true}, // 變為 NOT NULL
		}},
	}
	snapshot := &Snapshot{Tables: map[string]TableSchema{
		"users": {Name: "users", Columns: []ColumnSchema{
			{Name: "name", GoType: "string", NotNull: false},
		}},
	}}

	changes := Diff(current, snapshot)

	if len(changes) != 1 || changes[0].Type != AlterColumn {
		t.Fatalf("expected 1 AlterColumn, got %v", changes)
	}
}

func TestDiffNoChanges(t *testing.T) {
	cols := []ColumnSchema{{Name: "id", GoType: "int64"}}
	current := []TableSchema{{Name: "users", Columns: cols}}
	snapshot := &Snapshot{Tables: map[string]TableSchema{
		"users": {Name: "users", Columns: cols},
	}}

	changes := Diff(current, snapshot)
	if len(changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(changes))
	}
}

// --- SQL Generator ---

func TestGenerateSQLPostgres(t *testing.T) {
	changes := []Change{
		{Type: AddColumn, Table: "users", Column: &ColumnSchema{
			Name: "bio", GoType: "string", SQLType: "text",
		}},
	}

	up, down := GenerateSQL(changes, "postgres")

	if !strings.Contains(up, `ADD COLUMN "bio" TEXT`) {
		t.Errorf("up SQL missing ADD COLUMN: %s", up)
	}
	if !strings.Contains(down, `DROP COLUMN "bio"`) {
		t.Errorf("down SQL missing DROP COLUMN: %s", down)
	}
}

func TestGenerateSQLMySQL(t *testing.T) {
	changes := []Change{
		{Type: AddColumn, Table: "users", Column: &ColumnSchema{
			Name: "age", GoType: "int",
		}},
	}

	up, _ := GenerateSQL(changes, "mysql")

	if !strings.Contains(up, "`users`") {
		t.Errorf("MySQL should use backticks: %s", up)
	}
}

func TestGenerateSQLAutoIncrement(t *testing.T) {
	changes := []Change{
		{Type: AddColumn, Table: "users", Column: &ColumnSchema{
			Name: "id", GoType: "int64", PrimaryKey: true, AutoIncrement: true,
		}},
	}

	upPg, _ := GenerateSQL(changes, "postgres")
	upMy, _ := GenerateSQL(changes, "mysql")

	if !strings.Contains(upPg, "BIGSERIAL") {
		t.Errorf("Postgres should use BIGSERIAL: %s", upPg)
	}
	if !strings.Contains(upMy, "AUTO_INCREMENT") {
		t.Errorf("MySQL should use AUTO_INCREMENT: %s", upMy)
	}
}

func TestGenerateSQLDropTable(t *testing.T) {
	changes := []Change{
		{Type: DropTable, Table: "old_table"},
	}

	up, _ := GenerateSQL(changes, "postgres")
	if !strings.Contains(up, `DROP TABLE IF EXISTS "old_table"`) {
		t.Errorf("up SQL: %s", up)
	}
}

func TestGenerateSQLEmpty(t *testing.T) {
	up, down := GenerateSQL(nil, "postgres")
	if up != "" || down != "" {
		t.Error("empty changes should produce empty SQL")
	}
}

// --- Snapshot ---

func TestSnapshotSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	tables := []TableSchema{
		{Name: "users", Columns: []ColumnSchema{
			{Name: "id", GoType: "int64", PrimaryKey: true},
			{Name: "name", GoType: "string", NotNull: true},
		}},
	}

	if err := SaveSnapshot(path, tables); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	if len(loaded.Tables) != 1 {
		t.Fatalf("loaded %d tables, want 1", len(loaded.Tables))
	}

	users := loaded.Tables["users"]
	if len(users.Columns) != 2 {
		t.Errorf("users has %d columns, want 2", len(users.Columns))
	}
}

func TestSnapshotLoadNotExist(t *testing.T) {
	snap, err := LoadSnapshot("/nonexistent/path.json")
	if err != nil {
		t.Fatalf("should not error on missing file: %v", err)
	}
	if len(snap.Tables) != 0 {
		t.Error("missing file should return empty snapshot")
	}
}

func TestSnapshotJSON(t *testing.T) {
	tables := []TableSchema{
		{Name: "test", Columns: []ColumnSchema{{Name: "id", GoType: "int64"}}},
	}

	snap := Snapshot{Tables: make(map[string]TableSchema)}
	for _, t := range tables {
		snap.Tables[t.Name] = t
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}

	var parsed Snapshot
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Tables["test"]; !ok {
		t.Error("test table not found in parsed snapshot")
	}
}

// --- Change.String ---

func TestChangeString(t *testing.T) {
	tests := []struct {
		change Change
		want   string
	}{
		{Change{Type: AddTable, Table: "users"}, "ADD TABLE users"},
		{Change{Type: DropTable, Table: "users"}, "DROP TABLE users"},
		{Change{Type: AddColumn, Table: "users", Column: &ColumnSchema{Name: "bio"}}, "ADD COLUMN users.bio"},
		{Change{Type: DropColumn, Table: "users", Column: &ColumnSchema{Name: "bio"}}, "DROP COLUMN users.bio"},
		{Change{Type: AlterColumn, Table: "users", Column: &ColumnSchema{Name: "bio"}}, "ALTER COLUMN users.bio"},
	}
	for _, tt := range tests {
		got := tt.change.String()
		if got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

// --- helpers ---

func findColumn(ts *TableSchema, name string) *ColumnSchema {
	for i := range ts.Columns {
		if ts.Columns[i].Name == name {
			return &ts.Columns[i]
		}
	}
	return nil
}

// --- Full Integration ---

func TestFullDiffWorkflow(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "schema.json")

	// 第一次：空快照 → 新增表
	r := NewRegistry()
	r.Register((*TestPost)(nil))

	tables := ScanModels(r)
	snap, _ := LoadSnapshot(snapshotPath)
	changes := Diff(tables, snap)

	if len(changes) == 0 {
		t.Fatal("first diff should have changes")
	}

	up, down := GenerateSQL(changes, "postgres")
	if up == "" {
		t.Fatal("up SQL should not be empty")
	}
	if down == "" {
		t.Fatal("down SQL should not be empty")
	}

	// 儲存快照
	if err := SaveSnapshot(snapshotPath, tables); err != nil {
		t.Fatal(err)
	}

	// 第二次：相同 Model → 無變更
	snap2, _ := LoadSnapshot(snapshotPath)
	changes2 := Diff(tables, snap2)
	if len(changes2) != 0 {
		t.Errorf("second diff should have 0 changes, got %d", len(changes2))
	}

	// 確認快照檔案存在
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		t.Error("snapshot file should exist")
	}
}
