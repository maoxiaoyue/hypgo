package lintdeep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// analyzeEdge 只載入一次：fixture 涵蓋所有案例，個別測試各看一部分
func analyzeEdge(t *testing.T) *Report {
	t.Helper()
	rep, err := Analyze("testdata/edge", ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.LoadErrors) > 0 {
		t.Fatalf("fixture should compile: %v", rep.LoadErrors)
	}
	return rep
}

func findingKinds(rep *Report) map[string]string {
	kinds := map[string]string{}
	for _, f := range rep.Findings {
		kinds[f.Route] = f.Kind
	}
	return kinds
}

func skippedRoutes(rep *Report) map[string]string {
	routes := map[string]string{}
	for _, s := range rep.Skipped {
		routes[s.Route] = s.Reason
	}
	return routes
}

func TestEdgeFindings(t *testing.T) {
	kinds := findingKinds(analyzeEdge(t))
	want := map[string]string{
		// 常數 Method：先前變空字串，no_bindinput 永遠不觸發
		"POST /const-method": KindNoBindInput,
		// 常數組合的 Path：先前路由名稱遺失
		"POST /const/wrong":         KindInputMismatch,
		"POST /lower":               KindNoBindInput,
		"POST (dynamic path)":       KindNoBindInput,
		"POST /registered":          KindInputMismatch,
		"POST /helper-wrong":        KindInputMismatch,
		"POST /loop":                KindNoBindInput,
		"POST /std-context":         KindNoBindInput,
		"POST /map":                 KindInputMismatch, // 先前訊息是「未呼叫」
		"POST /gen-wrong":           KindInputMismatch, // 先前因泛型 receiver 被略過
		"POST /api/v1/grp":          KindInputMismatch,
		"POST /api/v1/admin/nested": KindInputMismatch,
		"POST /inline/x":            KindInputMismatch,
		"POST /param-group":         KindInputMismatch, // 參數傳入的 group 解析不到
		"POST /reassigned":          KindInputMismatch, // 變數值不唯一
	}
	for route, kind := range want {
		if kinds[route] != kind {
			t.Errorf("%s: got %q, want %q", route, kinds[route], kind)
		}
	}
	for route, kind := range kinds {
		if _, ok := want[route]; !ok {
			t.Errorf("unexpected finding %s: %s", route, kind)
		}
	}
}

func TestEdgeSkipped(t *testing.T) {
	rep := analyzeEdge(t)
	routes := skippedRoutes(rep)
	for _, route := range []string{
		"? /var-method", "POST /generic-helper", "POST /interface", "POST /method-value",
		"POST /any-input", "POST /gen-t", "POST /factory",
	} {
		if _, ok := routes[route]; !ok {
			t.Errorf("%s should be skipped; skipped = %v", route, routes)
		}
	}

	// 變數 Route、表格、分開存的 SchemaRoute、RegisterSchema(變數)：先前完全看不到
	nonLiteral := 0
	for _, s := range rep.Skipped {
		if strings.HasPrefix(s.Route, "(non-literal route") {
			nonLiteral++
		}
	}
	if nonLiteral != 4 {
		t.Errorf("non-literal routes skipped = %d, want 4: %+v", nonLiteral, rep.Skipped)
	}
	if len(rep.Skipped) != 7+4 {
		t.Errorf("skipped = %d, want 11: %+v", len(rep.Skipped), rep.Skipped)
	}
}

// 這些路由不該有任何回報，而且要算進「已分析」
func TestEdgeCorrectRoutesPass(t *testing.T) {
	rep := analyzeEdge(t)
	kinds := findingKinds(rep)
	skipped := skippedRoutes(rep)
	for _, route := range []string{
		"POST /helper", "POST /chain", "POST /handler-arg", "POST /alias", "POST /ptr-alias",
		"POST /alias-input", "POST /pointer-input", "POST /slice", "POST /nil-input",
		"GET /get-unknown",
	} {
		if k, ok := kinds[route]; ok {
			t.Errorf("%s reported as %s", route, k)
		}
		if r, ok := skipped[route]; ok {
			t.Errorf("%s skipped: %s", route, r)
		}
	}
	// 15 條 finding + 9 條通過（/nil-input 沒有 Input，不計入）
	if rep.Analyzed != 15+9 {
		t.Errorf("Analyzed = %d, want 24", rep.Analyzed)
	}
}

// 編譯錯誤要進 LoadErrors，而且不能印 ✓
func TestBrokenPackageReportsLoadErrors(t *testing.T) {
	rep, err := Analyze("testdata/broken", ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.LoadErrors) == 0 {
		t.Fatal("compile error not reported")
	}
	if out := Format(rep); strings.Contains(out, "✓") {
		t.Errorf("green check despite load errors:\n%s", out)
	}
}

// 目錄存在但沒有 Go 檔：go list 只警告，不能算通過
func TestNoPackagesIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(dir); err == nil {
		t.Error("a pattern matching no packages should be an error")
	}
}

func TestFormatGreenCheckOnlyWhenSomethingWasVerified(t *testing.T) {
	cases := []struct {
		name  string
		rep   Report
		green bool
	}{
		{"nothing to check", Report{}, false},
		{"only skipped", Report{Skipped: []Skipped{{Route: "POST /s"}}}, false},
		{"load errors", Report{Analyzed: 3, LoadErrors: []string{"boom"}}, false},
		{"findings", Report{Analyzed: 3, Findings: []Finding{{Route: "POST /f"}}}, false},
		{"verified", Report{Analyzed: 3}, true},
	}
	for _, c := range cases {
		if got := strings.Contains(Format(&c.rep), "✓"); got != c.green {
			t.Errorf("%s: green = %v, want %v", c.name, got, c.green)
		}
	}
}
