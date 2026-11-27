package context_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type bindReq struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Admin bool   `json:"admin"`
}

func serve(r *router.Router, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func bindRouter() *router.Router {
	r := router.New()
	r.POST("/u", func(c *hypcontext.Context) {
		var v bindReq
		if !c.BindInput(&v) {
			return
		}
		c.JSON(200, v)
	})
	return r
}

func postBody(r *router.Router, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/u", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return serve(r, req)
}

// 認得的 JSON 型別都要綁到值；有 body 而型別認不得回 415。
// 歷史 bug：Application/JSON、application/vnd.api+json、text/plain 都被當成表單，
// 什麼都沒綁到，handler 拿著全零值回 200
func TestBindInputContentTypes(t *testing.T) {
	r := bindRouter()
	body := `{"name":"amy","age":30}`
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON",
		"application/vnd.api+json", "application/merge-patch+json"} {
		w := postBody(r, ct, body)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"amy"`) {
			t.Errorf("%q: %d %s", ct, w.Code, w.Body.String())
		}
	}
	for _, ct := range []string{"text/plain", "", "application/octet-stream"} {
		w := postBody(r, ct, body)
		if w.Code != http.StatusUnsupportedMediaType || !strings.Contains(w.Body.String(), `"E0005"`) {
			t.Errorf("%q: %d %s, want 415 E0005", ct, w.Code, w.Body.String())
		}
	}
	// 沒有 body 也沒有型別：沒有東西可綁，不是 415
	if w := serve(r, httptest.NewRequest("POST", "/u", nil)); w.Code != 200 {
		t.Errorf("empty POST: %d %s", w.Code, w.Body.String())
	}
	// 表單照舊
	if w := postBody(r, "application/x-www-form-urlencoded", "name=amy&age=30"); !strings.Contains(w.Body.String(), `"age":30`) {
		t.Errorf("form: %d %s", w.Code, w.Body.String())
	}
	// YAML 的兩種型別名
	for _, ct := range []string{"application/x-yaml", "application/yaml"} {
		if w := postBody(r, ct, "name: amy\nage: 30\n"); !strings.Contains(w.Body.String(), `"age":30`) {
			t.Errorf("%q: %d %s", ct, w.Code, w.Body.String())
		}
	}
}

// JSON 值之後不能有其他內容；錯誤說明不帶 Go 型別名。
// 歷史 bug：{"name":"a"}{"admin":true} 與尾端雜訊都回 200；空 body 的說明只有 "EOF"，
// 型別錯誤輸出 "Go struct field bindReq.age of type int"
func TestBindInputJSONStrictness(t *testing.T) {
	r := bindRouter()
	for body, want := range map[string]string{
		`{"name":"a"}{"admin":true}`: "unexpected data after the JSON value",
		`{"name":"a"} trailing`:      "unexpected data after the JSON value",
		``:                           "request body is empty",
		`{"name":"a","age":"x"}`:     `field \"age\" must be a number`,
		`[]`:                         "expected an object",
		`{"name":`:                   "truncated",
	} {
		w := postBody(r, "application/json", body)
		if w.Code != 400 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%q: %d %s, want 400 containing %q", body, w.Code, w.Body.String(), want)
		}
		if strings.Contains(w.Body.String(), "bindReq") || strings.Contains(w.Body.String(), "Go struct") {
			t.Errorf("%q: response leaks Go type names: %s", body, w.Body.String())
		}
	}
	if w := postBody(r, "application/json", `{"name":"a","extra":1}  `+"\n"); w.Code != 200 {
		t.Errorf("unknown field / trailing whitespace should be accepted: %d %s", w.Code, w.Body.String())
	}
}

// BindInput 可以綁到 slice 與 map，元素逐一驗證。
// 歷史 bug：validator.Struct 不收非 struct，合法的 JSON 陣列一律回 422
func TestBindInputSliceAndMap(t *testing.T) {
	type item struct {
		Name string `json:"name" validate:"required"`
	}
	r := router.New()
	r.POST("/list", func(c *hypcontext.Context) {
		var v []item
		if c.BindInput(&v) {
			c.JSON(200, len(v))
		}
	})
	r.POST("/map", func(c *hypcontext.Context) {
		var v map[string]int
		if c.BindInput(&v) {
			c.JSON(200, len(v))
		}
	})
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		return serve(r, req)
	}
	if w := post("/list", `[{"name":"a"},{"name":"b"}]`); w.Code != 200 || w.Body.String() != "2" {
		t.Errorf("slice: %d %s", w.Code, w.Body.String())
	}
	if w := post("/map", `{"a":1}`); w.Code != 200 {
		t.Errorf("map: %d %s", w.Code, w.Body.String())
	}
	if w := post("/list", `[{"name":"a"},{}]`); w.Code != 422 || !strings.Contains(w.Body.String(), `"[1].name"`) {
		t.Errorf("invalid element: %d %s, want 422 naming [1].name", w.Code, w.Body.String())
	}
}

// 驗證錯誤的 key 是完整路徑。
// 歷史 bug：只取最後一段，billing.city / shipping.city / items[1].city 互相覆蓋成一筆 city
func TestValidationDetailsUseFullPath(t *testing.T) {
	type addr struct {
		City string `json:"city" validate:"required"`
	}
	type order struct {
		Note     string `json:"note" validate:"required"`
		Billing  addr   `json:"billing"`
		Shipping addr   `json:"shipping"`
		Items    []addr `json:"items" validate:"dive"`
	}
	r := router.New()
	r.POST("/o", func(c *hypcontext.Context) {
		var v order
		if c.BindInput(&v) {
			c.JSON(200, v)
		}
	})
	req := httptest.NewRequest("POST", "/o", strings.NewReader(`{"items":[{"city":"x"},{}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := serve(r, req)
	for _, key := range []string{`"note"`, `"billing.city"`, `"shipping.city"`, `"items[1].city"`} {
		if !strings.Contains(w.Body.String(), key) {
			t.Errorf("details missing %s: %s", key, w.Body.String())
		}
	}
}

// query 綁定：沒有 tag 的欄位不分大小寫、time.Time 與 TextUnmarshaler、沒有 body 的 DELETE。
// 歷史 bug：struct{Page int} 配 ?page=2 得到 0；?since=… 回 400；DELETE ?force=true 綁出 false
func TestQueryBindingFixes(t *testing.T) {
	type q struct {
		Page  int
		Since time.Time `form:"since"`
		Day   time.Time `form:"day"`
		Level level     `form:"level"`
		Force bool      `form:"force"`
	}
	r := router.New()
	h := func(c *hypcontext.Context) {
		var v q
		if c.BindInput(&v) {
			c.String(200, "%d|%s|%s|%d|%v", v.Page, v.Since.Format(time.RFC3339), v.Day.Format("2006-01-02"), v.Level, v.Force)
		}
	}
	r.GET("/q", h)
	r.DELETE("/q", h)
	w := serve(r, httptest.NewRequest("GET", "/q?page=2&since=2026-01-02T03:04:05Z&day=2026-03-04&level=high", nil))
	if w.Body.String() != "2|2026-01-02T03:04:05Z|2026-03-04|3|false" {
		t.Errorf("GET: %d %s", w.Code, w.Body.String())
	}
	if w := serve(r, httptest.NewRequest("GET", "/q?since=yesterday", nil)); w.Code != 400 {
		t.Errorf("bad time: %d %s", w.Code, w.Body.String())
	}
	if w := serve(r, httptest.NewRequest("DELETE", "/q?force=true", nil)); !strings.HasSuffix(w.Body.String(), "|true") {
		t.Errorf("DELETE: %d %s", w.Code, w.Body.String())
	}
}

type level int

func (l *level) UnmarshalText(b []byte) error {
	switch string(b) {
	case "low":
		*l = 1
	case "high":
		*l = 3
	default:
		return fmt.Errorf("unknown level %q", b)
	}
	return nil
}

// ProtoBuf 與 MsgPack 綁定器可以用。歷史 bug：兩者一律回 "not implemented"
func TestProtoBufAndMsgPackBinding(t *testing.T) {
	r := router.New()
	r.POST("/pb", func(c *hypcontext.Context) {
		var v wrapperspb.StringValue
		if err := c.ShouldBind(&v); err != nil {
			c.String(400, err.Error())
			return
		}
		c.String(200, v.GetValue())
	})
	r.POST("/mp", func(c *hypcontext.Context) {
		var v bindReq
		if c.BindInput(&v) {
			c.String(200, "%s/%d", v.Name, v.Age)
		}
	})
	pb, _ := proto.Marshal(wrapperspb.String("hello"))
	req := httptest.NewRequest("POST", "/pb", bytes.NewReader(pb))
	req.Header.Set("Content-Type", "application/x-protobuf")
	if w := serve(r, req); w.Body.String() != "hello" {
		t.Errorf("protobuf: %d %s", w.Code, w.Body.String())
	}
	mp, _ := msgpack.Marshal(map[string]interface{}{"name": "amy", "age": 30})
	req = httptest.NewRequest("POST", "/mp", bytes.NewReader(mp))
	req.Header.Set("Content-Type", "application/msgpack")
	if w := serve(r, req); w.Body.String() != "amy/30" {
		t.Errorf("msgpack: %d %s", w.Code, w.Body.String())
	}
}

// body 上限：超過回 413，SetBodyLimit 可以放寬。
// 歷史 bug：沒有任何上限，300MB 的 JSON 字串整包讀進記憶體
func TestBodyLimit(t *testing.T) {
	r := router.New()
	r.POST("/u", func(c *hypcontext.Context) {
		var v bindReq
		if c.BindInput(&v) {
			c.JSON(200, len(v.Name))
		}
	})
	r.POST("/big", func(c *hypcontext.Context) {
		c.SetBodyLimit(1 << 20)
		var v bindReq
		if c.BindInput(&v) {
			c.JSON(200, len(v.Name))
		}
	})
	send := func(path string, size int) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"name":"`+strings.Repeat("a", size)+`"}`))
		req.Header.Set("Content-Type", "application/json")
		hypcontext.LimitRequestBody(req, 1024) // server 以 server.max_body_size 做的事
		return serve(r, req)
	}
	if w := send("/u", 100); w.Code != 200 {
		t.Errorf("small body: %d %s", w.Code, w.Body.String())
	}
	if w := send("/u", 5000); w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), `"E0006"`) {
		t.Errorf("large body: %d %s, want 413 E0006", w.Code, w.Body.String())
	}
	if w := send("/big", 5000); w.Code != 200 {
		t.Errorf("raised limit: %d %s", w.Code, w.Body.String())
	}

	// 剛好等於上限可以讀完；多一個位元組就失敗
	req := httptest.NewRequest("POST", "/", strings.NewReader("12345"))
	hypcontext.LimitRequestBody(req, 5)
	if b, err := io.ReadAll(req.Body); err != nil || string(b) != "12345" {
		t.Errorf("exactly at the limit: %q, %v", b, err)
	}
	// 長度未知（chunked）：讀到上限才知道超過
	req = httptest.NewRequest("POST", "/", strings.NewReader("123456"))
	req.ContentLength = -1
	hypcontext.LimitRequestBody(req, 5)
	var tooBig *http.MaxBytesError
	if b, err := io.ReadAll(req.Body); !errors.As(err, &tooBig) || len(b) != 5 {
		t.Errorf("one byte over: %q, %v", b, err)
	}
	// Content-Length 已經超過上限：一個位元組都不讀。歷史 bug：仍先讀 limit+1 位元組，
	// 帶 Expect: 100-continue 的客戶端也被要求上傳
	src := &countingReader{r: strings.NewReader("123456")}
	req = httptest.NewRequest("POST", "/", src)
	req.ContentLength = 6
	hypcontext.LimitRequestBody(req, 5)
	if b, err := io.ReadAll(req.Body); !errors.As(err, &tooBig) || len(b) != 0 || src.n != 0 {
		t.Errorf("declared length over the limit: read %q (%d from the source), %v", b, src.n, err)
	}
	// 放寬上限後照常讀
	hypcontext.LimitRequestBody(req, 10)
	if b, err := io.ReadAll(req.Body); err != nil || string(b) != "123456" {
		t.Errorf("after raising the limit: %q, %v", b, err)
	}
}

// 多行 X-Forwarded-For 要全部算進去。
// 歷史 bug：只讀第一行，客戶端自帶一行就能偽造 ClientIP（HAProxy 另加一行而不是接在後面）
func TestClientIPUsesAllForwardedForLines(t *testing.T) {
	hypcontext.SetTrustedProxies("10.0.0.1")
	defer hypcontext.SetTrustedProxies()
	r := router.New()
	r.GET("/ip", func(c *hypcontext.Context) { c.String(200, c.ClientIP()) })
	req := httptest.NewRequest("GET", "/ip", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Add("X-Forwarded-For", "6.6.6.6")
	req.Header.Add("X-Forwarded-For", "203.0.113.9")
	if got := serve(r, req).Body.String(); got != "203.0.113.9" {
		t.Errorf("ClientIP = %s, want the address appended by the proxy", got)
	}
}

// JSONP 的 callback 只收識別字。歷史 bug：?callback=alert(document.cookie)// 原樣成為可執行的腳本
func TestJSONPCallbackIsValidated(t *testing.T) {
	r := router.New()
	r.GET("/p", func(c *hypcontext.Context) { c.JSONP(200, map[string]int{"a": 1}) })
	get := func(cb string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/p", nil)
		q := req.URL.Query()
		q.Set("callback", cb)
		req.URL.RawQuery = q.Encode()
		return serve(r, req)
	}
	if w := get("app.cb_1$"); w.Code != 200 || w.Body.String() != `app.cb_1$({"a":1});` {
		t.Errorf("valid callback: %d %s", w.Code, w.Body.String())
	}
	for _, cb := range []string{"alert(document.cookie)//", "a;b", "a b", "x`y"} {
		if w := get(cb); w.Code != 400 || strings.Contains(w.Body.String(), cb) {
			t.Errorf("callback %q: %d %s, want 400 without echoing it", cb, w.Code, w.Body.String())
		}
	}
}

// SSE：事件名稱不能帶換行、序列化失敗時不送出半個事件、不送 Connection 標頭。
// 歷史 bug：名稱的換行可以多塞一行 data:；event 行送出後才 panic；HTTP/3 禁止的 Connection: keep-alive
func TestSSEventIsSafe(t *testing.T) {
	r := router.New()
	r.GET("/sse", func(c *hypcontext.Context) {
		c.SSEvent("x\ndata: injected", map[string]int{"n": 1})
		defer func() { recover() }()
		c.SSEvent("bad", make(chan int))
	})
	w := serve(r, httptest.NewRequest("GET", "/sse", nil))
	if got, want := w.Body.String(), "event: xdata: injected\ndata: {\"n\":1}\n\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if w.Header().Get("Connection") != "" {
		t.Errorf("Connection header = %q", w.Header().Get("Connection"))
	}
}

// 附件檔名要正確編碼。歷史 bug：引號截斷標頭、中文檔名沒有 filename*
func TestFileAttachmentFilename(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(p, []byte("x"), 0o644)
	r := router.New()
	r.GET("/dl", func(c *hypcontext.Context) { c.FileAttachment(p, c.Query("n")) })
	for name, want := range map[string]string{
		"report.pdf":  `attachment; filename=report.pdf`,
		"報告.pdf":      `attachment; filename*=utf-8''%E5%A0%B1%E5%91%8A.pdf`,
		`a";x="b.txt`: `attachment; filename="a\";x=\"b.txt"`,
	} {
		req := httptest.NewRequest("GET", "/dl", nil)
		q := req.URL.Query()
		q.Set("n", name)
		req.URL.RawQuery = q.Encode()
		if got := serve(r, req).Header().Get("Content-Disposition"); got != want {
			t.Errorf("%q: Content-Disposition = %s, want %s", name, got, want)
		}
	}
}

// 其餘小修正
func TestMiscContextFixes(t *testing.T) {
	r := router.New()
	r.GET("/rid", func(c *hypcontext.Context) {
		a := c.GetRequestID()
		time.Sleep(time.Millisecond)
		c.String(200, "%v", a != "" && a == c.GetRequestID())
	})
	r.GET("/rid-set", func(c *hypcontext.Context) {
		c.SetRequestID("abc")
		c.String(200, c.GetRequestID())
	})
	r.GET("/offset", func(c *hypcontext.Context) { c.String(200, "%v", c.GetOffset() >= 0) })
	r.GET("/name", func(c *hypcontext.Context) {
		c.Abort()
		c.String(200, "[%s]", c.HandlerName())
	})
	r.GET("/neg", func(c *hypcontext.Context) { c.String(200, c.NegotiateFormat("application/json", "text/html")) })
	r.GET("/err", func(c *hypcontext.Context) {
		c.Error(errors.New("pq: password authentication failed"))
		c.AddPublicError(errors.New("email already taken"))
		c.ErrorJSON(409)
	})
	r.GET("/err-private", func(c *hypcontext.Context) {
		c.Error(errors.New("pq: password authentication failed"))
		c.ErrorJSON(500)
	})
	r.GET("/copy", func(c *hypcontext.Context) {
		cp := c.Copy()
		cp.JSON(200, 1) // 丟棄，不 panic
		c.String(200, "ok")
	})
	r.GET("/cors", func(c *hypcontext.Context) {
		c.SetCORS("*")
		c.SetSecurityHeaders()
		c.Status(204)
	})
	r.POST("/metrics", func(c *hypcontext.Context) {
		io.Copy(io.Discard, c.Request.Body)
		c.String(200, "hello")
		m := c.GetMetrics()
		c.Header("X-In", fmt.Sprint(m.BytesIn))
		c.Set("out", m.BytesOut)
		if m.BytesIn != 3 || m.BytesOut != 5 {
			panic(fmt.Sprintf("metrics in=%d out=%d", m.BytesIn, m.BytesOut))
		}
	})
	get := func(path string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		return serve(r, req)
	}
	for path, want := range map[string]string{
		"/rid":     "true",
		"/rid-set": "abc",
		"/offset?page=9223372036854775807&page_size=100": "true",
		"/name": "[]",
		"/copy": "ok",
	} {
		if got := get(path).Body.String(); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	for accept, want := range map[string]string{
		"text/html;q=0.1, application/json;q=0.9": "application/json",
		"text/*":               "text/html",
		"application/json;q=0": "",
		"image/png":            "",
	} {
		if got := get("/neg", "Accept", accept).Body.String(); got != want {
			t.Errorf("Accept %q -> %q, want %q", accept, got, want)
		}
	}
	if got := get("/err").Body.String(); strings.Contains(got, "pq:") || !strings.Contains(got, "email already taken") {
		t.Errorf("ErrorJSON = %s, want only the public error", got)
	}
	if got := get("/err-private").Body.String(); got != `{"error":"Internal Server Error"}` {
		t.Errorf("ErrorJSON without public errors = %s", got)
	}
	w := get("/cors")
	if w.Header().Get("Access-Control-Allow-Credentials") != "" || w.Header().Get("X-XSS-Protection") != "0" {
		t.Errorf("headers: credentials=%q xss=%q", w.Header().Get("Access-Control-Allow-Credentials"), w.Header().Get("X-XSS-Protection"))
	}
	req := httptest.NewRequest("POST", "/metrics", strings.NewReader("abc"))
	if w := serve(r, req); w.Code != 200 {
		t.Errorf("metrics: %d %s", w.Code, w.Body.String())
	}
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}
