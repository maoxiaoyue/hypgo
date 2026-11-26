package lintdeep

import (
	"path/filepath"
	"strings"
	"testing"
)

func analyzeSecond(t *testing.T) *Report {
	t.Helper()
	rep, err := Analyze("testdata/second", ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.LoadErrors) > 0 {
		t.Fatalf("fixture should compile: %v", rep.LoadErrors)
	}
	return rep
}

// 明確綁錯型別的 handler 另有追不到的呼叫時仍然回報；綁定寫在前面的 handler 也算；
// 套件層級的函式變數可以解析。
// 歷史 bug：(1) 只要有追不到的呼叫整條列為略過，綁錯型別的 handler 多呼叫一個函式變數或介面方法
// 就通過了；(2) 只看 Handle 的最後一個參數，Handle(bind, create) 被報成 no_bindinput；
// (3) var h = func(c) {...} 一律略過
func TestSecondRoundFindings(t *testing.T) {
	rep := analyzeSecond(t)
	kinds := findingKinds(rep)
	want := map[string]string{
		"POST /wrong-func-var":  KindInputMismatch,
		"POST /wrong-interface": KindInputMismatch,
		"POST /mw-binds-wrong":  KindInputMismatch,
		"POST /mw-factory":      KindNoBindInput,
		"POST /var-wrong":       KindInputMismatch,
		"GET /get-wrong":        KindInputMismatch,
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
	for _, f := range rep.Findings {
		if f.Route == "POST /wrong-func-var" && !strings.Contains(f.Message, "無法追蹤") {
			t.Errorf("mismatch next to an untraceable call should say so: %s", f.Message)
		}
		if f.Route == "POST /var-wrong" && strings.Contains(f.Message, "無法追蹤") {
			t.Errorf("no untraceable call here: %s", f.Message)
		}
	}

	skipped := skippedRoutes(rep)
	for _, route := range []string{"POST /wrong-any-bind", "POST /last-factory", "POST /var-swapped"} {
		if _, ok := skipped[route]; !ok {
			t.Errorf("%s should be skipped; skipped = %v", route, skipped)
		}
	}
	if len(skipped) != 3 {
		t.Errorf("skipped = %v, want 3 routes", skipped)
	}
	// 6 條 finding + 4 條通過（/mw-binds、/mw-factory-binds、/var-right、GET /get-bound）
	if rep.Analyzed != 6+4 {
		t.Errorf("Analyzed = %d, want 10", rep.Analyzed)
	}
}

// GET / DELETE 宣告了 Input 而沒有綁定：資訊級，不算進已驗證的數量。
// 歷史 bug：不回報，卻計入「✓ 已分析 N 條，handler 綁定皆對齊宣告」
func TestUnboundInputOnReadOnlyMethodsIsInfo(t *testing.T) {
	rep := analyzeSecond(t)
	got := map[string]string{}
	for _, f := range rep.Info {
		got[f.Route] = f.Kind
	}
	if len(got) != 2 || got["GET /get-unbound"] != KindInputUnbound || got["DELETE /del-unbound"] != KindInputUnbound {
		t.Errorf("info = %v", got)
	}
	// 把 c 傳給追不到的函式的 GET：不能說它沒綁定，也沒有東西可驗證
	if _, ok := skippedRoutes(rep)["GET /get-unknown"]; ok {
		t.Error("GET /get-unknown skipped")
	}
	out := Format(rep)
	if !strings.Contains(out, "ℹ [deep:input_unbound] GET /get-unbound") || !strings.Contains(out, "資訊 2 個") {
		t.Errorf("report:\n%s", out)
	}
	// 只有資訊級的結果時不能說「沒有可檢查的路由」，也不能印 ✓
	only := Format(&Report{Info: rep.Info})
	if strings.Contains(only, "✓") || strings.Contains(only, "沒有可檢查的路由") {
		t.Errorf("info-only report:\n%s", only)
	}
}

// 位置是相對於載入目錄的路徑。歷史 bug：一律印絕對路徑
func TestPositionsAreRelative(t *testing.T) {
	rep := analyzeSecond(t)
	var all []string
	for _, f := range rep.Findings {
		all = append(all, f.Pos)
	}
	for _, f := range rep.Info {
		all = append(all, f.Pos)
	}
	for _, s := range rep.Skipped {
		all = append(all, s.Pos)
	}
	if len(all) == 0 {
		t.Fatal("nothing reported")
	}
	for _, pos := range all {
		if filepath.IsAbs(pos) || !strings.HasPrefix(pos, "handlers.go:") {
			t.Errorf("position %q is not relative to the analysed directory", pos)
		}
	}
}
