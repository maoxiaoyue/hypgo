package contract

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type EmbBase struct {
	Name string `json:"name" validate:"required"`
}

type embPublic struct {
	EmbBase
	Extra string `json:"extra"`
}

type embPrivateBase struct {
	Code string `json:"code"`
}

type embPrivate struct {
	embPrivateBase
}

type embPtr struct {
	*EmbBase
	Extra string `json:"extra"`
}

// TestEmbeddedStructsFollowJSONRules 回歸測試：內嵌 struct 依 encoding/json 規則展開。
// 歷史 bug：必填檢查要求 JSON 有 "EmbBase" 鍵（正確回應被判失敗）、未匯出的內嵌型別整個跳過
// （缺欄位照樣通過）；生成端產生 {"EmbBase":{...}}，handler 解不到必填欄位而回 422。
func TestEmbeddedStructsFollowJSONRules(t *testing.T) {
	if err := validateResponse([]byte(`{"name":"a","extra":"x"}`), embPublic{}); err != nil {
		t.Errorf("valid flattened body rejected: %v", err)
	}
	if err := validateResponse([]byte(`{"extra":"x"}`), embPublic{}); err == nil {
		t.Error("missing promoted field \"name\" not detected")
	}
	if err := validateResponse([]byte(`{}`), embPrivate{}); err == nil {
		t.Error("missing field of unexported embedded type not detected")
	}
	if err := validateResponse([]byte(`{"extra":"x"}`), embPtr{}); err != nil {
		t.Errorf("fields under embedded pointer must be optional: %v", err)
	}

	var got embPublic
	gen := generateMinimalJSON(embPublic{})
	if err := json.Unmarshal([]byte(gen), &got); err != nil || got.Name == "" {
		t.Errorf("generated %s does not fill promoted field (Name=%q, err=%v)", gen, got.Name, err)
	}
}

type listQuery struct {
	Page int    `form:"page" json:"page" validate:"required,min=1"`
	Sort string `json:"sort" validate:"oneof=asc desc"`
}

type listItem struct {
	ID int `json:"id"`
}

// TestGeneratedCasesPassForCorrectHandlers 回歸測試：對正確實作的 handler，自動生成的案例必須通過。
// 歷史 bug：GET 的 Input 不生成 query（BindInput 必回 422）；DELETE 宣告 Output 卻預設期望 204
// （回 200 狀態不符、回 204 body 為空，兩頭都失敗）。
func TestGeneratedCasesPassForCorrectHandlers(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/items", Summary: "list", Input: listQuery{}, Output: listItem{}}).
		Handle(func(c *hypcontext.Context) {
			var q listQuery
			if !c.BindInput(&q) {
				return
			}
			c.JSON(200, listItem{ID: q.Page})
		})
	r.Schema(schema.Route{Method: "DELETE", Path: "/items/:id", Summary: "delete", Output: listItem{}}).
		Handle(func(c *hypcontext.Context) { c.JSON(200, listItem{ID: 1}) })

	for _, rt := range schema.Global().All() {
		tc := generateTestCase(rt)
		tc.schemaPath = rt.Path
		if pass, reason, _ := runTestOnce(r, tc); !pass {
			t.Errorf("%s %s: %s (query=%v)", rt.Method, rt.Path, reason, tc.Query)
		}
	}
}

// TestManualTestFindsSchemaForParamRoute 回歸測試：手動 Test 以實際路徑也找得到帶參數路由的 schema。
// 歷史 bug：精確查表 "GET /api/users/1"，而登記的 key 是 "/api/users/:id"，回 no schema registered。
func TestManualTestFindsSchemaForParamRoute(t *testing.T) {
	r := setupTestRouter()
	tc := TestCase{Route: "GET /api/users/1?verbose=1", ExpectStatus: 200, ExpectSchema: true}
	if pass, reason, _ := runTestOnce(r, tc); !pass {
		t.Errorf("runTestOnce: %s", reason)
	}

	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		{"/api/users/:id", "/api/users/1", true},
		{"/api/users/:id", "/api/users/", false},
		{"/api/users/:id", "/api/users/1/posts", false},
		{"/static/*filepath", "/static/a/b.css", true},
		{"/api/users", "/api/users", true},
		{"/api/users", "/api/posts", false},
	} {
		if got := pathMatches(c.pattern, c.path); got != c.want {
			t.Errorf("pathMatches(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// TestObserveReportsFailuresToT 回歸測試：ObserveAll 對失敗路由呼叫 t.Errorf，且 Input schema 不符即判失敗。
// 歷史 bug：Observe 從不呼叫 t.Errorf，路由失敗測試仍是綠的；Input schema 步驟失敗也不影響 Pass，
// 與 runTestOnce 的判定不一致。
func TestObserveReportsFailuresToT(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/boom", Summary: "boom"}).
		Handle(func(c *hypcontext.Context) { c.Status(500) })

	results := ObserveAll(nil, r, ObserveOptions{OutputPath: filepath.Join(t.TempDir(), "o.html")})
	var reported []string
	reportObserveFailures(func(f string, a ...interface{}) { reported = append(reported, fmt.Sprintf(f, a...)) }, results)
	if len(reported) != 1 || !strings.Contains(reported[0], "GET /boom") {
		t.Errorf("reported = %q, want one failure for GET /boom", reported)
	}

	type in struct {
		N int `json:"n" validate:"min=5"`
	}
	tc := TestCase{ExpectStatus: 200, Input: `{"n":1}`}
	_, pass, reason := validateExchange(schema.Route{Input: in{}}, tc, 200, nil)
	if pass {
		t.Error("Input schema violation must fail the observe result")
	} else if !strings.Contains(reason, "request schema") {
		t.Errorf("reason = %q, want request schema failure", reason)
	}
}

// TestTestAllRespectsMaxWorkers 回歸測試：TestAll 並行模式同時執行的路由數不超過 MaxWorkers。
// 歷史 bug：TestAll 從不讀 MaxWorkers，實際並行數只受 go test -parallel 限制。
func TestTestAllRespectsMaxWorkers(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	var cur, peak atomic.Int32
	h := func(c *hypcontext.Context) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		cur.Add(-1)
		c.Status(http.StatusOK)
	}
	for _, p := range []string{"/w1", "/w2", "/w3", "/w4", "/w5", "/w6"} {
		r.Schema(schema.Route{Method: "GET", Path: p, Summary: p}).Handle(h)
	}

	t.Run("all", func(t *testing.T) {
		TestAll(t, r, RunConfig{Parallel: true, MaxWorkers: 2})
	})
	if got := peak.Load(); got > 2 {
		t.Errorf("peak concurrency = %d, want <= MaxWorkers 2", got)
	}
}

// TestFixtureAndReportEdgeCases 回歸測試：A10 的幾個小問題。
func TestFixtureAndReportEdgeCases(t *testing.T) {
	// HasHeader：值為空字串的標頭也算存在
	res := &TestResult{Headers: http.Header{"X-Empty": {""}}}
	if !res.HasHeader("X-Empty") {
		t.Error("HasHeader must report a present header with an empty value")
	}

	// WithJSON 序列化失敗要記錄錯誤，不可靜默改送 {}
	if b := Request(router.New()).POST("/x").WithJSON(make(chan int)); b.err == nil {
		t.Error("WithJSON(chan) must record the marshal error")
	}

	// 截斷不得切在多位元組字元中間
	out := formatJSONCompact(strings.Repeat("a", 1999) + "中文")
	if body := strings.TrimSuffix(out, "\n… (truncated)"); !utf8.ValidString(body) || body != strings.Repeat("a", 1999) {
		t.Errorf("truncation broke a rune: %q", out[len(out)-20:])
	}

	// filter 需 HTML 跳脫
	html := GenerateObserveHTML([]ObserveResult{{Route: schema.Route{Method: "GET", Path: "/"}, Pass: true}}, "<script>")
	if strings.Contains(html, "<script>」") {
		t.Error("filter is inserted into the report without HTML escaping")
	}
}

// TestResolvePathUUIDParam 回歸測試：:uuid / :guid 參數要填 UUID。
// 歷史 bug：guessParamValue 先判斷是否含 "id"，而 uuid、guid 都含 "id"，UUID 分支永遠走不到。
func TestResolvePathUUIDParam(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000000"
	for path, want := range map[string]string{
		"/files/:uuid": "/files/" + uuid,
		"/files/:guid": "/files/" + uuid,
		"/users/:id":   "/users/1",
	} {
		if got := resolvePath(path); got != want {
			t.Errorf("resolvePath(%q) = %q, want %q", path, got, want)
		}
	}
}
