package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/maoxiaoyue/hypgo/pkg/hidb"
)

// MySQL 的錯誤碼對應到 hidb 的錯誤分類
func TestClassifyMySQLErrors(t *testing.T) {
	for number, want := range map[uint16]error{
		1062: hidb.ErrDuplicate, 1451: hidb.ErrForeignKey, 1452: hidb.ErrForeignKey, 1213: hidb.ErrDeadlock,
	} {
		err := hidb.Classify(fmt.Errorf("exec: %w", &driver.MySQLError{Number: number, Message: "boom"}))
		var me *driver.MySQLError
		if !errors.Is(err, want) || !errors.As(err, &me) {
			t.Errorf("error %d: %v, want %v with the original error", number, err, want)
		}
	}
	other := &driver.MySQLError{Number: 1064, Message: "syntax error"}
	if got := hidb.Classify(other); got != error(other) {
		t.Errorf("an unrelated error was classified: %v", got)
	}
}

// DSN 沒有 parseTime=true 時提示。
// 沒有它，DATETIME 欄位掃進 time.Time 會失敗，要到第一個含時間欄位的查詢才發現
func TestCheckDSNParseTime(t *testing.T) {
	d := New()
	if hints := d.CheckDSN("user:pw@tcp(127.0.0.1:3306)/app"); len(hints) != 1 || !strings.Contains(hints[0], "parseTime") {
		t.Errorf("missing parseTime: hints = %v", hints)
	}
	for _, ok := range []string{"user:pw@tcp(127.0.0.1:3306)/app?parseTime=true", "user:pw@tcp(h)/app?charset=utf8mb4&parseTime=True", "not a dsn"} {
		if hints := d.CheckDSN(ok); len(hints) != 0 {
			t.Errorf("%q: unexpected hints %v", ok, hints)
		}
	}
	if strings.Contains(strings.Join(d.CheckDSN("user:secret@tcp(h)/app"), " "), "secret") {
		t.Error("the hint must not echo the DSN")
	}
}

var (
	_ hidb.LagProber  = (*Dialect)(nil)
	_ hidb.DSNChecker = (*Dialect)(nil)
)

// 對真實的 MySQL 量複寫延遲。預設跳過；設定 HYPGO_TEST_MYSQL_DSN 後執行，建議指向副本或測試環境。
// 只執行 SHOW REPLICA STATUS（舊版為 SHOW SLAVE STATUS），帳號需要 REPLICATION CLIENT 權限；不寫入、不改結構
func TestReplicaLagAgainstRealMySQL(t *testing.T) {
	dsn := os.Getenv("HYPGO_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set HYPGO_TEST_MYSQL_DSN to run (read-only)")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lag, err := New().ReplicaLag(ctx, db)
	if err != nil {
		t.Fatalf("ReplicaLag: %v", err)
	}
	t.Logf("lag = %s (0 on a server that is not a replica)", lag)
}
