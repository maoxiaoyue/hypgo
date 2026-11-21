package schema

import (
	"reflect"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/schema/internal/samename"
)

type r2Page[T any] struct {
	Items []T `json:"items"`
}

type r2Tree []r2Tree
type r2Dict map[string]r2Dict
type r2Holder struct {
	Kids r2Tree  `json:"kids"`
	Dict r2Dict  `json:"dict"`
	Ptr  *r2Tree `json:"ptr"`
}

// 型別名稱一律帶套件名，泛型參數去掉匯入路徑。
// 歷史 bug：struct 用短名稱，不同套件的同名型別在 manifest 互相覆蓋；
// 泛型是 "Page[github.com/x/app/models.User]"
func TestTypeNameIsPackageQualified(t *testing.T) {
	for _, tc := range []struct {
		v    interface{}
		want string
	}{
		{samename.Item{}, "samename.Item"},
		{&samename.Item{}, "samename.Item"},
		{r2Page[samename.Item]{}, "schema.r2Page[samename.Item]"},
		{r2Page[int]{}, "schema.r2Page[int]"},
		{"s", "string"},
		{reflect.Value{}, "reflect.Value"},
		{[]r2Page[samename.Item]{}, "[]schema.r2Page[samename.Item]"},
		{map[string]*samename.Item{}, "map[string]samename.Item"},
	} {
		if got := TypeName(tc.v); got != tc.want {
			t.Errorf("TypeName(%T) = %q, want %q", tc.v, got, tc.want)
		}
	}
	if strings.Contains(TypeName(r2Page[samename.Item]{}), "/") {
		t.Error("import path leaked into the type name")
	}
}

// 自我參照的 slice / map 型別不能無限遞迴。
// 歷史 bug：type Tree []Tree 當欄位時 FieldsOf stack overflow，server 啟動就結束
func TestSelfReferentialContainerTypes(t *testing.T) {
	got := map[string]string{}
	for _, f := range FieldsOf(r2Holder{}) {
		got[f.Name] = f.Type
	}
	want := map[string]string{"kids": "[]schema.r2Tree", "dict": "map[string]schema.r2Dict", "ptr": "[]schema.r2Tree"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
}

func mustPanicWith(t *testing.T, name, fragment string, f func()) {
	t.Helper()
	defer func() {
		p := recover()
		if p == nil {
			t.Errorf("%s: expected a panic", name)
		} else if msg, _ := p.(string); !strings.Contains(msg, fragment) {
			t.Errorf("%s: panic %q does not mention %q", name, p, fragment)
		}
	}()
	f()
}

// Protocol 不分大小寫，REST 一律存成空字串；寫錯的宣告在註冊時 panic。
// 歷史 bug：Protocol: "REST" 被當成另一種協議；沒有斜線的路徑、空的方法、
// 重複而內容不同的命令都照收（後者靜默以新宣告取代）
func TestRegisterNormalizesProtocolAndRejectsBrokenRoutes(t *testing.T) {
	var r Registry
	r.Register(Route{Protocol: " REST ", Method: " get ", Path: "/x"})
	got, ok := r.Get("get", "/x")
	if !ok || got.Protocol != "" || got.Method != "GET" || !got.IsREST() {
		t.Errorf("normalized = %+v (found %v)", got, ok)
	}
	if !(Route{Protocol: "Rest"}).IsREST() || (Route{Protocol: "grpc"}).IsREST() {
		t.Error("IsREST should ignore case")
	}

	r.Register(Route{Protocol: "GRPC", Command: "Svc/Do", Summary: "s"})
	r.Register(Route{Protocol: "grpc", Command: "Svc/Do", Summary: "s"}) // 內容相同：不算衝突
	r.Register(Route{Protocol: "Bot", Platform: "Telegram", Command: "/start", Summary: "s"})
	if all := r.All(); len(all) != 3 || all[1].Protocol != "grpc" || all[2].Protocol != "bot" || all[2].Platform != "telegram" {
		t.Errorf("registry = %+v", all)
	}

	mustPanicWith(t, "different declaration", "already registered", func() {
		r.Register(Route{Protocol: "grpc", Command: "Svc/Do", Summary: "other"})
	})
	mustPanicWith(t, "no slash", "Path", func() { r.Register(Route{Method: "GET", Path: "users"}) })
	mustPanicWith(t, "no method", "Method", func() { r.Register(Route{Path: "/users"}) })
	mustPanicWith(t, "empty command", "Command", func() { r.Register(Route{Protocol: "cli"}) })

	// REST 重複註冊仍以新宣告取代（同一個測試行程裡重建 router 很常見）
	r.Register(Route{Method: "GET", Path: "/x", Summary: "new"})
	if got, _ := r.Get("GET", "/x"); got.Summary != "new" {
		t.Errorf("REST re-registration: %+v", got)
	}
}
