package schema

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"
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

type RNode struct {
	*RNode
	Val int `json:"val"`
}

type RA struct {
	*RB
	A int `json:"a"`
}

type RB struct {
	*RA
	B int `json:"b"`
}

// TestFieldsOfRecursiveEmbedding 迴歸測試（A1）：自我參照或互相內嵌的指標不會無限遞迴。
// 歷史 bug：展開內嵌指標時不記錄走過的型別，FieldsOf 讓整個行程 stack overflow（recover 救不回）
func TestFieldsOfRecursiveEmbedding(t *testing.T) {
	names := func(fs []FieldInfo) string {
		s := ""
		for _, f := range fs {
			s += f.Name + " "
		}
		return s
	}
	if got := names(FieldsOf(RNode{})); got != "val " {
		t.Errorf("FieldsOf(RNode) = %q, want \"val \"", got)
	}
	if got := names(FieldsOf(RA{})); got != "b a " {
		t.Errorf("FieldsOf(RA) = %q, want \"b a \"", got)
	}
}

type pShapes struct {
	At     time.Time       `json:"at"`
	AtPtr  *time.Time      `json:"at_ptr"`
	Blob   []byte          `json:"blob"`
	Raw    json.RawMessage `json:"raw"`
	Any    interface{}     `json:"any"`
	Quoted int             `json:"quoted,string"`
	IP     net.IP          `json:"ip"`
	Tags   []string        `json:"tags"`
}

// TestFieldsOfJSONShapes 迴歸測試（A2）：型別描述的是實際 JSON 形狀。
// 歷史 bug：只看 reflect.Kind——time.Time 寫成 object、[]byte / json.RawMessage 寫成 array、
// interface{} 寫成 "interface {}"、json:",string" 的數字仍寫 integer
func TestFieldsOfJSONShapes(t *testing.T) {
	got := map[string]string{}
	for _, f := range FieldsOf(pShapes{}) {
		got[f.Name] = f.Type
	}
	want := map[string]string{
		"at": "string", "at_ptr": "string", "blob": "string", "raw": "any",
		"any": "any", "quoted": "string", "ip": "string", "tags": "array",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: type = %q, want %q", k, got[k], w)
		}
	}
	if f := FieldsOf(time.Time{}); f != nil {
		t.Errorf("FieldsOf(time.Time) = %v, want nil (serialized as a string)", f)
	}
}

type PShadowA struct {
	Name string `json:"name"`
	ID   int
}

type PShadowB struct {
	Name string `json:"name,omitempty"`
	ID   int
}

type pShadowOuter struct {
	PShadowA
	Name string `json:"name,omitempty"` // 宣告在內嵌之後，仍遮蔽內嵌的 name
}

type pShadowTagged struct {
	PShadowA
	PShadowC
}

type PShadowC struct {
	ID int `json:"ID"` // 同層只有它帶名稱，勝出
}

// TestJSONFieldsShadowing 迴歸測試（B1）：同名欄位只留 encoding/json 實際輸出的那個。
// 歷史 bug：全部列出——manifest 重複，contract 以被遮蔽的欄位做必填檢查（外層 omitempty 卻被要求必填）
func TestJSONFieldsShadowing(t *testing.T) {
	// 同層內嵌 PShadowA 與 PShadowB：name（兩個都帶名稱）與 ID（都不帶）都衝突，encoding/json 一律不輸出。
	// 以 reflect.StructOf 建立，go vet 的 structtag 檢查才不會把刻意的衝突當成錯誤
	conflict := reflect.New(reflect.StructOf([]reflect.StructField{
		{Name: "PShadowA", Type: reflect.TypeOf(PShadowA{}), Anonymous: true},
		{Name: "PShadowB", Type: reflect.TypeOf(PShadowB{}), Anonymous: true},
	})).Elem().Interface()

	desc := func(v interface{}) string {
		s := ""
		for _, f := range JSONFields(reflect.TypeOf(v)) {
			s += fmt.Sprintf("%s:%v:%s ", f.Name, f.Required, f.StructField.Type)
		}
		return s
	}
	// 對照 encoding/json 的實際輸出
	for _, tc := range []struct {
		v    interface{}
		want string
		json string
	}{
		{pShadowOuter{}, "ID:true:int name:false:string ", `{"ID":0}`},
		{conflict, "", `{}`},
		{pShadowTagged{}, "name:true:string ID:true:int ", `{"name":"","ID":0}`},
	} {
		if got := desc(tc.v); got != tc.want {
			t.Errorf("%T: JSONFields = %q, want %q", tc.v, got, tc.want)
		}
		if b, _ := json.Marshal(tc.v); string(b) != tc.json {
			t.Errorf("%T: test premise wrong, encoding/json gives %s", tc.v, b)
		}
	}
}

// TestTypeNameComposite 迴歸測試（B2）：複合型別的元素也用短名稱。
// 歷史 bug：回 t.String()，"[]schema.PEmbedBase" 與 struct 的 "PEmbedBase" 命名不一致
func TestTypeNameComposite(t *testing.T) {
	cases := []struct {
		v    interface{}
		want string
	}{
		{[]PEmbedBase{}, "[]PEmbedBase"},
		{[]*PEmbedBase{}, "[]PEmbedBase"},
		{&[]PEmbedBase{}, "[]PEmbedBase"},
		{map[string]PEmbedBase{}, "map[string]PEmbedBase"},
		{[2]PEmbedBase{}, "[2]PEmbedBase"},
	}
	for _, c := range cases {
		if got := TypeName(c.v); got != c.want {
			t.Errorf("TypeName(%T) = %q, want %q", c.v, got, c.want)
		}
	}
}

// TestRouteKeyPlatformAndPath 迴歸測試（B3）：非 REST 路由的 key 含 Platform，沒有 Command 時用 Method / Path。
// 歷史 bug：同一個 bot 指令註冊到多個平台互相取代；沒有 Command 的 websocket 路由全部共用 "websocket|"
func TestRouteKeyPlatformAndPath(t *testing.T) {
	var r Registry
	r.Register(Route{Protocol: "bot", Platform: "telegram", Command: "/start"})
	r.Register(Route{Protocol: "bot", Platform: "discord", Command: "/start"})
	r.Register(Route{Protocol: "websocket", Path: "/ws/chat"})
	r.Register(Route{Protocol: "websocket", Path: "/ws/feed"})
	if r.Len() != 4 {
		t.Errorf("Len() = %d, want 4 distinct routes", r.Len())
	}
	if k := (Route{Protocol: "grpc", Command: "S/M"}).RouteKey(); k != "grpc|S/M" {
		t.Errorf("gRPC RouteKey = %q, want unchanged grpc|S/M", k)
	}
}
