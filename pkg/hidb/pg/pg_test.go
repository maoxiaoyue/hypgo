package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/maoxiaoyue/hypgo/pkg/hidb"
)

// PostgreSQL 的 SQLSTATE 對應到 hidb 的錯誤分類
func TestClassifyPostgresErrors(t *testing.T) {
	for code, want := range map[pq.ErrorCode]error{
		"23505": hidb.ErrDuplicate, "23503": hidb.ErrForeignKey,
		"40001": hidb.ErrDeadlock, "40P01": hidb.ErrDeadlock,
	} {
		err := hidb.Classify(fmt.Errorf("exec: %w", &pq.Error{Code: code, Message: "boom"}))
		var pe *pq.Error
		if !errors.Is(err, want) || !errors.As(err, &pe) {
			t.Errorf("SQLSTATE %s: %v, want %v with the original error", code, err, want)
		}
	}
	other := &pq.Error{Code: "42601", Message: "syntax error"}
	if got := hidb.Classify(other); got != error(other) {
		t.Errorf("an unrelated SQLSTATE was classified: %v", got)
	}
}

var _ hidb.LagProber = (*Dialect)(nil)

// 對真實的 PostgreSQL 量複寫延遲。預設跳過；設定 HYPGO_TEST_PG_DSN 後執行，建議指向副本或測試環境。
// 只執行一個唯讀查詢（pg_is_in_recovery / pg_last_wal_*_lsn / pg_last_xact_replay_timestamp），不寫入、不改結構
func TestReplicaLagAgainstRealPostgres(t *testing.T) {
	dsn := os.Getenv("HYPGO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set HYPGO_TEST_PG_DSN to run (read-only)")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lag, err := New().ReplicaLag(ctx, db)
	if err != nil || lag < 0 {
		t.Fatalf("ReplicaLag = %v, %v", lag, err)
	}
	var inRecovery bool
	db.QueryRowContext(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery)
	t.Logf("replica = %v, lag = %s", inRecovery, lag)
}
