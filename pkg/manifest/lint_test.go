package manifest

import (
	"net/http"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

func lintHandler(c *hypcontext.Context) {}

func setupLintRouter() *router.Router {
	schema.Global().Reset()
	r := router.New()

	// 忽略清單：不應計入
	r.GET("/health", lintHandler)

	// 完整 schema（無警告）
	r.Schema(schema.Route{
		Method: "POST", Path: "/api/users", Summary: "建立使用者",
		Input: createUserReq{}, Output: userResp{},
	}).Handle(lintHandler)

	// GET + Output + Summary（無警告，GET 不需 Input）
	r.Schema(schema.Route{
		Method: "GET", Path: "/api/users/:id", Summary: "取得使用者",
		Output: userResp{},
	}).Handle(lintHandler)

	// 可寫方法缺 Input
	r.Schema(schema.Route{
		Method: "POST", Path: "/api/orders", Summary: "建立訂單",
		Output: userResp{},
	}).Handle(lintHandler)

	// 缺 Output 與 Summary
	r.Schema(schema.Route{
		Method: "GET", Path: "/api/reports",
	}).Handle(lintHandler)

	// 完全無 schema
	r.GET("/legacy", lintHandler)

	return r
}

func TestLintRoutes(t *testing.T) {
	r := setupLintRouter()
	rep := lintRoutes(r, schema.Global())
	if rep == nil {
		t.Fatal("expected lint report, got nil")
	}

	// /health 被忽略；其餘 5 條可檢查
	if rep.Total != 5 {
		t.Errorf("Total = %d, want 5", rep.Total)
	}
	// users POST/GET、orders POST、reports GET 有 schema = 4
	if rep.WithSchema != 4 {
		t.Errorf("WithSchema = %d, want 4", rep.WithSchema)
	}
	if rep.Coverage != "4/5" {
		t.Errorf("Coverage = %q, want 4/5", rep.Coverage)
	}

	kinds := map[string]int{}
	for _, w := range rep.Warnings {
		kinds[w.Kind]++
		if w.Route == "GET /health" {
			t.Error("/health should be ignored")
		}
	}
	if kinds["no_schema"] != 1 {
		t.Errorf("no_schema = %d, want 1 (GET /legacy)", kinds["no_schema"])
	}
	if kinds["missing_input"] != 1 {
		t.Errorf("missing_input = %d, want 1 (POST /api/orders)", kinds["missing_input"])
	}
	if kinds["missing_output"] != 1 {
		t.Errorf("missing_output = %d, want 1 (GET /api/reports)", kinds["missing_output"])
	}
	if kinds["missing_summary"] != 1 {
		t.Errorf("missing_summary = %d, want 1 (GET /api/reports)", kinds["missing_summary"])
	}
}

func TestFormatLint(t *testing.T) {
	r := setupLintRouter()
	rep := lintRoutes(r, schema.Global())
	out := FormatLint(rep)

	if !strings.Contains(out, "Schema 覆蓋率：4/5") {
		t.Errorf("expected coverage line, got:\n%s", out)
	}
	if !strings.Contains(out, "GET /legacy") || !strings.Contains(out, "no_schema") {
		t.Errorf("expected no_schema warning for /legacy, got:\n%s", out)
	}
}

func TestLintCleanProjectNoWarnings(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.GET("/health", lintHandler)
	r.Schema(schema.Route{
		Method: "POST", Path: "/api/users", Summary: "建立使用者",
		Input: createUserReq{}, Output: userResp{},
	}).Handle(lintHandler)

	rep := lintRoutes(r, schema.Global())
	if len(rep.Warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %+v", len(rep.Warnings), rep.Warnings)
	}
	if rep.Coverage != "1/1" {
		t.Errorf("Coverage = %q, want 1/1", rep.Coverage)
	}
}

// TestLintSkipsStaticRoutes 迴歸測試：Static / StaticFS / StaticFile 註冊的檔案服務路由不報 no_schema。
// 歷史 bug：/static/*filepath 等框架產生的路由一律被判為缺 Schema。
// 用真實的 router 註冊，handler 名稱若因重構改變，此測試會失敗
func TestLintSkipsStaticRoutes(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	dir := t.TempDir()
	r := router.New()
	r.Static("/static", dir)
	r.StaticFile("/robots.txt", dir)
	r.NewGroup("/assets", func(c *hypcontext.Context) { c.Next() }).StaticFS("/", http.Dir(dir)) // 帶 group 中介層
	r.GET("/api/plain", func(c *hypcontext.Context) {})

	rep := lintRoutes(r, schema.Global())
	if len(rep.Warnings) != 1 || rep.Warnings[0].Route != "GET /api/plain" {
		t.Errorf("warnings = %+v, want only GET /api/plain", rep.Warnings)
	}
	if rep.Total != 1 {
		t.Errorf("Total = %d, want 1 (static routes not counted)", rep.Total)
	}
}
