package errors

import (
	"encoding/json"
	stderrors "errors"
	"net/http/httptest"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

func expectPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected a panic", name)
		}
	}()
	f()
}

// 直接序列化 AppError 與 JSON() 的輸出相同。
// 歷史 bug：c.JSON(404, appErr) 依 struct tag 多送出 http_status 與 category
func TestMarshalJSONMatchesJSON(t *testing.T) {
	e := ErrNotFound.With("id", 7).Wrap(stderrors.New("sql: no rows"))
	direct, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	viaJSON, _ := json.Marshal(e.JSON())
	if string(direct) != string(viaJSON) || strings.Contains(string(direct), "http_status") ||
		strings.Contains(string(direct), "category") || strings.Contains(string(direct), "no rows") {
		t.Errorf("direct = %s\nJSON() = %s", direct, viaJSON)
	}
}

// 細節值一定寫得出來：error 取訊息，無法序列化的值改成字串。
// 歷史 bug：With("err", err) 輸出 "err":{}；With("ch", make(chan int)) 讓 c.JSON panic，客戶端拿到空的 500
func TestDetailsAreAlwaysSerializable(t *testing.T) {
	e := ErrInternalError.With("err", stderrors.New("db down")).With("ch", make(chan int)).
		WithDetails(map[string]any{"fn": func() {}, "n": 3, "cause": stderrors.New("inner")})
	out, err := json.Marshal(e.JSON())
	if err != nil {
		t.Fatalf("details not serializable: %v", err)
	}
	for _, want := range []string{`"err":"db down"`, `"cause":"inner"`, `"n":3`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("%s missing from %s", want, out)
		}
	}
	if e.Details["ch"] != "<chan int>" || e.Details["fn"] != "<func()>" {
		t.Errorf("chan / func details = %#v / %#v, want their type names", e.Details["ch"], e.Details["fn"])
	}
}

// Wrap 的底層錯誤要交給 c.Error（server 據此記錄），而且不出現在回應裡。
// 歷史 bug：底層錯誤不在回應裡、也沒有任何地方記錄，500 的原因就此消失
func TestWrappedCauseIsReported(t *testing.T) {
	cause := stderrors.New("pq: password authentication failed")
	for name, respond := range map[string]func(*hypcontext.Context, *AppError){
		"AbortWithAppError": AbortWithAppError, "RespondError": RespondError,
	} {
		w := httptest.NewRecorder()
		c := hypcontext.New(w, httptest.NewRequest("GET", "/x", nil))
		respond(c, ErrInternalError.Wrap(cause))
		if len(c.Errors) != 1 || !stderrors.Is(c.Errors[0].Err, cause) || !stderrors.Is(c.Errors[0].Err, ErrInternalError) {
			t.Errorf("%s: c.Errors = %v", name, c.Errors)
		}
		if w.Code != 500 || strings.Contains(w.Body.String(), "password") {
			t.Errorf("%s: response = %d %s", name, w.Code, w.Body.String())
		}

		// 沒有 Wrap 的錯誤不記（404 這類預期中的錯誤不該洗版）
		c = hypcontext.New(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
		respond(c, ErrNotFound.With("id", 1))
		if len(c.Errors) != 0 {
			t.Errorf("%s: an error without a cause was reported: %v", name, c.Errors)
		}
	}
}

// Catalog 的查詢回傳副本並依錯誤碼排序；Is 遇到 nil 不 panic；Define 擋下空的碼與非錯誤的狀態碼。
// 歷史 bug：改 Get() 的結果會改掉 ErrNotFound；ByCategory 的順序每次不同；
// errors.Is(err, (*AppError)(nil)) panic；Define("", 200, …) 成功
func TestCatalogCopiesOrderAndDefineChecks(t *testing.T) {
	// 其他測試會 Reset 全域 catalog：這裡用自己的定義
	GlobalCatalog().Reset()
	defer GlobalCatalog().Reset()
	defs := []*AppError{
		Define("R2_C", 409, "c", "r2"), Define("R2_A", 404, "a", "r2"), Define("R2_D", 400, "d", "other"),
		Define("R2_B", 422, "b", "r2"), Define("R2_E", 500, "e", "r2"), Define("R2_F", 403, "f", "r2"),
	}
	got, ok := GlobalCatalog().Get("R2_A")
	if !ok || !stderrors.Is(got, defs[1]) {
		t.Fatalf("Get(R2_A) = %v, %v", got, ok)
	}
	got.Message = "changed"
	GlobalCatalog().All()[0].Message = "changed"
	GlobalCatalog().ByCategory("r2")[0].Message = "changed"
	if defs[1].Message != "a" {
		t.Errorf("a catalog query result aliases the definition: %q", defs[1].Message)
	}

	for i := 0; i < 20; i++ {
		var codes []string
		for _, e := range GlobalCatalog().ByCategory("r2") {
			codes = append(codes, e.Code)
		}
		if strings.Join(codes, ",") != "R2_A,R2_B,R2_C,R2_E,R2_F" {
			t.Fatalf("ByCategory(r2) = %v", codes)
		}
	}
	if all := GlobalCatalog().All(); len(all) != 6 || all[3].Code != "R2_D" {
		t.Errorf("All() = %v", all)
	}

	if stderrors.Is(ErrNotFound, (*AppError)(nil)) || stderrors.Is(ErrNotFound, ErrForbidden) || !stderrors.Is(ErrNotFound.With("a", 1), ErrNotFound) {
		t.Error("Is comparison is wrong")
	}

	expectPanic(t, "empty code", func() { Define("", 400, "x", "t") })
	expectPanic(t, "status 200", func() { Define("R2_OK", 200, "x", "t") })
	expectPanic(t, "status 302", func() { Define("R2_REDIRECT", 302, "x", "t") })
	if _, ok := GlobalCatalog().Get("R2_OK"); ok {
		t.Error("a rejected definition was registered")
	}
}
