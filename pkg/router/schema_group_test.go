package router

import (
	"net/http/httptest"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type declaredInput struct {
	Name string `json:"name"`
}

type wrongInput struct {
	Title string `json:"title"`
}

// TestGroupSchemaRouteReportsBindMismatch 回歸測試：group 底下的 schema 路由同樣偵測 BindInput 綁錯型別。
// 歷史 bug：Group.RegisterSchema 是另一份實作、漏了 schema-input 標記，group 路由綁錯 struct 完全不回報。
func TestGroupSchemaRouteReportsBindMismatch(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	var reports []string
	hypcontext.SetBindInputReporter(func(key, declared, bound string) { reports = append(reports, key) })
	defer hypcontext.SetBindInputReporter(nil)

	r := New()
	h := func(c *hypcontext.Context) {
		var x wrongInput
		c.BindInput(&x)
	}
	r.Schema(schema.Route{Method: "POST", Path: "/top", Input: declaredInput{}}).Handle(h)
	r.NewGroup("/api").NewGroup("/v1").Schema(schema.Route{Method: "post", Path: "/grp", Input: declaredInput{}}).Handle(h)

	for _, p := range []string{"/top", "/api/v1/grp"} {
		req := httptest.NewRequest("POST", p, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	want := "rest|POST /top,rest|POST /api/v1/grp"
	if got := strings.Join(reports, ","); got != want {
		t.Errorf("mismatch reports = %q, want %q", got, want)
	}
}
