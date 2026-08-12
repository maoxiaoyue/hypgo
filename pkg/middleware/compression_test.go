package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// TestCompressionActuallyCompresses 回歸測試：Compression 中間件必須讓 body
// 真的被 gzip 壓縮，且請求結束時不得 panic。
//
// 歷史 bug（兩個）：
//  1. 中間件只替換 c.Response、沒換 c.Writer，而框架所有輸出方法都走
//     c.Writer —— body 以明文直送卻帶著 Content-Encoding: gzip，
//     客戶端解碼失敗（實測 body = 明文 + 空 gzip 串流）。
//  2. Context 歸還池時對 c.Response 做無檢查型別斷言，遇到被替換過的
//     包裝器直接 panic（interface conversion: *middleware.gzipWriter）。
func TestCompressionActuallyCompresses(t *testing.T) {
	const payload = "hello world hello world hello world"

	r := router.New()
	r.Use(Compression(CompressionConfig{Level: 5, MinLength: 1}))
	r.GET("/x", func(c *hypcontext.Context) { c.String(200, payload) })

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("request panicked (release with wrapped writer?): %v", rec)
		}
	}()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	// body 必須是合法 gzip，且解壓後等於原文
	zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("body is not valid gzip (sent as plaintext?): %v", err)
	}
	defer zr.Close()

	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("failed to decompress body: %v", err)
	}
	if string(got) != payload {
		t.Errorf("decompressed body = %q, want %q", got, payload)
	}
}
