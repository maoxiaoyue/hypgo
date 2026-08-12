package context

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSecureJSONHasPrefix 回歸測試：SecureJSON 必須真的加上 while(1); 前綴。
// 歷史 bug：secureJSONRender{Data: obj} 漏填 Prefix，render 的
// if r.Prefix != "" 恆假——輸出與 c.JSON 位元組相同，防 JSON 劫持形同虛設。
func TestSecureJSONHasPrefix(t *testing.T) {
	w := httptest.NewRecorder()
	c := AcquireContext(w, httptest.NewRequest("GET", "/x", nil))
	defer c.Release()

	c.SecureJSON(200, []string{"a", "b"})

	body := w.Body.String()
	if !strings.HasPrefix(body, "while(1);") {
		t.Errorf("SecureJSON body = %q, want while(1); prefix", body)
	}
	if !strings.Contains(body, `["a","b"]`) {
		t.Errorf("SecureJSON body = %q, want the payload after the prefix", body)
	}
}

// TestJSONPCarriesPayload 回歸測試：JSONP 必須輸出 callback(payload);。
// 歷史 bug：jsonpJSONRender{Data: callback} 把 callback 當成 payload 傳、
// Callback 欄位留空 —— 實際輸出 ("cb"); 而 obj 整包遺失（靜默失敗）。
func TestJSONPCarriesPayload(t *testing.T) {
	w := httptest.NewRecorder()
	c := AcquireContext(w, httptest.NewRequest("GET", "/x?callback=cb", nil))
	defer c.Release()

	c.JSONP(200, map[string]int{"n": 7})

	body := w.Body.String()
	if !strings.HasPrefix(body, "cb(") {
		t.Errorf("JSONP body = %q, want it to start with the callback name", body)
	}
	if !strings.Contains(body, `{"n":7}`) {
		t.Errorf("JSONP body = %q, want the payload inside the callback", body)
	}
}
