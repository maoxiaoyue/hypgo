package contract

// @ai purpose: Test Observe — 以完整 HTTP 捕捉模式執行 contract 測試，並生成結構化 HTML 報告
// @ai input: *testing.T, *router.Router, 可選的函式名稱過濾字串與 ObserveOptions
// @ai output: []ObserveResult（每條路由一筆完整記錄），並將 HTML 報告寫入 .hyp/observe_*.html
// @ai sideeffect: 建立 .hyp/ 目錄、寫入 HTML 報告檔案、可選擇自動開啟瀏覽器
// date: 2026-05-23

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	hypRouter "github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// ObserveOptions 配置 Observe 模式的行為
type ObserveOptions struct {
	// OutputPath 指定 HTML 報告的輸出路徑
	// 預設：.hyp/observe_YYYYMMDD_HHMMSS.html
	OutputPath string

	// OpenBrowser 生成報告後自動在瀏覽器開啟
	// macOS: open, Linux: xdg-open, Windows: start
	OpenBrowser bool

	// Silent 抑制 t.Logf 的報告路徑輸出
	Silent bool
}

// CapturedRequest 記錄實際發出的 HTTP 請求完整內容
type CapturedRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    string
}

// CapturedResponse 記錄實際收到的 HTTP 回應完整內容
type CapturedResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       string
}

// ValidationStep 代表驗證鏈中的單一檢查項目
type ValidationStep struct {
	Name   string
	Pass   bool
	Detail string
}

// ObserveResult 為單一路由的完整觀察記錄
type ObserveResult struct {
	Route      schema.Route
	Request    CapturedRequest
	Response   CapturedResponse
	Steps      []ValidationStep
	Pass       bool
	FailReason string
	Duration   time.Duration
	Timestamp  time.Time
}

// ObserveAll 以觀察模式執行所有 schema-registered REST 路由的 contract 測試
// 捕捉每條路由的完整 HTTP 交換並生成 HTML 報告
//
// 用法：
//
//	func TestObserve(t *testing.T) {
//	    contract.ObserveAll(t, setupRouter())
//	}
//
//	func TestObserveWithBrowser(t *testing.T) {
//	    contract.ObserveAll(t, setupRouter(), contract.ObserveOptions{OpenBrowser: true})
//	}
func ObserveAll(t *testing.T, r *hypRouter.Router, opts ...ObserveOptions) []ObserveResult {
	if t != nil {
		t.Helper()
	}
	return runObserve(t, r, "", mergeObserveOpts(opts))
}

// Observe 以觀察模式執行與 funcName 相關的路由 contract 測試
// funcName 會對路由的 HandlerNames、Path、Summary、Tags 做大小寫不敏感的子字串比對
// 空字串等同於 ObserveAll
//
// 用法：
//
//	func TestObserveCreateOrder(t *testing.T) {
//	    contract.Observe(t, setupRouter(), "createOrderHandler")
//	}
//
//	// 也可以用路徑片段或標籤過濾
//	func TestObserveOrders(t *testing.T) {
//	    contract.Observe(t, setupRouter(), "orders")
//	}
func Observe(t *testing.T, r *hypRouter.Router, funcName string, opts ...ObserveOptions) []ObserveResult {
	if t != nil {
		t.Helper()
	}
	return runObserve(t, r, funcName, mergeObserveOpts(opts))
}

// runObserve 為 ObserveAll 和 Observe 的共用內部實作
func runObserve(t *testing.T, r *hypRouter.Router, filter string, opt ObserveOptions) []ObserveResult {
	routes := schema.Global().All()
	if len(routes) == 0 {
		if t != nil {
			t.Skip("no schema-registered routes found — SKIP")
		}
		return nil
	}

	var results []ObserveResult
	for _, route := range routes {
		if !route.IsREST() {
			continue
		}
		if filter != "" && !observeRouteMatchesFilter(route, filter) {
			continue
		}
		results = append(results, captureRouteExchange(r, route))
	}

	if len(results) == 0 {
		if t != nil {
			t.Logf("[observe] no routes matched filter %q", filter)
		}
		return results
	}

	// 寫入 HTML 報告
	outPath := opt.OutputPath
	if outPath == "" {
		if err := os.MkdirAll(".hyp", 0o755); err == nil {
			ts := time.Now().Format("20060102_150405")
			outPath = filepath.Join(".hyp", fmt.Sprintf("observe_%s.html", ts))
		}
	}
	if outPath != "" {
		html := GenerateObserveHTML(results, filter)
		if err := os.WriteFile(outPath, []byte(html), 0o644); err == nil {
			if t != nil && !opt.Silent {
				t.Logf("[observe] report → %s", outPath)
			}
			if opt.OpenBrowser {
				openObserveBrowser(outPath)
			}
		}
	}

	return results
}

// captureRouteExchange 執行一條路由的測試並捕捉完整的 HTTP 交換記錄
func captureRouteExchange(r *hypRouter.Router, route schema.Route) ObserveResult {
	tc := generateTestCase(route)
	tc.schemaPath = route.Path

	start := time.Now()

	// 解析 Route 字串（格式："METHOD /path"）
	method, urlPath := parseRoute(tc.Route)

	// 建立請求
	req := httptest.NewRequest(method, urlPath, strings.NewReader(tc.Input))
	if tc.Input != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range tc.Headers {
		req.Header.Set(k, v)
	}
	if len(tc.Query) > 0 {
		q := req.URL.Query()
		for k, v := range tc.Query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	// 捕捉請求詳情
	capturedReq := CapturedRequest{
		Method:  method,
		Path:    req.URL.RequestURI(),
		Body:    tc.Input,
		Headers: make(map[string]string),
	}
	for k, v := range req.Header {
		capturedReq.Headers[k] = strings.Join(v, ", ")
	}

	// 執行請求
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	dur := time.Since(start)

	// 捕捉回應詳情
	respBody := w.Body.Bytes()
	capturedResp := CapturedResponse{
		StatusCode: w.Code,
		Body:       string(respBody),
		Headers:    make(map[string]string),
	}
	for k, v := range w.Header() {
		capturedResp.Headers[k] = strings.Join(v, ", ")
	}

	// 逐步驗證
	steps, pass, failReason := runObserveValidation(route, tc, w.Code, respBody)

	return ObserveResult{
		Route:      route,
		Request:    capturedReq,
		Response:   capturedResp,
		Steps:      steps,
		Pass:       pass,
		FailReason: failReason,
		Duration:   dur,
		Timestamp:  time.Now(),
	}
}

// runObserveValidation 逐步執行驗證並記錄每個步驟的結果
func runObserveValidation(route schema.Route, tc TestCase, code int, body []byte) (steps []ValidationStep, pass bool, reason string) {
	pass = true

	// Step 1：狀態碼
	wantStatus := tc.ExpectStatus
	statusOK := code == wantStatus
	steps = append(steps, ValidationStep{
		Name:   "Status Code",
		Pass:   statusOK,
		Detail: fmt.Sprintf("got %d, want %d", code, wantStatus),
	})
	if !statusOK {
		pass = false
		reason = fmt.Sprintf("status = %d, want %d", code, wantStatus)
	}

	// Step 2：回應 body 非空（有 Output schema 時）
	if route.Output != nil {
		bodyOK := len(bytes.TrimSpace(body)) > 0
		steps = append(steps, ValidationStep{
			Name:   "Response Body",
			Pass:   bodyOK,
			Detail: observeIfElse(bodyOK, "body present", "body is empty"),
		})
		if !bodyOK && pass {
			pass = false
			reason = "response body is empty"
		}
	}

	// Step 3：Input schema 驗證
	if route.Input != nil && tc.Input != "" {
		err := validateRequest([]byte(tc.Input), route.Input)
		inputOK := err == nil
		var inputDetail string
		if inputOK {
			inputDetail = "matches schema"
		} else {
			inputDetail = err.Error()
		}
		steps = append(steps, ValidationStep{
			Name:   fmt.Sprintf("Input Schema (%s)", route.InputName),
			Pass:   inputOK,
			Detail: inputDetail,
		})
	}

	// Step 4：Output schema 驗證
	if route.Output != nil && len(bytes.TrimSpace(body)) > 0 {
		err := validateResponse(body, route.Output)
		outOK := err == nil
		var outDetail string
		if outOK {
			outDetail = "matches schema"
		} else {
			outDetail = err.Error()
		}
		steps = append(steps, ValidationStep{
			Name:   fmt.Sprintf("Output Schema (%s)", route.OutputName),
			Pass:   outOK,
			Detail: outDetail,
		})
		if !outOK && pass {
			pass = false
			reason = err.Error()
		}
	}

	return
}

// observeRouteMatchesFilter 判斷路由是否符合過濾字串
// 對 HandlerNames、Path、Summary、Tags 做大小寫不敏感的子字串比對
func observeRouteMatchesFilter(route schema.Route, filter string) bool {
	f := strings.ToLower(filter)
	for _, h := range route.HandlerNames {
		if strings.Contains(strings.ToLower(h), f) {
			return true
		}
	}
	if strings.Contains(strings.ToLower(route.Path), f) {
		return true
	}
	if strings.Contains(strings.ToLower(route.Summary), f) {
		return true
	}
	for _, tag := range route.Tags {
		if strings.Contains(strings.ToLower(tag), f) {
			return true
		}
	}
	return false
}

// mergeObserveOpts 取第一個 ObserveOptions 或回傳預設值
func mergeObserveOpts(opts []ObserveOptions) ObserveOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return ObserveOptions{}
}

// observeIfElse 為內部使用的三元表達式替代
func observeIfElse(cond bool, t, f string) string {
	if cond {
		return t
	}
	return f
}

// openObserveBrowser 在預設瀏覽器中開啟指定路徑的 HTML 報告
func openObserveBrowser(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	url := "file://" + filepath.ToSlash(abs)
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start() //nolint:errcheck
	case "linux":
		exec.Command("xdg-open", url).Start() //nolint:errcheck
	case "windows":
		exec.Command("cmd", "/c", "start", "", url).Start() //nolint:errcheck
	}
}
