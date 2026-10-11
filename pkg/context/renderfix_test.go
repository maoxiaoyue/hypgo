package context

import (
	"encoding/json"
	"encoding/xml"
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

// TestAsciiJSONEscapesNonASCII 回歸測試：AsciiJSON 輸出必須是純 ASCII，且能無損解回原字串。
// 歷史 bug：asciiJSONRender 只是 SetEscapeHTML(false)，與 PureJSON 完全相同，中文原樣輸出。
func TestAsciiJSONEscapesNonASCII(t *testing.T) {
	w := httptest.NewRecorder()
	c := AcquireContext(w, httptest.NewRequest("GET", "/x", nil))
	defer c.Release()

	const want = "中文<b>😀"
	c.AsciiJSON(200, map[string]string{"s": want})

	body := w.Body.Bytes()
	for _, b := range body {
		if b >= 0x80 {
			t.Fatalf("AsciiJSON body %q contains non-ASCII byte", body)
		}
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil || got["s"] != want {
		t.Errorf("AsciiJSON round-trip = %q, %v; want %q", got["s"], err, want)
	}
}

// TestXMLRenderWritesNothingOnError 回歸測試：編碼失敗時不得已送出半截 body。
// 歷史 bug：xml.NewEncoder(w) 直接串流，大 payload 的前段已隨 bufio 分塊送出、header 已是 200，
// 之後 Render 的 panic 由 Recovery 轉 500 也送不出去。
func TestXMLRenderWritesNothingOnError(t *testing.T) {
	w := httptest.NewRecorder()
	data := struct {
		XMLName xml.Name `xml:"r"`
		Big     string
		Bad     chan int
	}{Big: strings.Repeat("x", 64<<10), Bad: make(chan int)}

	if err := (xmlRender{Data: data}).Render(w); err == nil {
		t.Fatal("expected encode error for chan field")
	}
	if w.Body.Len() != 0 {
		t.Errorf("body has %d bytes, want nothing written before the error", w.Body.Len())
	}
}
