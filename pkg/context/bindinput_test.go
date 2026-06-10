package context

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type biUser struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

// biCtx 建立一個帶 JSON body 的測試 Context
func biCtx(method, body string) (*Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, "/api/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	return New(w, req), w
}

func TestBindInputSuccess(t *testing.T) {
	c, _ := biCtx("POST", `{"name":"alice","email":"alice@test.com"}`)
	var u biUser
	if !c.BindInput(&u) {
		t.Fatal("expected BindInput to succeed")
	}
	if u.Name != "alice" || u.Email != "alice@test.com" {
		t.Errorf("unexpected bound value: %+v", u)
	}
	if !c.BindInputCalled() {
		t.Error("BindInputCalled() should be true after BindInput")
	}
}

func TestBindInputValidationFails(t *testing.T) {
	c, w := biCtx("POST", `{"name":"a","email":"not-an-email"}`)
	var u biUser
	if c.BindInput(&u) {
		t.Fatal("expected BindInput to fail validation")
	}
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if body["code"] != "E1001" {
		t.Errorf("expected error code E1001, got %v", body["code"])
	}
	if _, ok := body["details"]; !ok {
		t.Error("expected details with field errors")
	}
}

func TestBindInputParseFails(t *testing.T) {
	c, w := biCtx("POST", `{invalid json`)
	var u biUser
	if c.BindInput(&u) {
		t.Fatal("expected BindInput to fail parsing")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != "E0002" {
		t.Errorf("expected error code E0002, got %v", body["code"])
	}
}

func TestBindInputTypeMismatchReports(t *testing.T) {
	var fired [3]string
	SetBindInputReporter(func(routeKey, declared, bound string) {
		fired = [3]string{routeKey, declared, bound}
	})
	defer SetBindInputReporter(nil)

	c, _ := biCtx("POST", `{"name":"alice","email":"alice@test.com"}`)
	// schema 宣告 Input = biUser，但 handler 卻綁定到 other（靜默失敗情境）
	c.SetSchemaInput("rest|POST /api/users", biUser{})

	type other struct {
		Name string `json:"name"`
	}
	var o other
	c.BindInput(&o)

	if fired[0] != "rest|POST /api/users" {
		t.Errorf("reporter routeKey = %q, want rest|POST /api/users", fired[0])
	}
	if !strings.Contains(fired[1], "biUser") || !strings.Contains(fired[2], "other") {
		t.Errorf("reporter declared/bound = %q / %q, want biUser / other", fired[1], fired[2])
	}
}

func TestBindInputNoReporterNoMismatchNoop(t *testing.T) {
	// 型別一致時不應觸發回報
	called := false
	SetBindInputReporter(func(string, string, string) { called = true })
	defer SetBindInputReporter(nil)

	c, _ := biCtx("POST", `{"name":"alice","email":"alice@test.com"}`)
	c.SetSchemaInput("rest|POST /api/users", biUser{})
	var u biUser
	if !c.BindInput(&u) {
		t.Fatal("expected success")
	}
	if called {
		t.Error("reporter should not fire when bound type matches declared type")
	}
}
