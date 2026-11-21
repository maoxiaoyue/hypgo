package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

type r3Before struct {
	bun.BaseModel `bun:"table:r3_users"`
	ID            int64   `bun:",pk,autoincrement"`
	Email         string  `bun:"email"`
	Nick          *string `bun:"nick"`
	Bio           string  `bun:"bio,notnull"`
}

type r3After struct {
	bun.BaseModel `bun:"table:r3_users"`
	ID            int64  `bun:",pk,autoincrement"`
	Email         string `bun:"email,unique"`
	Nick          string `bun:"nick,notnull"`
	Bio           string `bun:"bio"`
}

func r3Scan(t *testing.T, dialect string, model interface{}) []TableSchema {
	t.Helper()
	r := NewRegistry()
	r.Register(model)
	tables, err := ScanModels(r, dialect)
	if err != nil {
		t.Fatal(err)
	}
	return tables
}

// SET NOT NULL 與 ADD UNIQUE 會在既有資料上失敗，要標成 risky；放寬（DROP NOT NULL）不算。
// 歷史 bug：兩者不需要任何旗標，MySQL 上中途失敗時前面的陳述式已生效而無法回滾
func TestRiskyChangesAreFlagged(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectMySQL} {
		snap := &Snapshot{Version: SnapshotVersion, Dialect: dialect, Tables: map[string]TableSchema{}}
		for _, ts := range r3Scan(t, dialect, (*r3Before)(nil)) {
			snap.Tables[ts.Name] = ts
		}
		risky := map[string]string{}
		for _, c := range Diff(r3Scan(t, dialect, (*r3After)(nil)), snap) {
			if c.Destructive() != "" {
				t.Errorf("%s: %s should not be destructive: %s", dialect, c, c.Destructive())
			}
			risky[c.String()] = c.Risky()
		}
		for change, wantRisky := range map[string]bool{
			"ALTER COLUMN r3_users.nick":  true,  // SET NOT NULL
			"ADD UNIQUE r3_users (email)": true,  // ADD UNIQUE
			"ALTER COLUMN r3_users.bio":   false, // DROP NOT NULL
		} {
			reason, found := risky[change]
			if !found || (reason != "") != wantRisky {
				t.Errorf("%s: %s: risky = %q (found %v), want risky = %v", dialect, change, reason, found, wantRisky)
			}
		}
	}
}

// decimal 加寬不算破壞性；整數位或小數位變少才是。
// 歷史 bug：decimal(10,2) → decimal(12,2) 被判成可能截斷，只能加 --allow-destructive
func TestDecimalWidening(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		widens   bool
	}{
		{"decimal(10,2)", "decimal(12,2)", true},
		{"DECIMAL(10,2)", "numeric(12, 4)", true},
		{"numeric(10)", "numeric(12)", true},
		{"decimal(10,2)", "decimal(10,2)", true},
		{"decimal(10,2)", "decimal(12,4)", true},
		{"decimal(10,2)", "decimal(10,4)", false}, // 整數位 8 → 6
		{"decimal(10,2)", "decimal(8,2)", false},
		{"decimal(10,4)", "decimal(12,2)", false}, // 小數位變少
		{"decimal(10,2)", "integer", false},
	} {
		if got := typeWidens(tc.from, tc.to); got != tc.widens {
			t.Errorf("typeWidens(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.widens)
		}
	}
}

type r3Model struct {
	bun.BaseModel `bun:"table:r3_things"`
	ID            int64     `bun:",pk,autoincrement"`
	At            time.Time `bun:"at"`
}

// 單一 model 可依方言掃描；driver 對應到方言。
// 歷史 bug：ScanModel 寫死 PostgreSQL，MySQL 專案的 manifest 寫著 BIGSERIAL、TIMESTAMPTZ
func TestScanModelForDialect(t *testing.T) {
	types := func(dialect string) string {
		ts := ScanModelFor((*r3Model)(nil), dialect)
		if ts == nil {
			t.Fatalf("ScanModelFor(%s) = nil", dialect)
		}
		var out []string
		for _, c := range ts.Columns {
			out = append(out, c.SQLType)
		}
		return strings.Join(out, ",")
	}
	if pg, my := types(DialectPostgres), types(DialectMySQL); pg != "BIGSERIAL,TIMESTAMPTZ" || my != "BIGINT,DATETIME" {
		t.Errorf("postgres = %s, mysql = %s", pg, my)
	}
	if ScanModelFor((*r3Model)(nil), "oracle") != nil || ScanModel((*r3Model)(nil)).Columns[0].SQLType != "BIGSERIAL" {
		t.Error("unknown dialect should give nil; ScanModel stays PostgreSQL")
	}
	for driver, want := range map[string]string{
		"mysql": DialectMySQL, "MariaDB": DialectMySQL, " tidb ": DialectMySQL,
		"postgres": DialectPostgres, "pg": DialectPostgres, "": DialectPostgres, "redis": DialectPostgres,
	} {
		if got := DialectForDriver(driver); got != want {
			t.Errorf("DialectForDriver(%q) = %s, want %s", driver, got, want)
		}
	}
}

// SaveSnapshot 先寫暫存檔再換上，不留下 .tmp；目的地不可寫時原本的快照不動
func TestSaveSnapshotIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")
	tables := r3Scan(t, DialectPostgres, (*r3Model)(nil))
	if err := SaveSnapshot(path, tables, DialectPostgres); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "snap.json" {
		t.Errorf("directory holds %v, want only snap.json", entries)
	}
	if snap, err := LoadSnapshot(path); err != nil || len(snap.Tables) != 1 {
		t.Errorf("snapshot = %+v, %v", snap, err)
	}

	// 目的地是目錄：換不上去，回錯且不留暫存檔
	blocked := filepath.Join(dir, "blocked")
	os.Mkdir(blocked, 0o755)
	os.WriteFile(filepath.Join(blocked, "keep"), nil, 0o644)
	if err := SaveSnapshot(blocked, tables, DialectPostgres); err == nil {
		t.Error("saving over a directory should fail")
	}
	if _, err := os.Stat(blocked + ".tmp"); err == nil {
		t.Error("the temporary file was left behind")
	}
}
