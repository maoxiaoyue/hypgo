package manifest

import (
	"bytes"
	"strings"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type r2Addr struct {
	City string `json:"city"`
}

type r2Line struct {
	SKU  string   `json:"sku"`
	Ship *r2Addr  `json:"ship,omitempty"`
	Next *r2Line  `json:"next,omitempty"` // 自我參照不能無限遞迴
	Alt  []r2Addr `json:"alt,omitempty"`
}

type r2Order struct {
	Addr    r2Addr            `json:"addr"`
	Lines   []r2Line          `json:"lines"`
	Meta    map[string]string `json:"meta,omitempty"`
	Created time.Time         `json:"created"`
}

type R2WidgetController struct{}

func (*R2WidgetController) GetByID(c *hypcontext.Context) {}
func (*R2WidgetController) Create(c *hypcontext.Context)  {}

func r2Manifest(t *testing.T, setup func(r *router.Router)) (*Manifest, string) {
	t.Helper()
	schema.Global().Reset()
	t.Cleanup(schema.Global().Reset)
	r := router.New()
	setup(r)
	m := NewCollector(r, nil).Collect()
	var buf bytes.Buffer
	if err := WriteYAML(&buf, m); err != nil {
		t.Fatal(err)
	}
	return m, buf.String()
}

func r2Kinds(m *Manifest) map[string]bool {
	out := map[string]bool{}
	for _, w := range m.Lint.Warnings {
		out[w.Kind+" "+w.Route] = true
	}
	return out
}

// types 要寫出巢狀與元素的型別名稱，並登錄引用到的型別。
// 歷史 bug：欄位只有 object / array，巢狀型別的欄位不在 manifest 任何地方
func TestTypesDescribeNestedShapes(t *testing.T) {
	m, out := r2Manifest(t, func(r *router.Router) {
		r.Schema(schema.Route{Method: "POST", Path: "/orders", Summary: "s", Input: r2Order{}, Output: []r2Order{}}).Handle(func(c *hypcontext.Context) {})
	})
	want := map[string]map[string]string{
		"manifest.r2Order": {"addr": "manifest.r2Addr", "lines": "[]manifest.r2Line", "meta": "map[string]string", "created": "string"},
		"manifest.r2Line":  {"ship": "manifest.r2Addr", "next": "manifest.r2Line", "alt": "[]manifest.r2Addr"},
		"manifest.r2Addr":  {"city": "string"},
	}
	for typ, fields := range want {
		got := map[string]string{}
		for _, f := range m.Types[typ] {
			got[f.Name] = f.Type
		}
		for name, w := range fields {
			if got[name] != w {
				t.Errorf("types.%s.%s = %q, want %q\n%s", typ, name, got[name], w, out)
			}
		}
	}
	if _, ok := m.Types["Time"]; ok {
		t.Error("time.Time must not be listed as a type")
	}
}

// Schema Lint 不對合理的路由回報無法消除的錯誤。
// 歷史 bug：沒有 body 的 POST 一定被報 missing_input、回 204 的 PUT 一定被報 missing_output，
// /healthz 與 r.Any 帶出的 OPTIONS 被報 no_schema——只能宣告假的型別或放棄 hyp lint
func TestLintAcceptsBodylessAndNoContentRoutes(t *testing.T) {
	h := func(c *hypcontext.Context) {}
	noContent := map[int]schema.ResponseSchema{204: {Description: "done"}}
	m, _ := r2Manifest(t, func(r *router.Router) {
		r.Schema(schema.Route{Method: "POST", Path: "/logout", Summary: "s", NoInput: true, Responses: noContent}).Handle(h)
		r.Schema(schema.Route{Method: "PUT", Path: "/typed", Summary: "s", NoInput: true,
			Responses: map[int]schema.ResponseSchema{200: {Description: "ok", Type: r2Addr{}}}}).Handle(h)
		r.Schema(schema.Route{Method: "POST", Path: "/bare", Summary: "s"}).Handle(h) // 兩項都該報
		r.Schema(schema.Route{Method: "POST", Path: "/err-only", Summary: "s", NoInput: true,
			Responses: map[int]schema.ResponseSchema{404: {Description: "nf", Type: r2Addr{}}}}).Handle(h) // 只宣告錯誤回應不算
		r.GET("/healthz", h)
		r.GET("/readyz", h)
		r.GET("/livez", h)
		r.Any("/hook", h)
	})
	kinds := r2Kinds(m)
	for _, unwanted := range []string{
		"missing_input POST /logout", "missing_output POST /logout",
		"missing_input PUT /typed", "missing_output PUT /typed",
		"no_schema GET /healthz", "no_schema GET /readyz", "no_schema GET /livez",
		"no_schema OPTIONS /hook", "no_schema HEAD /hook",
	} {
		if kinds[unwanted] {
			t.Errorf("unexpected warning: %s", unwanted)
		}
	}
	for _, wanted := range []string{
		"missing_input POST /bare", "missing_output POST /bare", "missing_output POST /err-only", "no_schema GET /hook",
	} {
		if !kinds[wanted] {
			t.Errorf("missing warning: %s (got %v)", wanted, kinds)
		}
	}
}

// 只在 registry、router 上沒有的 REST Schema 要有提示（資訊級）。
// 歷史 bug：不出現在 manifest，也沒有任何警告
func TestLintReportsUnregisteredSchema(t *testing.T) {
	m, _ := r2Manifest(t, func(r *router.Router) {
		r.Schema(schema.Route{Method: "GET", Path: "/a", Summary: "s", Output: r2Addr{}}).Handle(func(c *hypcontext.Context) {})
		schema.Global().Register(schema.Route{Method: "GET", Path: "/typo", Summary: "s", Output: r2Addr{}})
		schema.RegisterGRPC("Svc/Do", "s", nil, nil) // 非 REST 不算
	})
	kinds := r2Kinds(m)
	if !kinds["unregistered_schema GET /typo"] || len(kinds) != 1 {
		t.Errorf("warnings = %v", kinds)
	}
	if !InfoKind("unregistered_schema") || !InfoKind("missing_summary") || InfoKind("no_schema") || InfoKind("config_warning") {
		t.Error("InfoKind classification changed")
	}
}

// routes 只列 API：靜態檔案與沒有自己 Schema 的 HEAD 不列；不需要 Schema 的路由不推斷 summary；
// handler 名稱推斷的 summary 把資源放在動詞後面；沒有設定檔時省略 server。
// 歷史 bug：/static/*filepath 列成 "List Static"、/health 是 "List Health"、GetByID 是 "Get By ID User"
func TestRoutesListOnlyAPIsWithSensibleSummaries(t *testing.T) {
	ctl := &R2WidgetController{}
	m, out := r2Manifest(t, func(r *router.Router) {
		r.Static("/static", ".")
		r.Any("/hook", func(c *hypcontext.Context) {})
		r.GET("/health", func(c *hypcontext.Context) {})
		r.Schema(schema.Route{Method: "GET", Path: "/users/:id", Output: r2Addr{}}).Handle(ctl.GetByID)
		r.Schema(schema.Route{Method: "POST", Path: "/users", Input: r2Addr{}, Output: r2Addr{}}).Handle(ctl.Create)
		r.Schema(schema.Route{Method: "HEAD", Path: "/probe", Summary: "probe"}).Handle(func(c *hypcontext.Context) {})
	})
	summaries := map[string]string{}
	for _, rt := range m.Routes {
		summaries[rt.Method+" "+rt.Path] = rt.Summary
	}
	for key := range summaries {
		if strings.Contains(key, "/static") || key == "HEAD /hook" {
			t.Errorf("%s should not be listed", key)
		}
	}
	for key, want := range map[string]string{
		"GET /users/:id": "Get R2Widget By ID", "POST /users": "Create R2Widget", "GET /health": "", "HEAD /probe": "probe",
	} {
		if got, ok := summaries[key]; !ok || got != want {
			t.Errorf("%s: summary = %q (listed = %v), want %q", key, got, ok, want)
		}
	}
	if m.Server != nil || strings.Contains(out, "server:") {
		t.Errorf("server section should be omitted without a config:\n%s", out)
	}
}

// FieldInfo 與 schema.FieldInfo 同名、欄位不同
type FieldInfo struct {
	Mine int `json:"mine"`
}

type r2Tree []r2Tree

type r2Paged[T any] struct {
	Items []T `json:"items"`
}

// 不同套件的同名型別各自一份；自我參照的容器型別不會卡住；泛型名稱不帶匯入路徑。
// 歷史 bug：types 以短名稱為鍵，兩條路由都寫 input_type: FieldInfo 而欄位只有其中一份；
// Output 是 type Tree []Tree 時產生 manifest 的迴圈永遠不結束（server 啟動卡住）
func TestTypesAreKeyedByQualifiedName(t *testing.T) {
	done := make(chan struct{})
	var m *Manifest
	var out string
	go func() {
		defer close(done)
		m, out = r2Manifest(t, func(r *router.Router) {
			h := func(c *hypcontext.Context) {}
			r.Schema(schema.Route{Method: "POST", Path: "/mine", Summary: "s", Input: FieldInfo{}, Output: r2Paged[FieldInfo]{}}).Handle(h)
			r.Schema(schema.Route{Method: "POST", Path: "/theirs", Summary: "s", Input: schema.FieldInfo{}, Output: r2Tree{}}).Handle(h)
		})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("collecting a manifest with a self-referential type did not finish")
	}

	inputs := map[string]string{}
	for _, rt := range m.Routes {
		inputs[rt.Path] = rt.InputType + " -> " + rt.OutputType
	}
	if inputs["/mine"] != "manifest.FieldInfo -> manifest.r2Paged[manifest.FieldInfo]" || inputs["/theirs"] != "schema.FieldInfo -> manifest.r2Tree" {
		t.Errorf("route types = %v", inputs)
	}
	mine, theirs := m.Types["manifest.FieldInfo"], m.Types["schema.FieldInfo"]
	if len(mine) != 1 || mine[0].Name != "mine" || len(theirs) != 3 || theirs[0].Name != "name" {
		t.Errorf("types: mine = %v, theirs = %v\n%s", mine, theirs, out)
	}
	if paged := m.Types["manifest.r2Paged[manifest.FieldInfo]"]; len(paged) != 1 || paged[0].Type != "[]manifest.FieldInfo" {
		t.Errorf("generic type = %v\n%s", paged, out)
	}
	if strings.Contains(out, "github.com") {
		t.Errorf("import path leaked into the manifest:\n%s", out)
	}
}
