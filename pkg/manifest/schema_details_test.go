package manifest

import (
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type notFoundResp struct {
	Code string `json:"code"`
}

// TestManifestIncludesSchemaDetails 回歸測試：schema 宣告的 Params / Headers / Responses 型別寫進 manifest。
// 歷史 bug：這些欄位宣告了也沒有任何地方讀，AI 從 .hyp/context.yaml 看不到。
func TestManifestIncludesSchemaDetails(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{
		Method:    "GET",
		Path:      "/items/:id",
		Summary:   "get item",
		Params:    []schema.ParamSchema{{Name: "id", In: "path", Required: true, Type: "int"}},
		Headers:   []schema.HeaderSchema{{Name: "X-Tenant", Required: true}},
		Responses: map[int]schema.ResponseSchema{404: {Description: "not found", Type: notFoundResp{}}},
	}).Handle(func(c *hypcontext.Context) {})

	var got *RouteManifest
	for _, rm := range NewCollector(r, nil).Collect().Routes {
		if rm.Path == "/items/:id" {
			got = &rm
		}
	}
	if got == nil {
		t.Fatal("route missing from manifest")
	}
	if len(got.Params) != 1 || got.Params[0].Name != "id" || len(got.Headers) != 1 || got.Headers[0].Name != "X-Tenant" {
		t.Errorf("Params=%+v Headers=%+v", got.Params, got.Headers)
	}
	if got.Responses[404] != "not found" || got.ResponseTypes[404] != "notFoundResp" {
		t.Errorf("Responses=%v ResponseTypes=%v", got.Responses, got.ResponseTypes)
	}
}
