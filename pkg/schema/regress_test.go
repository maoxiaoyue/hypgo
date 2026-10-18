package schema

import (
	"fmt"
	"testing"
)

// TestRegistryReRegisterAfterGrowth 回歸測試：切片擴容後重複註冊，Get 與 All 必須一致。
// 歷史 bug：byKey 存元素指標，擴容後指向舊陣列，重新註冊只改到舊副本——Get 回新宣告、All() 回舊的。
func TestRegistryReRegisterAfterGrowth(t *testing.T) {
	var r Registry // 零值即可使用（先前 nil map 會 panic）
	r.Register(Route{Method: "GET", Path: "/a", Summary: "v1"})
	for i := 0; i < 20; i++ {
		r.Register(Route{Method: "GET", Path: fmt.Sprintf("/x%d", i)})
	}
	r.Register(Route{Method: "GET", Path: "/a", Summary: "v2"})

	got, _ := r.Get("GET", "/a")
	if got.Summary != "v2" || r.All()[0].Summary != "v2" || r.Len() != 21 {
		t.Errorf("Get=%q All()[0]=%q Len=%d, want v2 / v2 / 21", got.Summary, r.All()[0].Summary, r.Len())
	}

	got.Summary = "mutated" // 回傳副本：呼叫端修改不得改到註冊表
	if again, _ := r.Get("GET", "/a"); again.Summary != "v2" {
		t.Errorf("caller mutation leaked into registry: %q", again.Summary)
	}
}

type PEmbedBase struct {
	Name string `json:"name"`
}

type pEmbedded struct {
	PEmbedBase
	Extra string `json:"extra"`
}

type pEmbeddedPtr struct {
	*PEmbedBase
}

type pOmitzero struct {
	A string `json:"a,omitzero"`
	B int    `json:"b"`
}

// TestFieldsOfFollowsEncodingJSON 回歸測試：FieldsOf 依 encoding/json 規則描述 JSON 形狀。
// 歷史 bug：內嵌 struct 變成一個名為 "PEmbedBase" 的 object 欄位（manifest 寫出錯誤形狀）；
// omitzero 欄位被標為必填（零值時根本不會序列化）。
func TestFieldsOfFollowsEncodingJSON(t *testing.T) {
	names := func(fs []FieldInfo) string {
		s := ""
		for _, f := range fs {
			s += fmt.Sprintf("%s:%v ", f.Name, f.Required)
		}
		return s
	}
	if got := names(FieldsOf(pEmbedded{})); got != "name:true extra:true " {
		t.Errorf("embedded FieldsOf = %q, want flattened name + extra", got)
	}
	if got := names(FieldsOf(pEmbeddedPtr{})); got != "name:false " {
		t.Errorf("embedded pointer FieldsOf = %q, want optional name", got)
	}
	if got := names(FieldsOf(pOmitzero{})); got != "a:false b:true " {
		t.Errorf("omitzero FieldsOf = %q, want a optional", got)
	}

}

// TestRegisterNormalizesDerivedFields 回歸測試：直接 Register 也補齊衍生欄位，且不改呼叫端的 map。
// 歷史 bug：只有 NewSchemaRoute / helper 會填 InputName（直接註冊的路由被 contract 判缺 InputName）；
// NewSchemaRoute 把 TypeName 寫回呼叫端的 Responses map；Method "get" 原樣保留。
func TestRegisterNormalizesDerivedFields(t *testing.T) {
	var r Registry
	resps := map[int]ResponseSchema{200: {Description: "ok", Type: PEmbedBase{}}}
	r.Register(Route{Method: "get", Path: "/n", Input: PEmbedBase{}, Responses: resps})

	got, ok := r.Get("GET", "/n")
	if !ok {
		t.Fatal("lowercase method not normalized: Get(GET) found nothing")
	}
	if got.Method != "GET" || got.InputName != "PEmbedBase" || got.Responses[200].TypeName != "PEmbedBase" {
		t.Errorf("normalized route = %+v", got)
	}
	if resps[200].TypeName != "" {
		t.Error("caller's Responses map was modified")
	}

	NewSchemaRoute(Route{Responses: resps}, nil)
	if resps[200].TypeName != "" {
		t.Error("NewSchemaRoute modified caller's Responses map")
	}

}
