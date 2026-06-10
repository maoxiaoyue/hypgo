package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// 帶 validate: tag 的 struct，涵蓋 required / min / max / email / oneof / 數值範圍
type signupReq struct {
	Username string  `json:"username" validate:"required,min=3,max=20"`
	Email    string  `json:"email" validate:"required,email"`
	Age      int     `json:"age" validate:"required,gte=18,lte=120"`
	Role     string  `json:"role" validate:"required,oneof=admin user guest"`
	Price    float64 `json:"price" validate:"required,gt=0"`
	Accepted bool    `json:"accepted" validate:"required"`
}

// --- 生成 → 驗證 round-trip：自動生成的值必須能通過自身的約束 ---

func TestGenerateSatisfiesConstraints(t *testing.T) {
	generated := generateMinimalJSON(signupReq{})

	// 自動生成的 input 必須通過 validateRequest（含 validate: 約束）
	if err := validateRequest([]byte(generated), signupReq{}); err != nil {
		t.Fatalf("generated JSON should satisfy its own constraints, got: %v\nJSON: %s", err, generated)
	}

	var parsed signupReq
	if err := json.Unmarshal([]byte(generated), &parsed); err != nil {
		t.Fatalf("generated JSON not parseable: %v", err)
	}

	// 逐項檢查語意/約束是否被尊重
	if !strings.Contains(parsed.Email, "@") {
		t.Errorf("email = %q, want a valid email", parsed.Email)
	}
	if parsed.Age < 18 || parsed.Age > 120 {
		t.Errorf("age = %d, want within [18,120]", parsed.Age)
	}
	if parsed.Role != "admin" {
		t.Errorf("role = %q, want first oneof option %q", parsed.Role, "admin")
	}
	if parsed.Price <= 0 {
		t.Errorf("price = %v, want > 0", parsed.Price)
	}
	if !parsed.Accepted {
		t.Errorf("accepted = false, want true (required bool)")
	}
	if l := len(parsed.Username); l < 3 || l > 20 {
		t.Errorf("username length = %d, want within [3,20]", l)
	}
}

// --- 違反約束的 payload 必須被攔下 ---

func TestValidateConstraintsCatchesViolations(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"bad email", `{"username":"alice","email":"not-an-email","age":30,"role":"user","price":1,"accepted":true}`},
		{"age too low", `{"username":"alice","email":"a@b.com","age":5,"role":"user","price":1,"accepted":true}`},
		{"age too high", `{"username":"alice","email":"a@b.com","age":999,"role":"user","price":1,"accepted":true}`},
		{"role not in oneof", `{"username":"alice","email":"a@b.com","age":30,"role":"root","price":1,"accepted":true}`},
		{"username too short", `{"username":"ab","email":"a@b.com","age":30,"role":"user","price":1,"accepted":true}`},
		{"price not positive", `{"username":"alice","email":"a@b.com","age":30,"role":"user","price":0,"accepted":true}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRequest([]byte(tc.body), signupReq{}); err == nil {
				t.Errorf("expected constraint violation to be caught, but passed: %s", tc.body)
			}
		})
	}
}

func TestValidateConstraintsValidPayload(t *testing.T) {
	body := `{"username":"alice","email":"alice@example.com","age":30,"role":"guest","price":9.99,"accepted":true}`
	if err := validateRequest([]byte(body), signupReq{}); err != nil {
		t.Errorf("valid payload should pass: %v", err)
	}
}

// --- 無 validate: tag 的 struct 必須維持向後相容（不引入新失敗）---

func TestValidateConstraintsBackwardCompat(t *testing.T) {
	// createReq / userResp 無 validate tag，約束檢查應一律通過
	if err := validateRequest([]byte(`{"name":"x","email":"y"}`), createReq{}); err != nil {
		t.Errorf("tag-less struct should pass constraint check: %v", err)
	}
	if err := validateResponse([]byte(`{"id":1,"name":"x","email":"y"}`), userResp{}); err != nil {
		t.Errorf("tag-less struct should pass constraint check: %v", err)
	}
}

// --- 欄位名稱語意生成（無 tag 時也填合理值）---

func TestGenerateFieldNameHeuristics(t *testing.T) {
	type profile struct {
		FullName string  `json:"full_name"`
		Email    string  `json:"email"`
		Age      int     `json:"age"`
		Price    float64 `json:"price"`
		Website  string  `json:"website_url"`
	}

	var p profile
	if err := json.Unmarshal([]byte(generateMinimalJSON(profile{})), &p); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if !strings.Contains(p.Email, "@") {
		t.Errorf("email heuristic failed: %q", p.Email)
	}
	if !strings.HasPrefix(p.Website, "http") {
		t.Errorf("url heuristic failed: %q", p.Website)
	}
	if p.Age != 25 {
		t.Errorf("age heuristic = %d, want 25", p.Age)
	}
	if p.Price <= 0 {
		t.Errorf("price heuristic = %v, want > 0", p.Price)
	}
}
