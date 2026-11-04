package errors

import (
	"encoding/json"
	stderrors "errors"
	"io"
	"strings"
	"testing"
)

// mustPanic 回報 fn 是否 panic
func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic", name)
		}
	}()
	fn()
}

// TestDefineRejectsDuplicateCode 迴歸測試（A1、A2）：重複定義同一個錯誤碼（含框架保留的內建碼）時 panic。
// 歷史 bug：靜默覆蓋 catalog——文件範例 Define("E1001", 404, "User not found") 蓋掉內建的
// E1001 Validation failed，而 c.BindInput 驗證失敗回的仍是 E1001，同一個碼代表兩種錯誤
func TestDefineRejectsDuplicateCode(t *testing.T) {
	GlobalCatalog().Reset() // 其他測試會 Reset，先把內建的 E1001 登回去
	defer GlobalCatalog().Reset()
	GlobalCatalog().register(ErrValidationFailed)
	mustPanic(t, "reserved built-in code", func() { Define("E1001", 404, "User not found", "users") })

	Define("USER_001", 404, "User not found", "users")
	mustPanic(t, "duplicate app code", func() { Define("USER_001", 409, "User exists", "users") })
	if e, _ := GlobalCatalog().Get("USER_001"); e.Message != "User not found" {
		t.Errorf("first definition was overwritten: %q", e.Message)
	}
}

// TestDefineRejectsInvalidStatus 迴歸測試（B2）：HTTP 狀態碼不在 100–599 時 panic。
// 歷史 bug：狀態碼 0 的錯誤回應實際以 200 送出
func TestDefineRejectsInvalidStatus(t *testing.T) {
	GlobalCatalog().Reset()
	defer GlobalCatalog().Reset()
	mustPanic(t, "status 0", func() { Define("BAD_0", 0, "x", "x") })
	mustPanic(t, "status 600", func() { Define("BAD_600", 600, "x", "x") })
}

// TestWrapKeepsCauseOutOfJSON 迴歸測試（B1）：Wrap 的底層錯誤可經 errors.Is 取得，但不寫進回應。
// 歷史 bug：只能 With("err", err)，error 值序列化成 {}，回應只剩 "err":{}，也無法 Unwrap
func TestWrapKeepsCauseOutOfJSON(t *testing.T) {
	err := ErrInternalError.Wrap(io.ErrUnexpectedEOF)
	if !stderrors.Is(err, io.ErrUnexpectedEOF) || !stderrors.Is(err, ErrInternalError) {
		t.Error("errors.Is should match both the cause and the AppError")
	}
	if !strings.Contains(err.Error(), io.ErrUnexpectedEOF.Error()) {
		t.Errorf("Error() should include the cause for logs: %q", err.Error())
	}
	b, _ := json.Marshal(err.JSON())
	if strings.Contains(string(b), "EOF") || strings.Contains(string(b), "{}") {
		t.Errorf("cause leaked into the response: %s", b)
	}
	if ErrInternalError.Unwrap() != nil {
		t.Error("Wrap modified the original AppError")
	}
}

// TestCatalogAllSorted Catalog.All() 依錯誤碼排序：manifest 據此輸出，順序不固定時每次啟動都會改寫 context.yaml
func TestCatalogAllSorted(t *testing.T) {
	GlobalCatalog().Reset()
	defer GlobalCatalog().Reset()
	for _, c := range []string{"C3", "A1", "B2"} {
		Define(c, 400, c, "x")
	}
	var got []string
	for _, e := range GlobalCatalog().All() {
		got = append(got, e.Code)
	}
	if strings.Join(got, ",") != "A1,B2,C3" {
		t.Errorf("All() = %v, want sorted by code", got)
	}
}
