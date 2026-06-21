package contract

import (
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	hypvalidate "github.com/maoxiaoyue/hypgo/pkg/validate"
)

type widget struct {
	Code string `json:"code" validate:"required,shortcode"`
}

// 驗證 item 3：app 註冊的自訂規則同時被
//   - 生成器（RegisterSampleValue 餵入合法樣本）
//   - pkg/contract 的合約驗證（共用 registry）
// 看見
func TestSharedValidatorCustomRule(t *testing.T) {
	// app 在共用 registry 註冊自訂規則：剛好 4 個大寫字母
	err := hypvalidate.RegisterValidation("shortcode", func(fl validator.FieldLevel) bool {
		s := fl.Field().String()
		if len(s) != 4 {
			return false
		}
		for _, r := range s {
			if r < 'A' || r > 'Z' {
				return false
			}
		}
		return true
	})
	if err != nil {
		t.Fatalf("register custom validation: %v", err)
	}
	// 教生成器如何產生通過該規則的值
	RegisterSampleValue("shortcode", "ABCD")

	// 1. 生成端：自動生成的 input 應採用註冊的範例值
	gen := generateMinimalJSON(widget{})
	if !strings.Contains(gen, "ABCD") {
		t.Errorf("generated JSON should use registered sample, got %s", gen)
	}

	// 2. 生成 → 驗證 round-trip：生成值必須通過自訂規則
	if err := validateRequest([]byte(gen), widget{}); err != nil {
		t.Errorf("generated value should satisfy custom rule: %v", err)
	}

	// 3. contract 驗證端：違反自訂規則的值必須被攔下
	if err := validateRequest([]byte(`{"code":"xx"}`), widget{}); err == nil {
		t.Error("contract: value violating custom rule should be rejected")
	}
}

// 內建格式 e164：生成端產生合法 E.164，驗證端強制
func TestSharedValidatorE164(t *testing.T) {
	type contact struct {
		Phone string `json:"phone" validate:"required,e164"`
	}

	gen := generateMinimalJSON(contact{})
	if err := validateRequest([]byte(gen), contact{}); err != nil {
		t.Errorf("generated e164 should pass: %v (json=%s)", err, gen)
	}
	if err := validateRequest([]byte(`{"phone":"not-a-number"}`), contact{}); err == nil {
		t.Error("invalid e164 should be rejected")
	}
}
