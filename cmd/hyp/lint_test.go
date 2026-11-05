package main

import (
	"os"
	"path/filepath"
	"testing"
)

const lintRouteHeader = `package main

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type Req struct{}
type Other struct{}

func right(c *hypcontext.Context) { var r Req; c.BindInput(&r) }
func wrong(c *hypcontext.Context) { var o Other; c.BindInput(&o) }
func make1() hypcontext.HandlerFunc { return right }
func main() { reg(router.New()) }
`

// writeLintProject 寫入 main.go，reg 的內容由 body 決定
func writeLintProject(t *testing.T, body string) {
	t.Helper()
	src := lintRouteHeader + "func reg(r *router.Router) {\n" + body + "\n}\n"
	if err := os.WriteFile("main.go", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	lintRouteRight   = `r.Schema(schema.Route{Method: "POST", Path: "/r", Input: Req{}}).Handle(right)`
	lintRouteWrong   = `r.Schema(schema.Route{Method: "POST", Path: "/w", Input: Req{}}).Handle(wrong)`
	lintRouteSkipped = `r.Schema(schema.Route{Method: "POST", Path: "/s", Input: Req{}}).Handle(make1())`
)

func TestLintDeepExitCodes(t *testing.T) {
	migrateProject(t)

	writeLintProject(t, lintRouteRight)
	if err := runHyp(t, "lint", "--deep"); err != nil {
		t.Errorf("clean project: %v", err)
	}

	writeLintProject(t, lintRouteWrong)
	if err := runHyp(t, "lint", "--deep"); err == nil {
		t.Error("a finding should fail")
	}

	// 略過的路由只有 --strict 才失敗
	writeLintProject(t, lintRouteSkipped)
	if err := runHyp(t, "lint", "--deep"); err != nil {
		t.Errorf("skipped without --strict: %v", err)
	}
	if err := runHyp(t, "lint", "--deep", "--strict"); err == nil {
		t.Error("skipped routes should fail with --strict")
	}
}

// 分析不完整就不能通過：先前這些情況都 exit 0 並印 ✓
func TestLintDeepLoadErrorsFail(t *testing.T) {
	migrateProject(t)
	writeLintProject(t, lintRouteRight)

	if err := runHyp(t, "lint", "--deep", "./typo/..."); err == nil {
		t.Error("a mistyped package pattern should fail")
	}

	os.Mkdir("empty", 0o755)
	if err := runHyp(t, "lint", "--deep", "./empty/..."); err == nil {
		t.Error("a pattern matching no packages should fail")
	}

	// 路由綁定正確但編譯不過：不加 --strict 也要失敗
	writeLintProject(t, lintRouteRight+"\n\tundefinedThing()")
	if err := runHyp(t, "lint", "--deep"); err == nil {
		t.Error("a compile error should fail")
	}
}

func TestLintDeepOutsideModuleFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runHyp(t, "lint", "--deep"); err == nil {
		t.Error("--deep outside a module should fail")
	}
}

// 從子目錄執行：預設分析整個 module，先前只看該子目錄並 exit 0
func TestLintUsesModuleRootFromSubdir(t *testing.T) {
	migrateProject(t)
	writeLintProject(t, lintRouteWrong)
	root, _ := os.Getwd()
	sub := filepath.Join(root, "routers")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "s.go"), []byte("package routers\n"), 0o644)
	t.Chdir(sub)

	if err := runHyp(t, "lint", "--deep"); err == nil {
		t.Error("should find the problem at the module root")
	}
	// 明確給的套件樣式仍相對於當前目錄
	if err := runHyp(t, "lint", "--deep", "."); err != nil {
		t.Errorf("explicit pattern should stay relative to the current directory: %v", err)
	}

	// manifest 也從根目錄讀
	os.MkdirAll(filepath.Join(root, ".hyp"), 0o755)
	os.WriteFile(filepath.Join(root, ".hyp", "context.yaml"), []byte("lint:\n  total: 0\n  with_schema: 0\n  coverage: 0/0\n"), 0o644)
	if err := runHyp(t, "lint"); err != nil {
		t.Errorf("manifest at the module root not used: %v", err)
	}
}

// 明確指定的 -f 讀不到要失敗；預設路徑不存在時 --deep 仍可單獨執行
func TestLintDeepExplicitManifestMustExist(t *testing.T) {
	migrateProject(t)
	writeLintProject(t, lintRouteRight)

	if err := runHyp(t, "lint", "--deep", "-f", "typo.yaml"); err == nil {
		t.Error("a mistyped -f should fail")
	}
	if err := runHyp(t, "lint", "--deep"); err != nil {
		t.Errorf("default manifest missing: %v", err)
	}
}
