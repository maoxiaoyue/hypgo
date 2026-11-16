package contract

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
	hypvalidate "github.com/maoxiaoyue/hypgo/pkg/validate"
)

type r2Profile struct {
	Bio string `json:"bio"`
}

type r2Out struct {
	ID      int       `json:"id"`
	Name    string    `json:"name" validate:"min=2"`
	Profile r2Profile `json:"profile"`
	Tags    []string  `json:"tags"`
	At      time.Time `json:"at"`
}

type r2Err struct {
	Code string `json:"code"`
}

type r2In struct {
	Name string `json:"name" validate:"required,min=2"`
}

const r2Good = `{"id":1,"name":"ab","profile":{"bio":"x"},"tags":null,"at":"2026-06-10T00:00:00Z"}`

func r2Router(t *testing.T, routes map[string]string) *router.Router {
	t.Helper()
	schema.Global().Reset()
	t.Cleanup(schema.Global().Reset)
	r := router.New()
	for path, body := range routes {
		var out interface{} = r2Out{}
		if strings.HasPrefix(path, "/list") {
			out = []r2Out{}
		}
		r.Schema(schema.Route{Method: "GET", Path: path, Summary: "s", Output: out}).Handle(func(c *hypcontext.Context) {
			c.Data(200, "application/json", []byte(body))
		})
	}
	return r
}

// 回應的形狀要逐層檢查，失敗訊息要指出是哪個欄位。
// 歷史 bug：只看最外層的 struct——宣告 []Out 而回 [{"wrong":1}]、巢狀必填缺漏、必填欄位是 null 都通過
func TestResponseShapeIsCheckedDeeply(t *testing.T) {
	cases := map[string]struct {
		body string
		want []string // 失敗原因要包含的片段；空 = 應該通過
	}{
		"/ok":          {r2Good, nil},
		"/list-ok":     {"[" + r2Good + "," + r2Good + "]", nil},
		"/list-empty":  {"[]", nil},
		"/list-wrong":  {`[` + r2Good + `,{"wrong":1}]`, []string{"[1].id is missing", "[1].profile is missing"}},
		"/nested":      {`{"id":1,"name":"ab","profile":{},"tags":[],"at":"2026-06-10T00:00:00Z"}`, []string{"profile.bio is missing"}},
		"/null-field":  {`{"id":null,"name":"ab","profile":null,"tags":null,"at":"2026-06-10T00:00:00Z"}`, []string{"id is null", "profile is null"}},
		"/null-body":   {`null`, []string{"body is null"}},
		"/list-constr": {`[` + r2Good + `,{"id":1,"name":"a","profile":{"bio":""},"tags":[],"at":"2026-06-10T00:00:00Z"}]`, []string{"[1].name must be >= 2"}},
	}
	routes := map[string]string{}
	for path, tc := range cases {
		routes[path] = tc.body
	}
	r := r2Router(t, routes)
	for path, tc := range cases {
		pass, reason, _ := runTestOnce(r, TestCase{Route: "GET " + path, ExpectStatus: 200, ExpectSchema: true})
		if len(tc.want) == 0 {
			if !pass {
				t.Errorf("%s: should pass, got %s", path, reason)
			}
			continue
		}
		if pass {
			t.Errorf("%s: passed, want a failure mentioning %v", path, tc.want)
		}
		for _, w := range tc.want {
			if !strings.Contains(reason, w) {
				t.Errorf("%s: reason %q does not mention %q", path, reason, w)
			}
		}
	}
	// tags 是 nil slice（Go 序列化成 null）不算缺漏
	if strings.Contains(cases["/ok"].body, `"tags":[]`) {
		t.Fatal("the passing case must use a null slice")
	}
}

// 宣告了 JSON 型別的回應要以 JSON 送出。歷史 bug：text/html 的 JSON 也通過
func TestResponseContentTypeIsChecked(t *testing.T) {
	route := schema.Route{Output: r2Out{}, OutputName: "r2Out"}
	tc := TestCase{ExpectStatus: 200}
	for ct, want := range map[string]bool{
		"application/json":                true,
		"application/json; charset=utf-8": true,
		"application/problem+json":        true,
		"text/html; charset=utf-8":        false,
		"":                                false,
	} {
		_, pass, reason := validateExchange(route, tc, 200, ct, []byte(r2Good))
		if pass != want || (!want && !strings.Contains(reason, "Content-Type")) {
			t.Errorf("Content-Type %q: pass = %v (%s), want %v", ct, pass, reason, want)
		}
	}
}

// StrictFields：未宣告的欄位算不符；預設不檢查
func TestStrictFields(t *testing.T) {
	extra := strings.Replace(r2Good, `"profile":{"bio":"x"}`, `"profile":{"bio":"x","secret":1}`, 1)
	extra = strings.Replace(extra, `{"id"`, `{"password_hash":"h","id"`, 1)
	r := r2Router(t, map[string]string{"/x": extra})
	if pass, reason, _ := runTestOnce(r, TestCase{Route: "GET /x", ExpectStatus: 200, ExpectSchema: true}); !pass {
		t.Errorf("extra fields should pass by default: %s", reason)
	}
	pass, reason, _ := runTestOnce(r, TestCase{Route: "GET /x", ExpectStatus: 200, ExpectSchema: true, StrictFields: true})
	if pass || !strings.Contains(reason, "password_hash is not declared in contract.r2Out") || !strings.Contains(reason, "profile.secret is not declared in contract.r2Profile") {
		t.Errorf("strict: pass = %v, reason = %s", pass, reason)
	}
}

// 測錯誤路徑：期望非 2xx 時不拿 Input 驗送出的 body，宣告的錯誤型別照驗。
// 歷史 bug：故意送不合格的輸入、期望 422 並開 ExpectSchema，測試被自己的 Input 檢查判失敗
func TestErrorPathIsTestable(t *testing.T) {
	schema.Global().Reset()
	t.Cleanup(schema.Global().Reset)
	r := router.New()
	r.Schema(schema.Route{Method: "POST", Path: "/u", Summary: "s", Input: r2In{}, Output: r2Out{},
		Responses: map[int]schema.ResponseSchema{422: {Description: "bad", Type: r2Err{}}},
	}).Handle(func(c *hypcontext.Context) {
		var in r2In
		if !c.BindInput(&in) {
			return
		}
		c.JSON(201, r2Out{})
	})
	if pass, reason, _ := runTestOnce(r, TestCase{Route: "POST /u", Input: `{"name":"x"}`, ExpectStatus: 422, ExpectSchema: true}); !pass {
		t.Errorf("error path: %s", reason)
	}
	// 期望成功時仍然檢查 Input
	if pass, reason, _ := runTestOnce(r, TestCase{Route: "POST /u", Input: `{"name":"x"}`, ExpectStatus: 201, ExpectSchema: true}); pass || !strings.Contains(reason, "request schema validation failed") {
		t.Errorf("success path with bad input: pass = %v, reason = %s", pass, reason)
	}
}

// TestAll / Observe 只測傳入的 router 有註冊的 REST 路由；RunConfig 的 Headers / Params / Skip 生效。
// 歷史 bug：取全域 registry，別的 router 的路由被拿來打而 404；無法帶標頭，需要授權的路由一律 401；
// :id 只能是 1
func TestRunConfigHeadersParamsSkipAndRouterScope(t *testing.T) {
	schema.Global().Reset()
	t.Cleanup(schema.Global().Reset)
	var gotAuth, gotID string
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/items/:id", Summary: "s", Output: r2Out{}}).Handle(func(c *hypcontext.Context) {
		gotAuth, gotID = c.GetHeader("Authorization"), c.Param("id")
		if gotAuth == "" {
			c.Status(401)
			return
		}
		c.Data(200, "application/json", []byte(r2Good))
	})
	r.Schema(schema.Route{Method: "DELETE", Path: "/admin/wipe", Summary: "s"}).Handle(func(c *hypcontext.Context) { c.Status(500) })
	other := router.New()
	other.Schema(schema.Route{Method: "GET", Path: "/other", Summary: "s", Output: r2Out{}}).Handle(func(c *hypcontext.Context) {})
	schema.RegisterGRPC("Svc/Do", "s", nil, nil)

	var names []string
	for _, rt := range routesOf(r) {
		names = append(names, rt.Protocol+rt.Method+rt.Path+rt.Command)
	}
	if got := strings.Join(names, ","); strings.Contains(got, "/other") || !strings.Contains(got, "/items/:id") || !strings.Contains(got, "Svc/Do") {
		t.Errorf("routesOf = %s", got)
	}

	cfg := RunConfig{
		Headers: map[string]string{"Authorization": "Bearer t"},
		Params:  map[string]string{"id": "42"},
		Skip:    []string{"/admin/*"},
	}
	ok := t.Run("all", func(t *testing.T) { TestAll(t, r, cfg) })
	if !ok || gotAuth != "Bearer t" || gotID != "42" {
		t.Errorf("TestAll ok = %v, Authorization = %q, id = %q", ok, gotAuth, gotID)
	}
	res := ObserveAll(nil, r, ObserveOptions{OutputPath: t.TempDir() + "/o.html", Run: cfg})
	if len(res) != 1 || !res[0].Pass || res[0].Request.Path != "/items/42" {
		t.Errorf("observe = %+v", res)
	}

	for pat, want := range map[string]bool{
		"DELETE /admin/wipe": true, "/admin/wipe": true, "/admin/*": true, "DELETE /adm*": true,
		"GET /admin/wipe": false, "/admin": false, "GET *": false,
	} {
		if got := (RunConfig{Skip: []string{pat}}).skips(schema.Route{Method: "DELETE", Path: "/admin/wipe"}); got != want {
			t.Errorf("Skip %q = %v, want %v", pat, got, want)
		}
	}
}

// 什麼都沒要求的 Test 要失敗；負的 RetryCount 當 0。
// 歷史 bug：TestCase{Route: "GET /nope"}（實際 404）是綠的；RetryCount: -1 讓每條路由以空的原因失敗
func TestEmptyExpectationsAndNegativeRetry(t *testing.T) {
	r := r2Router(t, map[string]string{"/ok": r2Good})
	if pass, reason, _ := runTestOnce(r, TestCase{Route: "GET /nope"}); pass || !strings.Contains(reason, "nothing to check") {
		t.Errorf("empty expectations: pass = %v, reason = %q", pass, reason)
	}
	if cfg := mergeRunConfig([]RunConfig{{RetryCount: -1}}); cfg.RetryCount != 0 {
		t.Errorf("RetryCount = %d", cfg.RetryCount)
	}
	if !t.Run("all", func(t *testing.T) { TestAll(t, r, RunConfig{RetryCount: -1}) }) {
		t.Error("TestAll with a negative RetryCount failed")
	}
}

type r2Gen struct {
	Rate   float64 `json:"rate" validate:"gt=0,lt=1"`
	Ratio  float64 `json:"r2" validate:"gt=5"`
	Alpha  string  `json:"alpha" validate:"required,alpha,min=4"`
	Num    string  `json:"num" validate:"required,numeric,len=6"`
	Hex    string  `json:"hex" validate:"required,hexadecimal,max=4"`
	Mail   string  `json:"mail" validate:"required,email,max=12"`
	Mail2  string  `json:"mail2" validate:"required,email,min=30"`
	NotOne int     `json:"not_one" validate:"required,ne=1"`
	NotStr string  `json:"not_str" validate:"required,ne=test"`
	Prefix string  `json:"prefix" validate:"startswith=ab,min=5"`
	Suffix string  `json:"suffix" validate:"endswith=.go"`
	Has    string  `json:"has" validate:"required,contains=@"`
	Day    string  `json:"day" validate:"datetime=2006-01-02"`
	Stamp  string  `json:"stamp" validate:"required,datetime=2006-01-02T15:04:05Z07:00"`
}

// 自動產生的輸入要過得了它自己的 validate 規則。
// 歷史 bug：gt=0,lt=1 得到 0、alpha,min=4 得到 "abc"、email,max=12 得到 16 個字元、ne=1 得到 1，
// startswith / contains / datetime 一律是 "test"
func TestGeneratedInputSatisfiesItsRules(t *testing.T) {
	raw := generateMinimalJSON(r2Gen{})
	var g r2Gen
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if err := hypvalidate.Default().Struct(g); err != nil {
		t.Errorf("generated %s\n%v", raw, err)
	}
}

type r2Unknown struct {
	Shade string `json:"shade" validate:"required,hexcolor"`
}

// 產生器處理不了的規則：失敗訊息要說是產生的輸入不合格、指出欄位與規則，並給出處理方式
func TestUnsupportedRuleIsReportedAsGeneratorLimit(t *testing.T) {
	schema.Global().Reset()
	t.Cleanup(schema.Global().Reset)
	r := router.New()
	r.Schema(schema.Route{Method: "POST", Path: "/c", Summary: "s", Input: r2Unknown{}}).Handle(func(c *hypcontext.Context) {
		var in r2Unknown
		if !c.BindInput(&in) {
			return
		}
		c.Status(201)
	})
	route, _ := schema.Global().Get("POST", "/c")
	pass, reason, _ := runTestOnce(r, RunConfig{}.testCase(*route))
	for _, w := range []string{"auto-generated input", "shade failed hexcolor", "RegisterSampleValue", "RunConfig.Skip"} {
		if pass || !strings.Contains(reason, w) {
			t.Errorf("pass = %v, reason %q does not mention %q", pass, reason, w)
		}
	}
}

type r2Node struct {
	Val  int       `json:"val"`
	Next *r2Node   `json:"next"`
	Kids []r2Node  `json:"kids" validate:"min=1"`
	Tree r2SelfRef `json:"tree"`
}

type r2SelfRef []r2SelfRef

// 自我參照的 Input 不能讓產生器無限遞迴。
// 歷史 bug：type Node struct{ Next *Node } 當 Input 時 TestAll stack overflow
func TestGeneratorHandlesSelfReferentialTypes(t *testing.T) {
	raw := generateMinimalJSON(r2Node{})
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if got["val"] == nil || got["next"] != nil {
		t.Errorf("generated %s, want val set and next null", raw)
	}
	var n r2Node
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Errorf("generated input does not decode into its own type: %v (%s)", err, raw)
	}
}

// 方法名稱不分大小寫：Route 寫 "get /ok"、Skip 寫 "delete /x"、TestRoute 給 "get" 都與大寫相同。
// 歷史 bug："get /ok" 送出小寫的方法而得到 405
func TestMethodNamesAreCaseInsensitive(t *testing.T) {
	r := r2Router(t, map[string]string{"/ok": r2Good})
	if pass, reason, _ := runTestOnce(r, TestCase{Route: "get /ok", ExpectStatus: 200, ExpectSchema: true}); !pass {
		t.Errorf("lowercase method in Route: %s", reason)
	}
	if !(RunConfig{Skip: []string{"delete /admin/wipe"}}).skips(schema.Route{Method: "DELETE", Path: "/admin/wipe"}) ||
		!(RunConfig{Skip: []string{"delete /adm*"}}).skips(schema.Route{Method: "DELETE", Path: "/admin/wipe"}) {
		t.Error("Skip should match the method case-insensitively")
	}
	if !t.Run("TestRoute", func(t *testing.T) { TestRoute(t, r, "get", "/ok", 200) }) {
		t.Error("TestRoute with a lowercase method failed")
	}
}
