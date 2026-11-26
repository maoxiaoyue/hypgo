package testgen

import (
	"bytes"
	"errors"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Test 之後的第一個字母不能是小寫：未匯出的名稱前面加底線。
// 歷史 bug：未匯出型別上的匯出方法產生 Testserver_ServeHTTP、--include-unexported 產生 Testadd，
// go vet 擋下（malformed name），整個套件的測試編不過
func TestUnexportedNamesGetAnUnderscore(t *testing.T) {
	src := `package p

type server struct{}

func (s *server) ServeHTTP(code int) int { return code }

type Public struct{}

func (p Public) Name() string { return "x" }

func (p Public) reset() {}

func add(a, b int) int { return a + b }
`
	dir := writePkg(t, map[string]string{"p.go": src})
	if got, want := added(t, dir), []string{"TestPublic_Name", "Test_server_ServeHTTP"}; !slices.Equal(got, want) {
		t.Errorf("default: added %v, want %v", got, want)
	}

	dir = writePkg(t, map[string]string{"p.go": src})
	opts := DefaultOptions()
	opts.IncludeUnexported = true
	rep, err := GenerateForFile(filepath.Join(dir, "p.go"), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range rep.Added {
		if r := name[len("Test")]; r >= 'a' && r <= 'z' {
			t.Errorf("%s: a lowercase letter right after Test", name)
		}
	}
	if !slices.Contains(rep.Added, "Test_add") || !slices.Contains(rep.Added, "TestPublic_reset") {
		t.Errorf("added %v", rep.Added)
	}
	typecheck(t, dir, "p")
}

// 手寫的測試用首字母大寫的寫法（add → TestAdd）時也算已有測試；與骨架的區域變數同名的函式退回空殼
func TestUnexportedExistingTestsAndShadowedNames(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"p.go":      "package p\n\nfunc add(a, b int) int { return a + b }\n\nfunc tests(n int) int { return n }\n",
		"p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestAddWorks(t *testing.T) {}\n",
	})
	opts := DefaultOptions()
	opts.IncludeUnexported = true
	rep, err := GenerateForFile(filepath.Join(dir, "p.go"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Added, []string{"Test_tests"}) || len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0], "TestAddWorks") {
		t.Errorf("added %v, kept %v", rep.Added, rep.Skipped)
	}
	typecheck(t, dir, "p") // 歷史 bug：骨架的 tests := ... 遮住函式 tests，呼叫不到
}

// 原始檔的 build 條件帶進新的測試檔；//go:build ignore 的檔案不補測試；既有測試檔的條件不同時不追加。
// 歷史 bug：//go:build linux 的原始檔對應沒有條件的測試檔（其他平台 undefined）；
// ignore 的 package main 產生 gen_test.go，套件變成 found packages x and main
func TestBuildConstraints(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"plat.go":        "//go:build linux && amd64\n\npackage p\n\nfunc LinuxOnly(n int) int { return n }\n",
		"old.go":         "// +build darwin\n\npackage p\n\nfunc Mac() {}\n",
		"gen.go":         "//go:build ignore\n\npackage main\n\nfunc Generate() {}\n\nfunc main() {}\n",
		"tagged.go":      "//go:build windows\n\npackage p\n\nfunc Win() {}\n",
		"plain.go":       "package p\n\nfunc Plain() {}\n",
		"tagged_test.go": "package p\n\nimport \"testing\"\n\nfunc TestHelperOnly(t *testing.T) {}\n",
	})
	reports, err := GenerateForDir(dir, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "plat_test.go")); !strings.HasPrefix(got, "//go:build linux && amd64\n\n") {
		t.Errorf("plat_test.go does not start with the source's constraint:\n%s", got[:min(len(got), 120)])
	}
	if got := readFile(t, filepath.Join(dir, "old_test.go")); !strings.HasPrefix(got, "//go:build darwin\n\n") {
		t.Errorf("old-style constraint not carried over:\n%s", got[:min(len(got), 120)])
	}
	if got := readFile(t, filepath.Join(dir, "plain_test.go")); strings.Contains(got, "go:build") {
		t.Error("a constraint appeared in the test of an unconstrained file")
	}
	if _, err := os.Stat(filepath.Join(dir, "gen_test.go")); err == nil {
		t.Error("a test file was created for a //go:build ignore file")
	}
	var mismatch []string
	for _, r := range reports {
		mismatch = append(mismatch, r.BuildMismatch...)
	}
	if !slices.Equal(mismatch, []string{"TestWin"}) || strings.Contains(readFile(t, filepath.Join(dir, "tagged_test.go")), "TestWin") {
		t.Errorf("stub appended to a test file with another constraint; BuildMismatch = %v", mismatch)
	}
	if out := FormatReports(reports); !strings.Contains(out, "different build constraint") {
		t.Errorf("report does not mention the skipped stub:\n%s", out)
	}
}

// 補 import 時不能與測試檔已有的名稱衝突；dot import 的型別分不出來源。兩者都退回空殼。
// 歷史 bug：errors redeclared in this block、undefined: Duration
func TestImportConflictsFallBackToStubs(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"e.go":      "package p\n\nimport \"example.com/x/errors\"\n\nfunc Wrap(e *errors.Err) *errors.Err { return e }\n",
		"e_test.go": "package p\n\nimport (\n\t\"errors\"\n\t\"testing\"\n)\n\nfunc TestOther(t *testing.T) { _ = errors.New(\"x\") }\n",
		"d.go":      "package p\n\nimport . \"time\"\n\nfunc Wait(d Duration) Duration { return d }\n",
	})
	if got, want := added(t, dir), []string{"TestWait", "TestWrap"}; !slices.Equal(got, want) {
		t.Fatalf("added %v, want %v", got, want)
	}
	if got := readFile(t, filepath.Join(dir, "e_test.go")); strings.Contains(got, "example.com/x/errors") || strings.Contains(got, "errors.Err") {
		t.Errorf("the conflicting import was added:\n%s", got)
	}
	if got := readFile(t, filepath.Join(dir, "d_test.go")); strings.Contains(got, "Duration") {
		t.Errorf("a table skeleton was generated for a dot-imported type:\n%s", got)
	}
}

// Go 不建置的目錄（. 或 _ 開頭）不寫入；無法處理的目錄以 *SkippedError 回報，其餘照常完成。
// 歷史 bug：.cache、_examples 裡也產生測試檔；測試檔語法錯誤時只在 stderr 印一行，回傳 nil
func TestSkippedDirectoriesAndProblems(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("ok/a.go", "package ok\n\nfunc A() {}\n")
	write(".cache/c.go", "package cache\n\nfunc C() {}\n")
	write("_examples/e.go", "package examples\n\nfunc E() {}\n")
	write("sub/.hidden/h.go", "package hidden\n\nfunc H() {}\n")
	write("broken/b.go", "package broken\n\nfunc B() {}\n")
	write("broken/b_test.go", "package broken\n\nfunc TestX(t *testing.T) {\n")

	reports, err := GenerateForDir(root, DefaultOptions())
	var skipped *SkippedError
	if !errors.As(err, &skipped) || len(skipped.Problems) != 1 || !strings.Contains(skipped.Problems[0], "broken") {
		t.Fatalf("err = %v, want a SkippedError naming the broken directory", err)
	}
	if len(reports) != 1 || !slices.Equal(reports[0].Added, []string{"TestA"}) {
		t.Errorf("the rest was not processed: %+v", reports)
	}
	for _, rel := range []string{".cache/c_test.go", "_examples/e_test.go", "sub/.hidden/h_test.go", "broken/b_test.go.bak"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s was written", rel)
		}
	}
	// 根目錄本身以 . 開頭時照常處理（hyp testgen .）
	dot := filepath.Join(root, ".cache")
	if r, err := GenerateForDir(dot, DefaultOptions()); err != nil || len(r) != 1 {
		t.Errorf("an explicitly named dot directory was skipped: %v, %v", r, err)
	}
}

// 追加到既有測試檔：gofmt 排好的檔案補完仍然是排好的，CRLF 的檔案不留下混合的行尾。
// 歷史 bug：補進去的 import 接在最後（gofmt -l 列出該檔）；CRLF 檔案被追加 LF 的內容
func TestAppendKeepsFormattingAndLineEndings(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"f.go":      "package p\n\nimport (\n\t\"net/http\"\n\t\"time\"\n)\n\nfunc A(d time.Duration) time.Duration { return d }\n\nfunc B(r *http.Request) *http.Request { return r }\n",
		"f_test.go": "package p\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestA(t *testing.T) {\n\t_ = time.Second\n}\n",
	})
	added(t, dir)
	got := []byte(readFile(t, filepath.Join(dir, "f_test.go")))
	if formatted, err := format.Source(got); err != nil || !bytes.Equal(formatted, got) {
		t.Errorf("the test file is no longer gofmt-clean (err %v):\n%s", err, got)
	}
	typecheck(t, dir, "p")

	dir = writePkg(t, map[string]string{
		"c.go":      "package p\r\n\r\nimport \"time\"\r\n\r\nfunc D(d time.Duration) time.Duration { return d }\r\n",
		"c_test.go": "package p\r\n\r\nimport (\r\n\t\"testing\"\r\n)\r\n\r\nfunc TestZ(t *testing.T) {}\r\n",
	})
	added(t, dir)
	got = []byte(readFile(t, filepath.Join(dir, "c_test.go")))
	if lf := bytes.Count(got, []byte("\n")) - bytes.Count(got, []byte("\r\n")); lf != 0 || !bytes.Contains(got, []byte("TestD")) {
		t.Errorf("%d LF-only line(s) in a CRLF file", lf)
	}
}

// 報表的 kept：同名而不同目標的測試各自產生；由名稱相近的測試涵蓋時寫出是哪一個。
// 歷史 bug：方法 Foo.Bar 與函式 Foo_Bar 都對應 TestFoo_Bar，後者被列為 kept 而沒有任何測試
func TestKeptReportsWhichTestCovers(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"c.go":      "package p\n\ntype Foo struct{}\n\nfunc (Foo) Bar() {}\n\nfunc Foo_Bar() {}\n\nfunc Add(n int) int { return n }\n",
		"c_test.go": "package p\n\nimport \"testing\"\n\nfunc TestAddHTTP(t *testing.T) {}\n",
	})
	rep, err := GenerateForFile(filepath.Join(dir, "c.go"), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Added, []string{"TestFoo_Bar", "TestFoo_Bar_2"}) {
		t.Errorf("added %v", rep.Added)
	}
	if !slices.Equal(rep.Skipped, []string{"TestAdd (by TestAddHTTP)"}) {
		t.Errorf("kept %v", rep.Skipped)
	}
	typecheck(t, dir, "p")
	// 重跑不再產生
	if again, _ := GenerateForFile(filepath.Join(dir, "c.go"), DefaultOptions()); len(again.Added) != 0 {
		t.Errorf("second run added %v", again.Added)
	}
}

// 回傳具名的函式型別時不以 reflect.DeepEqual 比對（函式值只有都是 nil 才相等）。
// 歷史 bug：只認字面的 func(...)，http.HandlerFunc 與套件內的 type Handler func(...) 都產生永遠不成立的比對
func TestNamedFuncResultsAreNotCompared(t *testing.T) {
	dir := writePkg(t, map[string]string{"h.go": `package p

import "net/http"

type Handler func(string) error

func Wrap(next http.HandlerFunc) http.HandlerFunc { return next }

func Local(h Handler) Handler { return h }

func Plain(n int) int { return n }
`})
	added(t, dir)
	got := readFile(t, filepath.Join(dir, "h_test.go"))
	if n := strings.Count(got, "reflect.DeepEqual"); n != 1 {
		t.Errorf("%d DeepEqual comparison(s), want 1 (only Plain):\n%s", n, got)
	}
	if strings.Count(got, "func value") != 2 {
		t.Errorf("no TODO for the func results:\n%s", got)
	}
	typecheck(t, dir, "p")
}

// 新檔案的檔頭不寫 "Code generated"：這個檔案是要讓人改的
func TestHeaderDoesNotClaimGenerated(t *testing.T) {
	dir := writePkg(t, map[string]string{"a.go": "package p\n\nfunc A() {}\n"})
	added(t, dir)
	if got := readFile(t, filepath.Join(dir, "a_test.go")); strings.Contains(got, "Code generated") || !strings.Contains(got, "hyp testgen") {
		t.Errorf("header:\n%s", got)
	}
}

// 同時有 add 與 Add 時，TestAdd 是 Add 的測試，不能拿來當成 add 已有測試
func TestAltNameBelongsToTheExportedFunc(t *testing.T) {
	dir := writePkg(t, map[string]string{
		"p.go":      "package p\n\nfunc Add(a, b int) int { return add(a, b) }\n\nfunc add(a, b int) int { return a + b }\n",
		"p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {}\n",
	})
	opts := DefaultOptions()
	opts.IncludeUnexported = true
	rep, err := GenerateForFile(filepath.Join(dir, "p.go"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Added, []string{"Test_add"}) || !slices.Equal(rep.Skipped, []string{"TestAdd"}) {
		t.Errorf("added %v, kept %v", rep.Added, rep.Skipped)
	}
}

// 不比對的回傳值沒有 want 欄位，它的 import 也不能加進測試檔。
// 歷史 bug：func Guard() http.HandlerFunc 的骨架引入 net/http 卻沒有用到，imported and not used
func TestUncomparedResultsDoNotAddImports(t *testing.T) {
	dir := writePkg(t, map[string]string{"g.go": `package p

import (
	"net/http"
	"time"
)

func Guard(limit int) http.HandlerFunc { return nil }

func GuardErr(limit int) (http.HandlerFunc, error) { return nil, nil }

func Literal() func(time.Duration) error { return nil }
`})
	added(t, dir)
	got := readFile(t, filepath.Join(dir, "g_test.go"))
	if strings.Contains(got, `"net/http"`) || strings.Contains(got, `"time"`) {
		t.Errorf("unused import in the generated file:\n%s", got)
	}
	typecheck(t, dir, "p")
}
