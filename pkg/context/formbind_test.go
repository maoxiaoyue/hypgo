package context

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type formTarget struct {
	Page     int      `form:"page"`
	Size     int64    `json:"size"`
	Ratio    float64  `form:"ratio"`
	Active   bool     `form:"active"`
	Name     string   `form:"user_name"`
	Tags     []string `form:"tags"`
	Ignored  string   `form:"-"`
	IsAdmin  bool     `form:"is_admin"`
	Untagged string
}

// TestFormBindingConvertsTypes 回歸測試：表單綁定必須依欄位型別轉換。
// 歷史 bug：mapFormToStruct 把所有值當字串再走 JSON round-trip，
// 任何非字串欄位必定失敗（?page=2 → "cannot unmarshal string into int"），
// 且完全不認 form tag（form:"user_name" 無效）。
func TestFormBindingConvertsTypes(t *testing.T) {
	vals := url.Values{
		"page":      {"2"},
		"size":      {"100"},
		"ratio":     {"1.5"},
		"active":    {"true"},
		"user_name": {"alice"},
		"tags":      {"a", "b"},
		"Ignored":   {"should-not-bind"},
		"Untagged":  {"plain"},
	}

	var got formTarget
	if err := mapFormToStruct(vals, &got); err != nil {
		t.Fatalf("mapFormToStruct: %v", err)
	}

	if got.Page != 2 {
		t.Errorf("Page = %d, want 2", got.Page)
	}
	if got.Size != 100 {
		t.Errorf("Size = %d, want 100 (json tag fallback)", got.Size)
	}
	if got.Ratio != 1.5 {
		t.Errorf("Ratio = %v, want 1.5", got.Ratio)
	}
	if !got.Active {
		t.Error("Active = false, want true")
	}
	if got.Name != "alice" {
		t.Errorf("Name = %q, want alice (form tag must be honored)", got.Name)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Errorf("Tags = %v, want [a b]", got.Tags)
	}
	if got.Ignored != "" {
		t.Errorf("Ignored = %q, want empty (form:\"-\" must skip)", got.Ignored)
	}
	if got.Untagged != "plain" {
		t.Errorf("Untagged = %q, want plain (fall back to field name)", got.Untagged)
	}
}

// TestFormBindingRejectsQueryMassAssignment 回歸測試（安全）：
// form 綁定只能吃 body，不得吃 query string。
//
// 歷史 bug：bindingForm.Bind 用 req.Form（body ∪ query），攻擊者可用
// POST /api/users?is_admin=true 注入 body 裡沒有的欄位（大量賦值漏洞）。
func TestFormBindingRejectsQueryMassAssignment(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/users?is_admin=true",
		strings.NewReader("user_name=alice"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var got formTarget
	if err := (bindingForm{}).Bind(req, &got); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if got.Name != "alice" {
		t.Errorf("Name = %q, want alice (body field should bind)", got.Name)
	}
	if got.IsAdmin {
		t.Error("IsAdmin was set from the query string — mass assignment vulnerability")
	}
}

// TestDefaultBinderBindsQueryForGET 回歸測試：GET / HEAD 的 ShouldBind 必須綁 query string。
// 歷史 bug：Default(GET) 回 Form 綁定器，而 Form 只讀 PostForm（防大量賦值），GET 的 PostForm
// 永遠是空的 —— GET /items?page=3 無錯誤但 page=0，BindInput 回 true 卻全零值。
func TestDefaultBinderBindsQueryForGET(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		c := AcquireContext(httptest.NewRecorder(), httptest.NewRequest(method, "/items?page=3&user_name=bob", nil))
		var got formTarget
		err := c.ShouldBind(&got)
		c.Release()
		if err != nil {
			t.Fatalf("%s ShouldBind: %v", method, err)
		}
		if got.Page != 3 || got.Name != "bob" {
			t.Errorf("%s ShouldBind = %+v, want page=3 user_name=bob", method, got)
		}
	}
}

// TestBindHeaderAcceptsNonCanonicalTag 回歸測試：header 綁定的 tag 不必寫成 Go 的 canonical 形式。
// 歷史 bug：直接用 url.Values(req.Header) 精確比對，form:"X-API-Key" 對不到 Go 存的 X-Api-Key，
// 綁到空字串且無錯誤。
func TestBindHeaderAcceptsNonCanonicalTag(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "k1")
	req.Header.Set("Content-Type", "text/plain")
	c := AcquireContext(httptest.NewRecorder(), req)
	defer c.Release()

	var got struct {
		Key string `form:"X-API-Key"`
		CT  string `form:"content-type"`
	}
	if err := c.ShouldBindHeader(&got); err != nil {
		t.Fatalf("ShouldBindHeader: %v", err)
	}
	if got.Key != "k1" || got.CT != "text/plain" {
		t.Errorf("ShouldBindHeader = %+v, want Key=k1 CT=text/plain", got)
	}
}

// TestPostFormRecordsParseError 回歸測試：表單解析失敗要記進 c.Errors，不能無聲吞掉。
func TestPostFormRecordsParseError(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("a=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c := AcquireContext(httptest.NewRecorder(), req)
	defer c.Release()

	if v := c.PostForm("a"); v != "" {
		t.Errorf("PostForm(a) = %q, want empty on parse failure", v)
	}
	if len(c.Errors) != 1 {
		t.Fatalf("c.Errors = %v, want exactly one parse error", c.Errors)
	}
	c.PostForm("b") // 第二次呼叫不重跑解析、不重複記錯
	if len(c.Errors) != 1 {
		t.Errorf("c.Errors after second call = %v, want still one", c.Errors)
	}
}
