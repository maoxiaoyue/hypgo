package context_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// 長度未知而實際沒有 body 的請求不算「有 body」。歷史 bug：HTTP/3 沒帶 Content-Length 的請求
// （ContentLength = -1、一律掛著 stream body）與 chunked 空 body 的 POST / PUT / DELETE 回 415
func TestUnknownLengthEmptyBodyIsNoBody(t *testing.T) {
	type q struct {
		Force bool `form:"force" json:"force"`
	}
	r := router.New()
	h := func(c *hypcontext.Context) {
		var v q
		if !c.BindInput(&v) {
			return
		}
		c.JSON(200, v)
	}
	r.DELETE("/items", h)
	r.POST("/items", h)

	for _, method := range []string{"DELETE", "POST", "PUT"} {
		req := httptest.NewRequest(method, "/items?force=true", io.NopCloser(strings.NewReader("")))
		req.ContentLength = -1
		if method == "PUT" {
			continue // 沒註冊，只檢查綁定器的選擇
		}
		w := serve(r, req)
		if w.Code != 200 {
			t.Errorf("%s with an empty body of unknown length: %d %s", method, w.Code, w.Body.String())
		}
		if method == "DELETE" && !strings.Contains(w.Body.String(), `"force":true`) {
			t.Errorf("DELETE did not bind the query: %s", w.Body.String())
		}
	}

	// 真的有 body 而型別認不得：仍然 415，而且 body 原封不動
	var rest string
	r.POST("/raw", func(c *hypcontext.Context) {
		var v q
		if !c.BindInput(&v) {
			b, _ := io.ReadAll(c.Request.Body)
			rest = string(b)
		}
	})
	req := httptest.NewRequest("POST", "/raw", io.NopCloser(strings.NewReader(`{"force":true}`)))
	req.ContentLength = -1
	if w := serve(r, req); w.Code != 415 || rest != `{"force":true}` {
		t.Errorf("body of unknown type: %d, body left = %q; want 415 with the body intact", w.Code, rest)
	}
}

// 代理附加 ip:port 時 ClientIP 取那個位址；看不懂的項目不能略過。
// 歷史 bug：XFF: 6.6.6.6 + 203.0.113.7:51234 → 略過帶埠號的那一項，回傳客戶端自填的 6.6.6.6
func TestClientIPWithPortsInForwardedFor(t *testing.T) {
	hypcontext.SetTrustedProxies("10.0.0.0/8")
	defer hypcontext.SetTrustedProxies()

	ip := func(xff ...string) string {
		r := router.New()
		var got string
		r.GET("/", func(c *hypcontext.Context) { got = c.ClientIP() })
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.5:4000"
		for _, v := range xff {
			req.Header.Add("X-Forwarded-For", v)
		}
		serve(r, req)
		return got
	}
	for want, xff := range map[string][]string{
		"203.0.113.7":  {"6.6.6.6", "203.0.113.7:51234"},
		"2001:db8::7":  {"6.6.6.6, [2001:db8::7]:443"},
		"2001:db8::8":  {"6.6.6.6, [2001:db8::8]"},
		"10.0.0.5":     {"6.6.6.6, garbage"}, // 看不懂：停在這裡，回實際的連線來源
		"198.51.100.2": {"6.6.6.6, 198.51.100.2, 10.0.0.9:80"},
	} {
		if got := ip(xff...); got != want {
			t.Errorf("XFF %v: ClientIP = %q, want %q", xff, got, want)
		}
	}
}
