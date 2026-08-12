package context

import (
	"bytes"
	stdcontext "context"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestStreamStopsWhenClientDisconnects 回歸測試：客戶端斷線後 Stream 必須結束。
//
// 歷史 bug：Stream 的 for 迴圈只看 step 的回傳值，不檢查請求 context。
// 多數 step 實作會忽略 Write 錯誤而永遠回 true → 客戶端關閉分頁後
// goroutine 永不退出、Context 也永不歸還池，每個斷線客戶端漏一條 goroutine。
func TestStreamStopsWhenClientDisconnects(t *testing.T) {
	req := httptest.NewRequest("GET", "/sse", nil)
	ctx, cancel := stdcontext.WithCancel(req.Context())
	req = req.WithContext(ctx)

	c := AcquireContext(httptest.NewRecorder(), req)
	defer c.Release()

	// 模擬客戶端在串流途中離線
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	done := make(chan bool, 1)
	go func() {
		// step 永遠回 true（典型的忽略寫入錯誤的實作）
		done <- c.Stream(func(w io.Writer) bool {
			time.Sleep(time.Millisecond)
			return true
		})
	}()

	select {
	case disconnected := <-done:
		if !disconnected {
			t.Error("Stream returned false, want true (client disconnected)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return after the client disconnected — goroutine leak")
	}
}

// TestFinishRemovesMultipartTempFiles 回歸測試：請求結束時必須清掉 multipart 暫存檔。
//
// 歷史 bug：ParseMultipartForm 超出記憶體上限的部分會落地成暫存檔，
// net/http 只在 HTTP/1.1 與 H2 的 finishRequest 幫忙 RemoveAll，
// quic-go 的 http3 沒有這段 —— HTTP/3 上傳會在磁碟無限堆積（DoS 面）。
func TestFinishRemovesMultipartTempFiles(t *testing.T) {
	// 造一個大到會落地的 multipart body
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte("x"), 2<<20)); err != nil { // 2MB
		t.Fatal(err)
	}
	mw.Close()

	req := httptest.NewRequest("POST", "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	c := AcquireContext(httptest.NewRecorder(), req)

	// 以極小的記憶體上限強制落地成暫存檔
	if err := c.Request.ParseMultipartForm(1024); err != nil {
		t.Fatal(err)
	}
	if c.Request.MultipartForm == nil {
		t.Fatal("expected a parsed multipart form")
	}

	before := countTempFiles(t)
	c.Finish()
	after := countTempFiles(t)

	if after > before {
		t.Errorf("Finish() left %d multipart temp file(s) behind", after-before)
	}
	c.Release()
}

// TestFinishReportsUnconsumedErrors 回歸測試：c.Error 累積的錯誤必須被回報。
//
// 歷史 bug：c.Errors 全 workspace 無任何消費者 —— AbortWithError(500, err)
// 只送出空的 500，錯誤本身徹底消失，連一行 log 都沒有。
func TestFinishReportsUnconsumedErrors(t *testing.T) {
	var gotMethod, gotPath string
	var gotErrs []error

	SetRequestErrorReporter(func(method, path string, errs []error) {
		gotMethod, gotPath, gotErrs = method, path, errs
	})
	defer SetRequestErrorReporter(nil)

	c := AcquireContext(httptest.NewRecorder(), httptest.NewRequest("POST", "/orders", nil))
	c.Error(errors.New("db write failed"))
	c.Finish()
	c.Release()

	if len(gotErrs) != 1 || gotErrs[0].Error() != "db write failed" {
		t.Errorf("reporter got %v, want the recorded error", gotErrs)
	}
	if gotMethod != "POST" || gotPath != "/orders" {
		t.Errorf("reporter got %s %s, want POST /orders", gotMethod, gotPath)
	}
}

func countTempFiles(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Skipf("cannot read temp dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "multipart-") {
			n++
		}
	}
	return n
}
