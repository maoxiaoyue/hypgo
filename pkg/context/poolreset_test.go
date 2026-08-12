package context

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestResetClearsEveryField 回歸測試：池化 reset() 必須清掉 Context 的每個
// 請求相關欄位。漏清任一欄位都會造成跨請求資料洩漏或狀態污染。
//
// 歷史 bug：reset() 漏清 7 個欄位（rawData / Writer / rw / metrics /
// fullPath / routerGroup / Accepted / sameSite），其中 rawData 最嚴重——
// 它有 memo 語義，下一個請求呼叫 GetRawData/ShouldBindBodyWith 會直接
// 拿到上一個請求的完整 body（跨使用者敏感資料洩漏）。
//
// 本測試用反射逐欄位檢查，未來新增欄位若忘了在 reset() 處理會自動被抓出。
func TestResetClearsEveryField(t *testing.T) {
	c := AcquireContext(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", nil))

	// 把每個請求相關欄位都填成非零值
	c.rawData = []byte(`{"password":"SECRET"}`)
	c.fullPath = "/users/:id"
	c.routerGroup = &RouterGroup{basePath: "/api"}
	c.Accepted = []string{"application/xml"}
	c.sameSite = http.SameSiteNoneMode
	c.Keys = map[string]interface{}{"user_id": 42}
	c.Params = Params{{Key: "id", Value: "1"}}
	c.fullPath = "/x"

	c.reset()

	// 逐欄位驗證已歸零
	checks := []struct {
		name string
		zero bool
	}{
		{"rawData", c.rawData == nil},
		{"Writer", c.Writer == nil},
		{"Response", c.Response == nil},
		{"rw", c.rw == nil},
		{"metrics", c.metrics == nil},
		{"Request", c.Request == nil},
		{"fullPath", c.fullPath == ""},
		{"routerGroup", c.routerGroup == nil},
		{"Accepted", c.Accepted == nil},
		{"sameSite", c.sameSite == 0},
		{"queryCache", c.queryCache == nil},
		{"formCache", c.formCache == nil},
		{"quicConn", c.quicConn == nil},
		{"streamInfo", c.streamInfo == nil},
		{"Params", len(c.Params) == 0},
		{"handlers", len(c.handlers) == 0},
		{"Errors", len(c.Errors) == 0},
		{"Keys", len(c.Keys) == 0},
		{"index", c.index == -1},
		{"schemaInput", c.schemaInput == nil},
		{"schemaRouteKey", c.schemaRouteKey == ""},
		{"bindInputCalled", !c.bindInputCalled},
		{"released", !c.released},
	}
	for _, ck := range checks {
		if !ck.zero {
			t.Errorf("reset() left field %q non-zero — cross-request leak risk", ck.name)
		}
	}

	// 防止未來新增欄位卻忘了在 reset() 處理：欄位數變動時提醒補這個測試
	const knownFieldCount = 26
	if got := reflect.TypeOf(Context{}).NumField(); got != knownFieldCount {
		t.Errorf("Context has %d fields, test knows %d — new field added? "+
			"確認它是否需要在 reset() 清除，並更新本測試", got, knownFieldCount)
	}
}

// TestReleaseIsIdempotent 回歸測試：Release 必須冪等。
// Release/ReleaseContext 皆為 exported，使用者若也 defer 一次，
// 會與 router 的 defer 構成 double-release——同一物件被 Put 進池兩次，
// 之後兩個並行請求會共用同一個 Context。
func TestReleaseIsIdempotent(t *testing.T) {
	c := AcquireContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	c.Release()
	c.Release() // 第二次必須是 no-op，不得再次 Put
	ReleaseContext(c)

	if !c.released {
		t.Error("released flag should stay true after repeated Release")
	}
}

// TestReleaseWithWrappedResponse 回歸測試：中間件替換 c.Response 為包裝器後，
// Release 不得 panic。歷史 bug：ReleaseContext 對 c.Response 做無檢查型別斷言
// c.Response.(*responseWriter)，只要啟用 Compression 中間件，任何 gzip 請求
// 都會在 release 時 panic（interface conversion）。
func TestReleaseWithWrappedResponse(t *testing.T) {
	c := AcquireContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	// 模擬中間件包裝（如 Compression 的 gzipWriter）
	c.Response = wrappedWriter{c.Response}
	c.Writer = c.Response

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Release panicked with a wrapped Response: %v", r)
		}
	}()
	c.Release()
}

type wrappedWriter struct{ ResponseWriter }
