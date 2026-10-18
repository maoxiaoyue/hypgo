package context

import (
	stdcontext "context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

	if userID, _ := got.Get("user_id"); userID != 42 {
		t.Errorf("expected user_id=42, got %v", userID)
	}
}

// TestResponseWriterReadFrom Bug2 修復驗證：responseWriter 實作 io.ReaderFrom
func TestResponseWriterReadFrom(t *testing.T) {
	w := httptest.NewRecorder()
	rw := newResponseWriter(w)

	// 驗證 io.ReaderFrom 介面
	rf, ok := rw.(io.ReaderFrom)
	if !ok {
		t.Fatal("responseWriter should implement io.ReaderFrom")
	}

	// 透過 ReadFrom 寫入資料
	src := strings.NewReader("hello from ReadFrom")
	n, err := rf.ReadFrom(src)
	if err != nil {
		t.Fatalf("ReadFrom returned error: %v", err)
	}
	if n != 19 {
		t.Errorf("expected 19 bytes written, got %d", n)
	}

	// 驗證 body 正確寫入
	if w.Body.String() != "hello from ReadFrom" {
		t.Errorf("expected body 'hello from ReadFrom', got %q", w.Body.String())
	}

	// 驗證 size 正確追蹤
	if rw.Size() != 19 {
		t.Errorf("expected size 19, got %d", rw.Size())
	}

	// 驗證 written 狀態
	if !rw.Written() {
		t.Error("expected Written() to be true after ReadFrom")
	}
}

// TestResponseWriterReadFromWithStatus Bug2 修復驗證：ReadFrom 前設定狀態碼
func TestResponseWriterReadFromWithStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rw := newResponseWriter(w)

	rw.WriteHeader(http.StatusPartialContent)

	rf := rw.(io.ReaderFrom)
	src := strings.NewReader("partial content")
	rf.ReadFrom(src)

	if w.Code != http.StatusPartialContent {
		t.Errorf("expected status 206, got %d", w.Code)
	}
}

// TestResponseWriter404Status Bug4 修復驗證：404 狀態碼必須正確寫入
func TestResponseWriter404Status(t *testing.T) {
	w := httptest.NewRecorder()
	rw := newResponseWriter(w)

	rw.WriteHeader(http.StatusNotFound)
	rw.WriteHeaderNow()

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
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

// TestIsEarlyData 歷史 bug：只在 HTTP/3 上檢查 Early-Data 標頭——但這個標頭只會
// 由代理加上，後端收到的是 HTTP/1.1 或 HTTP/2，於是恆為 false；直連 HTTP/3 的
// 0-RTT 又沒有這個標頭（quic-go 不加），同樣恆為 false。0-RTT 防重放兩頭落空。
func TestIsEarlyData(t *testing.T) {
	open := make(chan struct{})
	closed := make(chan struct{})
	close(closed)

	cases := []struct {
		name   string
		header string
		done   chan struct{} // nil = server 未注入
		want   bool
	}{
		{"proxy header over HTTP/1.1", "1", nil, true},
		{"no signal", "", nil, false},
		{"HTTP/3 handshake pending", "", open, true},
		{"HTTP/3 handshake complete", "", closed, false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("POST", "/pay", nil)
		if tc.header != "" {
			req.Header.Set("Early-Data", tc.header)
		}
		if tc.done != nil {
			req = req.WithContext(WithHandshakeComplete(req.Context(), tc.done))
		}
		c := AcquireContext(httptest.NewRecorder(), req)
		if got := c.IsEarlyData(); got != tc.want {
			t.Errorf("%s: IsEarlyData() = %v, want %v", tc.name, got, tc.want)
		}
		ReleaseContext(c)
	}
}

// TestWriteHeaderAfterWriteIsIgnored header 已送出後改狀態碼應無效：
// Status() 必須與客戶端實際收到的一致（先前會改 status 並往 stdout 印 debug）。
func TestWriteHeaderAfterWriteIsIgnored(t *testing.T) {
	w := httptest.NewRecorder()
	rw := newResponseWriter(w)

	rw.Write([]byte("body"))
	rw.WriteHeader(http.StatusInternalServerError)

	if rw.Status() != http.StatusOK || w.Code != http.StatusOK {
		t.Errorf("Status()=%d recorder=%d, want both 200", rw.Status(), w.Code)
	}
}
