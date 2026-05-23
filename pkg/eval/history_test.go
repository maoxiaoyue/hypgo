package eval

// @ai purpose: history_test — 驗證 EvalRecord 序列化、AppendHistory 原子寫入、LoadHistory 解析、HashInput 與 GitShortCommit
// @ai input: *testing.T
// @ai output: 無
// @ai sideeffect: 在測試暫存目錄中建立 eval_history.jsonl 與 .tmp 暫存檔
// date: 2026-05-23

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── DefaultHistoryPath ───────────────────────────────────────────────────────

func TestDefaultHistoryPath(t *testing.T) {
	p := DefaultHistoryPath()
	if !strings.Contains(p, "eval_history.jsonl") {
		t.Errorf("DefaultHistoryPath = %q, should contain eval_history.jsonl", p)
	}
	if !strings.Contains(p, ".hyp") {
		t.Errorf("DefaultHistoryPath = %q, should contain .hyp directory", p)
	}
}

// ─── HashInput ────────────────────────────────────────────────────────────────

func TestHashInput_Empty(t *testing.T) {
	if got := HashInput(""); got != "" {
		t.Errorf("HashInput(\"\") = %q, want empty string", got)
	}
}

func TestHashInput_NonEmpty(t *testing.T) {
	got := HashInput(`{"name":"test"}`)
	if !strings.HasPrefix(got, "sha256:") {
		t.Errorf("HashInput should start with sha256:, got %q", got)
	}
	// sha256: + 8 bytes * 2 hex chars = sha256: + 16 chars = 23 chars total
	if len(got) != 23 {
		t.Errorf("HashInput length = %d, want 23", len(got))
	}
}

func TestHashInput_Deterministic(t *testing.T) {
	input := `{"key":"value"}`
	h1 := HashInput(input)
	h2 := HashInput(input)
	if h1 != h2 {
		t.Errorf("HashInput not deterministic: %q != %q", h1, h2)
	}
}

func TestHashInput_DifferentInputsDifferentHashes(t *testing.T) {
	h1 := HashInput(`{"a":1}`)
	h2 := HashInput(`{"a":2}`)
	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}

// ─── GitShortCommit ───────────────────────────────────────────────────────────

func TestGitShortCommit_DoesNotPanic(t *testing.T) {
	// 不論是否在 git 倉庫中，都不應該 panic
	got := GitShortCommit()
	// 若在 git 倉庫中，長度應為 7；若不在則為空字串
	if len(got) != 0 && len(got) != 7 {
		t.Logf("GitShortCommit = %q (length %d, expected 0 or 7)", got, len(got))
	}
}

// ─── AppendHistoryTo ─────────────────────────────────────────────────────────

func TestAppendHistory_CreatesDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "eval.jsonl")

	record := EvalRecord{
		Timestamp: time.Now().UTC(),
		Route:     "GET /test",
		Status:    "pass",
		LatencyMs: 5,
	}

	if err := AppendHistoryTo(path, record); err != nil {
		t.Fatalf("AppendHistoryTo failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("history file should exist after append: %v", err)
	}
}

func TestAppendHistory_SingleRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eval.jsonl")

	record := EvalRecord{
		Timestamp:  time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC),
		GitCommit:  "a3f8b21",
		Route:      "POST /api/users",
		Status:     "pass",
		Scores:     map[string]float64{"contract": 1.0},
		LatencyMs:  42,
		InputHash:  HashInput(`{"name":"test"}`),
		FailReason: "",
	}

	if err := AppendHistoryTo(path, record); err != nil {
		t.Fatalf("AppendHistoryTo failed: %v", err)
	}

	records, err := LoadHistory(path)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	got := records[0]
	if got.Route != "POST /api/users" {
		t.Errorf("Route = %q, want POST /api/users", got.Route)
	}
	if got.Status != "pass" {
		t.Errorf("Status = %q, want pass", got.Status)
	}
	if got.GitCommit != "a3f8b21" {
		t.Errorf("GitCommit = %q, want a3f8b21", got.GitCommit)
	}
	if got.LatencyMs != 42 {
		t.Errorf("LatencyMs = %d, want 42", got.LatencyMs)
	}
	if got.Scores["contract"] != 1.0 {
		t.Errorf("Scores[contract] = %f, want 1.0", got.Scores["contract"])
	}
}

func TestAppendHistory_MultipleRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eval.jsonl")

	routes := []string{"GET /a", "POST /b", "DELETE /c"}
	for _, route := range routes {
		record := EvalRecord{
			Timestamp: time.Now().UTC(),
			Route:     route,
			Status:    "pass",
			LatencyMs: 10,
		}
		if err := AppendHistoryTo(path, record); err != nil {
			t.Fatalf("AppendHistoryTo(%q) failed: %v", route, err)
		}
	}

	records, err := LoadHistory(path)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
	for i, rec := range records {
		if rec.Route != routes[i] {
			t.Errorf("records[%d].Route = %q, want %q", i, rec.Route, routes[i])
		}
	}
}

func TestAppendHistory_NoTmpFileRemains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eval.jsonl")

	if err := AppendHistoryTo(path, EvalRecord{
		Timestamp: time.Now().UTC(),
		Route:     "GET /ok",
		Status:    "pass",
	}); err != nil {
		t.Fatal(err)
	}

	// .tmp 暫存檔不應遺留
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp file should not remain after successful append")
	}
}

// ─── AppendHistory 並行安全 ────────────────────────────────────────────────────

func TestAppendHistory_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eval_concurrent.jsonl")

	const goroutines = 10
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			record := EvalRecord{
				Timestamp: time.Now().UTC(),
				Route:     "GET /concurrent",
				Status:    "pass",
				LatencyMs: int64(n),
			}
			if err := AppendHistoryTo(path, record); err != nil {
				t.Errorf("goroutine %d: AppendHistoryTo failed: %v", n, err)
			}
		}(i)
	}

	wg.Wait()

	records, err := LoadHistory(path)
	if err != nil {
		t.Fatalf("LoadHistory after concurrent writes failed: %v", err)
	}
	if len(records) != goroutines {
		t.Errorf("expected %d records after concurrent writes, got %d", goroutines, len(records))
	}
}

// ─── LoadHistory ──────────────────────────────────────────────────────────────

func TestLoadHistory_NonExistentFile(t *testing.T) {
	records, err := LoadHistory(filepath.Join(t.TempDir(), "nonexistent.jsonl"))
	if err != nil {
		t.Errorf("LoadHistory non-existent file should not error, got: %v", err)
	}
	if records != nil {
		t.Errorf("LoadHistory non-existent should return nil slice, got %v", records)
	}
}

func TestLoadHistory_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := LoadHistory(path)
	if err != nil {
		t.Errorf("LoadHistory empty file should not error, got: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("LoadHistory empty file should return 0 records, got %d", len(records))
	}
}

func TestLoadHistory_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "malformed.jsonl")

	content := `{"ts":"2026-05-23T14:00:00Z","route":"GET /ok","status":"pass","latency_ms":10}
NOT_VALID_JSON
{"ts":"2026-05-23T14:01:00Z","route":"POST /ok2","status":"fail","latency_ms":20}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := LoadHistory(path)
	if err != nil {
		t.Fatalf("LoadHistory should not error on malformed line, got: %v", err)
	}
	// 只有 2 條有效記錄（中間那條被跳過）
	if len(records) != 2 {
		t.Errorf("expected 2 valid records (skip malformed), got %d", len(records))
	}
}

func TestLoadHistory_PreservesOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order.jsonl")

	routes := []string{"GET /first", "POST /second", "DELETE /third"}
	for _, r := range routes {
		if err := AppendHistoryTo(path, EvalRecord{
			Timestamp: time.Now().UTC(),
			Route:     r,
			Status:    "pass",
		}); err != nil {
			t.Fatal(err)
		}
	}

	records, err := LoadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range records {
		if r.Route != routes[i] {
			t.Errorf("record[%d].Route = %q, want %q", i, r.Route, routes[i])
		}
	}
}

// ─── EvalRecord 欄位序列化 ─────────────────────────────────────────────────────

func TestEvalRecord_OmitEmptyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "omit.jsonl")

	record := EvalRecord{
		Timestamp: time.Now().UTC(),
		Route:     "GET /minimal",
		Status:    "pass",
		LatencyMs: 1,
		// GitCommit、InputHash、FailReason、Scores 全部為空/nil → omitempty
	}
	if err := AppendHistoryTo(path, record); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	line := strings.TrimSpace(string(data))

	// omitempty 欄位不應出現在 JSON 中
	if strings.Contains(line, "git_commit") {
		t.Error("git_commit should be omitted when empty")
	}
	if strings.Contains(line, "input_hash") {
		t.Error("input_hash should be omitted when empty")
	}
	if strings.Contains(line, "fail_reason") {
		t.Error("fail_reason should be omitted when empty")
	}
	if strings.Contains(line, "scores") {
		t.Error("scores should be omitted when nil")
	}
}
