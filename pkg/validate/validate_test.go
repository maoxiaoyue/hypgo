package validate

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
)

func yes(validator.FieldLevel) bool { return true }
func no(validator.FieldLevel) bool  { return false }

func TestDefaultIsShared(t *testing.T) {
	if Default() == nil || Default() != Default() {
		t.Fatal("Default must return one shared instance")
	}
}

// 先前只看 json：query / form 綁定的欄位回報成 Go 欄位名
func TestFieldNamesInErrors(t *testing.T) {
	type in struct {
		JSON   string `json:"j,omitempty" validate:"required"`
		Form   string `form:"f" validate:"required"`
		Query  string `query:"q" validate:"required"`
		URI    string `uri:"u" validate:"required"`
		Both   string `json:"jb" form:"fb" validate:"required"`
		Hidden string `json:"-" form:"h" validate:"required"`
		Plain  string `validate:"required"`
	}
	var verrs validator.ValidationErrors
	if err := Struct(in{}); !errors.As(err, &verrs) {
		t.Fatalf("err = %v", err)
	}
	var got []string
	for _, fe := range verrs {
		got = append(got, fe.Field())
	}
	if want := "j,f,q,u,jb,h,Plain"; strings.Join(got, ",") != want {
		t.Errorf("fields = %v, want %s", got, want)
	}
}

// 先前 RegisterValidation("email", fn) 回 nil，整個框架的 email 規則被換掉；重複註冊也靜默覆蓋
func TestRegisterValidationRefusesToOverride(t *testing.T) {
	if err := RegisterValidation("vt_even", no); err != nil {
		t.Fatal(err)
	}
	if err := Var(1, "vt_even"); err == nil {
		t.Fatal("custom rule not applied")
	}
	for _, tag := range []string{"email", "vt_even", "required_if"} {
		if err := RegisterValidation(tag, yes); err == nil || !strings.Contains(err.Error(), "OverrideValidation") {
			t.Errorf("RegisterValidation(%q) = %v, want an error pointing at OverrideValidation", tag, err)
		}
	}
	if err := Var("not-an-email", "email"); err == nil {
		t.Error("the built-in email rule was replaced")
	}
	if err := Var(1, "vt_even"); err == nil {
		t.Error("the custom rule was replaced")
	}

	// 只接受整數的規則：以字串試驗它會 panic，仍要認得它已經存在
	intOnly := func(fl validator.FieldLevel) bool { return fl.Field().Int()%2 == 0 }
	if err := RegisterValidation("vt_intonly", intOnly); err != nil {
		t.Fatal(err)
	}
	if err := RegisterValidation("vt_intonly", yes); err == nil {
		t.Error("a rule that panics on a string probe was treated as undefined and replaced")
	}

	// 明確要取代時（要在規則第一次被使用之前：validator 會快取解析過的標籤）
	if err := RegisterValidation("vt_replace", no); err != nil {
		t.Fatal(err)
	}
	if err := OverrideValidation("vt_replace", yes); err != nil {
		t.Fatal(err)
	}
	if err := Var(1, "vt_replace"); err != nil {
		t.Errorf("OverrideValidation did not replace the rule: %v", err)
	}
	if err := OverrideValidation("uppercase", yes); err != nil { // 內建規則
		t.Fatal(err)
	}
	if err := Var("lower", "uppercase"); err != nil {
		t.Errorf("OverrideValidation did not replace the built-in rule: %v", err)
	}
}

// 先前保留名稱讓 validator 直接 panic；RegisterAlias 沒有回傳值
func TestRegistrationErrorsInsteadOfPanics(t *testing.T) {
	for name, fn := range map[string]func() error{
		"restricted tag":            func() error { return RegisterValidation("required", yes) },
		"restricted tag (override)": func() error { return OverrideValidation("dive", yes) },
		"empty tag":                 func() error { return RegisterValidation("", yes) },
		"nil func":                  func() error { return RegisterValidation("vt_nil", nil) },
		"alias: restricted":         func() error { return RegisterAlias("required", "max=1") },
		"alias: existing rule":      func() error { return RegisterAlias("email", "max=1") },
		"alias: unknown rule":       func() error { return RegisterAlias("vt_bad_alias", "required,nosuchrule") },
		"alias: empty":              func() error { return RegisterAlias("", "required") },
		"alias: empty tags":         func() error { return RegisterAlias("vt_empty", "") },
	} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s: panicked: %v", name, p)
				}
			}()
			if err := fn(); err == nil {
				t.Errorf("%s: no error", name)
			}
		}()
	}

	if err := RegisterAlias("vt_short", "required,max=5"); err != nil {
		t.Fatal(err)
	}
	if Var("toolongvalue", "vt_short") == nil || Var("ok", "vt_short") != nil {
		t.Error("alias not applied")
	}
	if err := RegisterAlias("vt_short", "max=9"); err == nil {
		t.Error("an existing alias was replaced silently")
	}
	// 沒有註冊成功的別名不能留下來
	if err := Var("x", "vt_bad_alias"); !errors.Is(err, ErrInvalidTag) {
		t.Errorf("a rejected alias was registered anyway: %v", err)
	}
}

type typo struct {
	Name string `json:"name" validate:"requried"`
}

// 先前標籤打錯時 Struct / Var 直接 panic
func TestInvalidTagIsAnErrorNotAPanic(t *testing.T) {
	err := Struct(typo{Name: "a"})
	if !errors.Is(err, ErrInvalidTag) || !strings.Contains(err.Error(), "requried") {
		t.Errorf("Struct = %v", err)
	}
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		t.Error("a tag error must not look like a data validation error")
	}
	if err := Var("x", "nosuchrule"); !errors.Is(err, ErrInvalidTag) {
		t.Errorf("Var = %v", err)
	}
	// 資料不合格仍是 ValidationErrors
	if err := Var("", "required"); !errors.As(err, &verrs) || errors.Is(err, ErrInvalidTag) {
		t.Errorf("data error = %v", err)
	}
	// 非 struct：回錯、不 panic
	for _, v := range []interface{}{nil, 3, (*typo)(nil)} {
		if Struct(v) == nil {
			t.Errorf("Struct(%v) = nil", v)
		}
	}
}

type addr struct {
	City string `json:"city" validate:"required"`
}

type noRules struct{ Note string }

type deepTypo struct {
	Inner []struct {
		X int `validate:"gte=1,nosuch"`
	} `json:"inner" validate:"dive"`
}

type order struct {
	Items    []addr           `json:"items"`                 // 缺 dive
	ByKey    map[string]*addr `json:"by_key"`                // 缺 dive
	Fixed    [2]addr          `form:"fixed"`                 // 缺 dive（名稱取 form）
	Dived    []addr           `json:"dived" validate:"dive"` // 有 dive
	Min      []addr           `json:"min" validate:"min=1,dive"`
	Skipped  []addr           `json:"skipped" validate:"-"` // 刻意不驗證
	Plain    []noRules        `json:"plain"`                // 元素沒有規則
	Tags     []string         `json:"tags"`
	Addr     addr             `json:"addr"` // 巢狀 struct 會自動驗證
	Ptr      *addr            `json:"ptr"`
	When     time.Time        `json:"when" validate:"required"`
	private  []addr
	Children []order `json:"children" validate:"dive"` // 自我參照
}

type tree []tree

func issueSet(issues []Issue) string {
	var out []string
	for _, i := range issues {
		out = append(out, i.Kind+":"+i.Field)
	}
	return strings.Join(out, " ")
}

// 啟動 lint 用：不需要資料就找出打錯的標籤與缺少的 dive
func TestCheckType(t *testing.T) {
	_ = order{}.private
	got := issueSet(CheckType(order{}))
	want := "missing_dive:validate.order.items missing_dive:validate.order.by_key missing_dive:validate.order.fixed"
	if got != want {
		t.Errorf("issues = %s\nwant     %s", got, want)
	}
	if msg := CheckType(order{})[0].Message; !strings.Contains(msg, `validate:"dive"`) || !strings.Contains(msg, `validate:"-"`) {
		t.Errorf("message does not say how to fix it: %s", msg)
	}

	// 指標、slice、map 包著的型別同樣檢查
	for name, v := range map[string]interface{}{"value": typo{}, "pointer": &typo{}, "slice": []typo{}, "map": map[string]*typo{}} {
		issues := CheckType(v)
		if len(issues) != 1 || issues[0].Kind != IssueInvalidTag || issues[0].Field != "validate.typo" || !strings.Contains(issues[0].Message, "requried") {
			t.Errorf("%s: issues = %+v", name, issues)
		}
		if strings.Contains(issues[0].Message, ErrInvalidTag.Error()) {
			t.Errorf("%s: message repeats the sentinel: %s", name, issues[0].Message)
		}
	}
	// 打錯的標籤在集合元素的 struct 裡：沒有資料時 validator 不會走到，要靠型別走訪
	if got := issueSet(CheckType(deepTypo{})); !strings.HasPrefix(got, "invalid_tag:") {
		t.Errorf("typo inside a slice element not found: %q", got)
	}

	for name, v := range map[string]interface{}{"nil": nil, "int": 3, "no rules": noRules{}, "valid": addr{}, "self-referential": tree{}} {
		done := make(chan []Issue, 1)
		go func() { done <- CheckType(v) }()
		select {
		case issues := <-done:
			if len(issues) != 0 {
				t.Errorf("%s: issues = %+v", name, issues)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: CheckType did not return", name)
		}
	}
}

// 先前兩個 goroutine 同時註冊會讓行程以 concurrent map writes 結束
func TestConcurrentRegistrationDoesNotCrash(t *testing.T) {
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				name := "vt_c" + string(rune('a'+i)) + string(rune('a'+j%26)) + string(rune('a'+j/26))
				if err := RegisterValidation(name, yes); err != nil {
					t.Errorf("%s: %v", name, err)
				}
				RegisterAlias(name+"_alias", "required")
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
