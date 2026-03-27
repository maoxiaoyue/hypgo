package errors

import (
	"encoding/json"
	"errors"
	"testing"
)

func init() {
	// 每個測試檔案重置 catalog（預定義錯誤在 init 前已註冊）
}

// --- Define & Catalog ---

func TestDefine(t *testing.T) {
	GlobalCatalog().Reset()

	e := Define("T0001", 404, "Test error", "test")

	if e.Code != "T0001" {
		t.Errorf("Code = %q, want %q", e.Code, "T0001")
	}
	if e.HTTPStatus != 404 {
		t.Errorf("HTTPStatus = %d, want 404", e.HTTPStatus)
	}
	if e.Message != "Test error" {
		t.Errorf("Message = %q, want %q", e.Message, "Test error")
	}
	if e.Category != "test" {
		t.Errorf("Category = %q, want %q", e.Category, "test")
	}
}

func TestCatalogGet(t *testing.T) {
	GlobalCatalog().Reset()
	Define("T1001", 400, "Bad", "test")

	got, ok := GlobalCatalog().Get("T1001")
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.Code != "T1001" {
		t.Errorf("Code = %q, want %q", got.Code, "T1001")
	}
}

func TestCatalogGetNotFound(t *testing.T) {
	GlobalCatalog().Reset()

	_, ok := GlobalCatalog().Get("NONEXISTENT")
	if ok {
		t.Error("should return false for nonexistent code")
	}
}

func TestCatalogAll(t *testing.T) {
	GlobalCatalog().Reset()
	Define("T2001", 400, "A", "a")
	Define("T2002", 500, "B", "b")

	all := GlobalCatalog().All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d, want 2", len(all))
	}
}

func TestCatalogByCategory(t *testing.T) {
	GlobalCatalog().Reset()
	Define("T3001", 400, "A", "alpha")
	Define("T3002", 400, "B", "alpha")
	Define("T3003", 500, "C", "beta")

	alpha := GlobalCatalog().ByCategory("alpha")
	if len(alpha) != 2 {
		t.Errorf("ByCategory(alpha) = %d, want 2", len(alpha))
	}

	beta := GlobalCatalog().ByCategory("beta")
	if len(beta) != 1 {
		t.Errorf("ByCategory(beta) = %d, want 1", len(beta))
	}
}

func TestCatalogLen(t *testing.T) {
	GlobalCatalog().Reset()
	Define("T4001", 400, "X", "x")
	if GlobalCatalog().Len() != 1 {
		t.Errorf("Len() = %d, want 1", GlobalCatalog().Len())
	}
}

func TestCatalogReset(t *testing.T) {
	GlobalCatalog().Reset()
	Define("T5001", 400, "X", "x")
	GlobalCatalog().Reset()
	if GlobalCatalog().Len() != 0 {
		t.Error("Reset should clear catalog")
	}
}

// --- AppError methods ---

func TestAppErrorError(t *testing.T) {
	e := &AppError{Code: "E1", Message: "fail"}
	if e.Error() != "[E1] fail" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestAppErrorErrorWithDetails(t *testing.T) {
	e := &AppError{Code: "E1", Message: "fail", Details: map[string]any{"id": 42}}
	got := e.Error()
	if got != "[E1] fail map[id:42]" {
		t.Errorf("Error() = %q", got)
	}
}

func TestAppErrorWith(t *testing.T) {
	original := &AppError{Code: "E1", HTTPStatus: 404, Message: "Not found", Category: "test"}
	withDetail := original.With("id", 42)

	// 副本有 detail
	if withDetail.Details["id"] != 42 {
		t.Error("With() should add detail")
	}

	// 原始不受影響
	if original.Details != nil {
		t.Error("With() should not modify original")
	}
}

func TestAppErrorWithDetails(t *testing.T) {
	e := &AppError{Code: "E1", Message: "fail"}
	cp := e.WithDetails(map[string]any{"a": 1, "b": 2})
	if len(cp.Details) != 2 {
		t.Errorf("Details count = %d, want 2", len(cp.Details))
	}
	if e.Details != nil {
		t.Error("original should not be modified")
	}
}

func TestAppErrorWithMessage(t *testing.T) {
	e := &AppError{Code: "E1", Message: "old"}
	cp := e.WithMessage("new")
	if cp.Message != "new" {
		t.Errorf("Message = %q, want %q", cp.Message, "new")
	}
	if e.Message != "old" {
		t.Error("original should not be modified")
	}
}

func TestAppErrorIs(t *testing.T) {
	e1 := &AppError{Code: "E1"}
	e2 := &AppError{Code: "E1"}
	e3 := &AppError{Code: "E2"}

	if !errors.Is(e1, e2) {
		t.Error("same code should match")
	}
	if errors.Is(e1, e3) {
		t.Error("different code should not match")
	}
}

func TestAppErrorIsWithDetails(t *testing.T) {
	original := &AppError{Code: "E1"}
	withDetail := original.With("id", 42)

	// 相同 code 即使 details 不同也應 match
	if !errors.Is(withDetail, original) {
		t.Error("same code with different details should match")
	}
}

func TestAppErrorJSON(t *testing.T) {
	e := &AppError{Code: "E1", Message: "fail", Details: map[string]any{"id": 1}}
	j := e.JSON()

	if j["code"] != "E1" {
		t.Errorf("code = %v", j["code"])
	}
	if j["message"] != "fail" {
		t.Errorf("message = %v", j["message"])
	}
	if j["details"] == nil {
		t.Error("details should be present")
	}
}

func TestAppErrorJSONNoDetails(t *testing.T) {
	e := &AppError{Code: "E1", Message: "fail"}
	j := e.JSON()
	if _, ok := j["details"]; ok {
		t.Error("details should not be present when empty")
	}
}

func TestAppErrorMarshalJSON(t *testing.T) {
	e := &AppError{Code: "E1", HTTPStatus: 400, Message: "bad", Category: "test"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if parsed["code"] != "E1" {
		t.Errorf("code = %v", parsed["code"])
	}
}

// --- Clone immutability ---

func TestCloneImmutability(t *testing.T) {
	e := &AppError{Code: "E1", Message: "x", Details: map[string]any{"a": 1}}
	cp := e.With("b", 2)

	// 修改副本不影響原始
	cp.Details["c"] = 3

	if _, ok := e.Details["c"]; ok {
		t.Error("modifying clone should not affect original")
	}
	if _, ok := e.Details["b"]; ok {
		t.Error("modifying clone should not affect original")
	}
}

// --- 預定義錯誤 ---

func TestPredefinedErrors(t *testing.T) {
	// 確認預定義錯誤可正常使用
	tests := []struct {
		err    *AppError
		code   string
		status int
	}{
		{ErrNotFound, "E0001", 404},
		{ErrBadRequest, "E0002", 400},
		{ErrInternalError, "E0003", 500},
		{ErrUnauthorized, "E2001", 401},
		{ErrForbidden, "E2002", 403},
	}

	for _, tt := range tests {
		if tt.err.Code != tt.code {
			t.Errorf("%s: Code = %q, want %q", tt.code, tt.err.Code, tt.code)
		}
		if tt.err.HTTPStatus != tt.status {
			t.Errorf("%s: HTTPStatus = %d, want %d", tt.code, tt.err.HTTPStatus, tt.status)
		}
	}
}
