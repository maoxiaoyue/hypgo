package manifest

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
	"gopkg.in/yaml.v3"
)

type fmtUser struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Age   int    `json:"age,omitempty"`
}

type fmtUserReq struct {
	Email string `json:"email"`
}

type fmtErr struct {
	Code string `json:"code"`
}

func compactRouter() *router.Router {
	r := router.New()
	r.Schema(schema.Route{Method: "POST", Path: "/users", Summary: "Create", Input: fmtUserReq{}, Output: fmtUser{},
		Responses: map[int]schema.ResponseSchema{409: {Description: "conflict", Type: fmtErr{}}}}).Handle(listOrdersHandler)
	r.Schema(schema.Route{Method: "GET", Path: "/users/:id", Summary: "Get", Output: fmtUser{}}).Handle(listOrdersHandler)
	r.Schema(schema.Route{Method: "PUT", Path: "/users/:id", Summary: "Update", Input: fmtUserReq{}, Output: fmtUser{}}).Handle(listOrdersHandler)
	return r
}

// TestManifestCompactYAML 迴歸測試：型別欄位集中在 types 區段、每型別一次、每欄位一行；
// REST 路由不輸出 protocol；tags / handler_names 為 flow style；generated_at 在最後。
// 先前欄位內嵌在每條路由（每欄位四行，被 N 條路由引用就重複 N 份），generated_at 在第 3 行
func TestManifestCompactYAML(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	var buf bytes.Buffer
	if err := WriteYAML(&buf, NewCollector(compactRouter(), nil).Collect()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"types:\n  fmtErr:\n    code: string!\n  fmtUser:\n    id: integer!\n    email: string!\n    age: integer\n  fmtUserReq:\n    email: string!\n",
		"    handler_names: [manifest.listOrdersHandler]\n",
		"    tags: [users]\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"protocol: rest", "input_fields", "output_fields", "json_tag"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output should not contain %q:\n%s", unwanted, out)
		}
	}
	if n := strings.Count(out, "fmtUser:"); n != 1 {
		t.Errorf("fmtUser listed %d times, want once", n)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.HasPrefix(lines[len(lines)-1], "generated_at:") {
		t.Errorf("last line = %q, want generated_at", lines[len(lines)-1])
	}
}

// TestManifestTypesRoundTrip types 區段經 YAML（hyp lint、genctx）與 JSON（hyp context 的 HYPGO_MANIFEST_OUT）
// 寫出再讀回，欄位順序與必填標記不變
func TestManifestTypesRoundTrip(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	m := NewCollector(compactRouter(), nil).Collect()
	want := TypeFields{{Name: "id", Type: "integer", Required: true}, {Name: "email", Type: "string", Required: true}, {Name: "age", Type: "integer"}}
	if !reflect.DeepEqual(m.Types["fmtUser"], want) {
		t.Fatalf("collected fmtUser = %+v, want %+v", m.Types["fmtUser"], want)
	}

	var y bytes.Buffer
	if err := WriteYAML(&y, m); err != nil {
		t.Fatal(err)
	}
	var fromYAML Manifest
	if err := yaml.Unmarshal(y.Bytes(), &fromYAML); err != nil {
		t.Fatal(err)
	}

	var j bytes.Buffer
	if err := WriteJSON(&j, m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j.String(), `"fmtUser": {
      "id": "integer!",
      "email": "string!",
      "age": "integer"
    }`) {
		t.Errorf("JSON types not compact / ordered:\n%s", j.String())
	}
	var fromJSON Manifest
	if err := json.Unmarshal(j.Bytes(), &fromJSON); err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string]map[string]TypeFields{"yaml": fromYAML.Types, "json": fromJSON.Types} {
		if !reflect.DeepEqual(got, m.Types) {
			t.Errorf("%s round trip: got %+v, want %+v", name, got, m.Types)
		}
	}
}

// TestManifestKeepsNonRESTProtocol 非 REST 路由仍輸出 protocol
func TestManifestKeepsNonRESTProtocol(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	schema.RegisterGRPC("UserService/Get", "Get user", nil, fmtUser{})
	m := NewCollector(router.New(), nil).Collect()
	if len(m.Routes) != 1 || m.Routes[0].Protocol != "grpc" {
		t.Fatalf("routes = %+v, want one grpc route", m.Routes)
	}
	if _, ok := m.Types["fmtUser"]; !ok {
		t.Error("types of non-REST routes should be collected too")
	}
}

// TestManifestTypesOff IncludeFields 關閉時不輸出 types
func TestManifestTypesOff(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	cfg := DefaultEnrichConfig()
	cfg.IncludeFields = false
	if m := NewCollectorWithEnrich(compactRouter(), nil, cfg).Collect(); m.Types != nil {
		t.Errorf("types = %+v, want none", m.Types)
	}
}
