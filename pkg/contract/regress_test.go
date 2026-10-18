package contract

import (
	"encoding/json"
	"testing"

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
