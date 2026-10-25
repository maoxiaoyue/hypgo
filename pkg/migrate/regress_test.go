package migrate

import (
	"strings"
	"testing"
)

// TestGenerateSQLAddTableCreatesTable 迴歸測試（CLI A5）：新增的表產生含全部欄位的 CREATE TABLE，down 依相反順序回滾。
// 歷史 bug：AddTable 的 up 為空（註解寫「由 GenerateCreateTable 處理」但該函式不存在），
// 只輸出一串對不存在的表執行的 ALTER TABLE ... ADD COLUMN；down 先 DROP TABLE 再 DROP COLUMN
func TestGenerateSQLAddTableCreatesTable(t *testing.T) {
	r := NewRegistry()
	r.Register((*TestPost)(nil))
	changes := Diff(ScanModels(r), &Snapshot{Tables: map[string]TableSchema{}})
	changes = append(changes, Change{Type: AddColumn, Table: "users", Column: &ColumnSchema{Name: "bio", GoType: "string"}})

	up, down := GenerateSQL(changes, "postgres")
	if !strings.HasPrefix(up, `CREATE TABLE IF NOT EXISTS "test_post" (`) {
		t.Fatalf("up should start with CREATE TABLE, got:\n%s", up)
	}
	for _, col := range []string{`"id" BIGSERIAL PRIMARY KEY`, `"title" TEXT NOT NULL`, `"user_id" BIGINT NOT NULL`} {
		if !strings.Contains(up, col) {
			t.Errorf("CREATE TABLE missing %s:\n%s", col, up)
		}
	}
	if strings.Contains(up, `ALTER TABLE "test_post"`) {
		t.Errorf("columns of a new table must not be added via ALTER TABLE:\n%s", up)
	}
	if !strings.Contains(up, `ALTER TABLE "users" ADD COLUMN "bio"`) {
		t.Errorf("AddColumn on an existing table still needs ALTER TABLE:\n%s", up)
	}

	wantDown := "ALTER TABLE \"users\" DROP COLUMN \"bio\";\nDROP TABLE IF EXISTS \"test_post\";"
	if down != wantDown {
		t.Errorf("down = %q, want %q", down, wantDown)
	}
}
