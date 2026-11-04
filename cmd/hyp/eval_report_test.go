package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func historyLine(route, status string, scores string, attempts int) string {
	return fmt.Sprintf(`{"route":%q,"status":%q,"scores":{%s},"attempts":%d}`, route, status, scores, attempts)
}

// reportRow 回傳報告中該路由那一列的欄位
func reportRow(t *testing.T, out, route string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, route+" ") {
			return strings.Join(strings.Fields(strings.TrimPrefix(line, route)), " ")
		}
	}
	t.Fatalf("no row for %s in:\n%s", route, out)
	return ""
}

func runReport(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "h.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runHypOutput(t, "eval-report", "--path", path)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func statuses(route, seq string) []string {
	var lines []string
	for _, c := range seq {
		if c == 'P' {
			lines = append(lines, historyLine(route, "pass", `"contract":1`, 1))
		} else {
			lines = append(lines, historyLine(route, "fail", `"contract":0`, 1))
		}
	}
	return lines
}

// 語意品質下滑要看得到：先前只看 contract，報告顯示 1.00 stable
func TestEvalReportUsesAggregateScore(t *testing.T) {
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, historyLine("POST /llm", "pass", `"contract":1,"aggregate":0.9`, 1))
	}
	for i := 0; i < 5; i++ {
		lines = append(lines, historyLine("POST /llm", "pass", `"contract":1,"aggregate":0.3`, 1))
	}
	out := runReport(t, lines...)
	if got, want := reportRow(t, out, "POST /llm"), "0.30 0.90 1.00 ↓ -0.60 ⚠ 10"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
	if !strings.Contains(out, "Contract") {
		t.Errorf("header:\n%s", out)
	}
}

func TestEvalReportFlaky(t *testing.T) {
	for route, want := range map[string]bool{
		"FFPPP": false, // 修好一次
		"PPPFF": false, // 退化一次
		"PPPPP": false,
		"PFPFP": true, // 真正不穩定
		"PPFPP": true,
	} {
		out := runReport(t, statuses("GET /x", route)...)
		if got := strings.Contains(reportRow(t, out, "GET /x"), "⚡"); got != want {
			t.Errorf("%s: flaky = %v, want %v", route, got, want)
		}
	}

	// 全部通過，但有一筆靠重試：不穩定
	lines := statuses("GET /x", "PPPP")
	lines = append(lines, historyLine("GET /x", "pass", `"contract":1`, 2))
	if out := runReport(t, lines...); !strings.Contains(reportRow(t, out, "GET /x"), "⚡") {
		t.Errorf("retried run not flagged:\n%s", out)
	}

	// 翻轉發生在視窗之外不算
	if out := runReport(t, statuses("GET /x", "PFPFPPPPPP")...); strings.Contains(reportRow(t, out, "GET /x"), "⚡") {
		t.Errorf("flips outside the window flagged:\n%s", out)
	}
}

// 前期不滿 N 筆就不比較：先前 6 筆的前期只有 1 筆，一次失敗就顯示 ↑ +1.00
func TestEvalReportNeedsFullPreviousWindow(t *testing.T) {
	for seq, want := range map[string]string{
		"FPPPPP":     "1.00 N/A 1.00 → new 6",
		"FFFFPPPPP":  "1.00 N/A 1.00 → new 9",
		"FFFFFPPPPP": "1.00 0.00 1.00 ↑ +1.00 10",
		"PPP":        "1.00 N/A 1.00 → new 3",
	} {
		out := runReport(t, statuses("GET /x", seq)...)
		if got := reportRow(t, out, "GET /x"); got != want {
			t.Errorf("%s: row = %q, want %q", seq, got, want)
		}
	}
}

func TestEvalReportOldRecordsWithoutScores(t *testing.T) {
	out := runReport(t, `{"route":"GET /old","status":"pass"}`, `{"route":"GET /old","status":"fail"}`)
	if got, want := reportRow(t, out, "GET /old"), "0.50 N/A 0.50 → new 2"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
}

func TestEvalReportWarnsAboutMalformedLines(t *testing.T) {
	out := runReport(t, `{"route":"GET /a","status":"pass"}`, `{broken`, `{}`)
	if !strings.Contains(out, "skipped 2 malformed") {
		t.Errorf("output:\n%s", out)
	}
	if out := runReport(t, `{"route":"GET /a","status":"pass"}`); strings.Contains(out, "malformed") {
		t.Errorf("false warning:\n%s", out)
	}
}

func TestEvalReportPaths(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "routers")
	if err := os.MkdirAll(filepath.Join(root, ".hyp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	// 預設路徑還沒有紀錄：不是錯誤
	out, err := runHypOutput(t, "eval-report")
	if err != nil || !strings.Contains(out, "尚無 eval 記錄") {
		t.Errorf("no history yet: %q, %v", out, err)
	}

	// 明確指定的路徑不存在：是打錯字
	if _, err := runHypOutput(t, "eval-report", "--path", "nope.jsonl"); err == nil {
		t.Error("missing --path should fail")
	}

	// 在子目錄也讀得到模組根目錄的紀錄
	hist := filepath.Join(root, ".hyp", "eval_history.jsonl")
	if err := os.WriteFile(hist, []byte(`{"route":"GET /a","status":"pass"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runHypOutput(t, "eval-report")
	if err != nil || !strings.Contains(out, "GET /a") {
		t.Errorf("from a subdirectory: %q, %v", out, err)
	}

	if _, err := runHypOutput(t, "eval-report", "--n", "0"); err == nil {
		t.Error("--n 0 should fail")
	}

	// hyp observe 同樣找模組根目錄；不相干的檔案不算報告
	for _, name := range []string{"observe_1.html", "observe_note.txt", "other.html"} {
		if err := os.WriteFile(filepath.Join(root, ".hyp", name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := findObserveReports()
	if err != nil || len(reports) != 1 || reports[0] != filepath.Join(root, ".hyp", "observe_1.html") {
		t.Errorf("reports = %v, err = %v", reports, err)
	}
}
