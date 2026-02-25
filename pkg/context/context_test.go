package context

import (
	stdcontext "context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewContextAndFromContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	c := New(w, req)

	// 將 HypGo Context 嵌入標準 context
	stdCtx := NewContext(stdcontext.Background(), c)

	// 從標準 context 中提取
	got, ok := FromContext(stdCtx)
	if !ok {
		t.Fatal("expected to find HypGo Context in standard context")
	}
	if got != c {
		t.Error("extracted context does not match original")
	}
}

func TestFromContextDirect(t *testing.T) {
	// *Context 本身實現 context.Context 介面
	// FromContext 應能直接從 *Context 提取
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	c := New(w, req)

	// 直接將 *Context 作為 context.Context 傳入
	var ctx stdcontext.Context = c
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("FromContext should work with *Context passed directly as context.Context")
	}
	if got != c {
		t.Error("extracted context does not match original")
	}
}

func TestFromContextNotFound(t *testing.T) {
	// 普通的 context.Background() 不含 HypGo Context
	ctx := stdcontext.Background()
	_, ok := FromContext(ctx)
	if ok {
		t.Error("FromContext should return false for context without HypGo Context")
	}
}

func TestStdContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	c := New(w, req)

	stdCtx := c.StdContext()

	// 應能從 StdContext 返回的 context 中提取回 HypGo Context
	got, ok := FromContext(stdCtx)
	if !ok {
		t.Fatal("StdContext should embed the HypGo Context")
	}
	if got != c {
		t.Error("extracted context does not match original")
	}
}

func TestStdContextNilRequest(t *testing.T) {
	// 沒有 Request 的 Context（例如測試場景）
	c := &Context{
		Keys: make(map[string]interface{}),
	}

	stdCtx := c.StdContext()

	// 應仍能提取
	got, ok := FromContext(stdCtx)
	if !ok {
		t.Fatal("StdContext should work even without Request")
	}
	if got != c {
		t.Error("extracted context does not match original")
	}
}

func TestMustFromContextPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustFromContext should panic when HypGo Context not found")
		}
	}()

	ctx := stdcontext.Background()
	MustFromContext(ctx) // 應 panic
}

func TestMustFromContextSuccess(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	c := New(w, req)

	stdCtx := NewContext(stdcontext.Background(), c)

	// 不應 panic
	got := MustFromContext(stdCtx)
	if got != c {
		t.Error("MustFromContext returned wrong context")
	}
}

func TestStdContextPreservesValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	c := New(w, req)

	// 在 HypGo Context 中設定值
	c.Set("user_id", 42)

	stdCtx := c.StdContext()

	// 從標準 context 提取 HypGo Context，然後讀取值
	got, ok := FromContext(stdCtx)
	if !ok {
		t.Fatal("expected to find HypGo Context")
	}

	userID := got.GetInt("user_id")
	if userID != 42 {
		t.Errorf("expected user_id=42, got %d", userID)
	}
}

func TestValueWithHypContextKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	c := New(w, req)

	// Value 應正確回應 hypContextKey
	val := c.Value(hypContextKey{})
	if val != c {
		t.Error("Value(hypContextKey{}) should return the Context itself")
	}
}
