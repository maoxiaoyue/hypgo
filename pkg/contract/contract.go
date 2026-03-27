// Package contract 提供 Contract Testing 內建驗證功能
// 根據 schema-first 路由的 metadata 自動驗證 handler 的行為
// 確保 AI 生成的程式碼符合宣告的合約
package contract

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// TestCase 定義單一 contract 測試案例
type TestCase struct {
	// Route 格式為 "METHOD /path"，例如 "POST /api/users"
	Route string

	// Input 為 JSON 格式的請求 body
	Input string

	// Headers 為自訂請求標頭
	Headers map[string]string

	// Query 為 URL query 參數
	Query map[string]string

	// ExpectStatus 為期望的 HTTP 狀態碼
	ExpectStatus int

	// ExpectSchema 為 true 時，驗證回應 body 是否符合 Output schema
	ExpectSchema bool

	// ExpectBody 為非空時，精確比對回應 body
	ExpectBody string

	// schemaPath 為內部欄位，用於 TestAll 時保留原始 schema 路徑（含 :param）
	schemaPath string
}

// Test 執行單一 contract 測試
func Test(t *testing.T, r *router.Router, tc TestCase) {
	t.Helper()
	testInternal(t, r, tc)
}

// testInternal 為內部共用實作
func testInternal(t *testing.T, r *router.Router, tc TestCase) {
	t.Helper()

	method, path := parseRoute(tc.Route)
	if method == "" || path == "" {
		t.Fatalf("invalid route format: %q (expected \"METHOD /path\")", tc.Route)
	}

	// 建立請求
	var body *strings.Reader
	if tc.Input != "" {
		body = strings.NewReader(tc.Input)
	} else {
		body = strings.NewReader("")
	}

	req := httptest.NewRequest(method, path, body)
	if tc.Input != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 設定自訂標頭
	for k, v := range tc.Headers {
		req.Header.Set(k, v)
	}

	// 設定 query 參數
	if len(tc.Query) > 0 {
		q := req.URL.Query()
		for k, v := range tc.Query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	// 執行請求
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 驗證狀態碼
	if tc.ExpectStatus > 0 && w.Code != tc.ExpectStatus {
		t.Errorf("[%s] status = %d, want %d", tc.Route, w.Code, tc.ExpectStatus)
	}

	// 驗證 schema（使用 schemaPath 或 URL path 查找）
	if tc.ExpectSchema {
		lookupPath := path
		if tc.schemaPath != "" {
			lookupPath = tc.schemaPath
		}
		s, ok := schema.Global().Get(method, lookupPath)
		if !ok {
			t.Errorf("[%s] no schema registered for validation", tc.Route)
		} else {
			// 驗證 Input schema（請求 body）
			if s.Input != nil && tc.Input != "" {
				if err := validateRequest([]byte(tc.Input), s.Input); err != nil {
					t.Errorf("[%s] request schema validation failed: %v", tc.Route, err)
				}
			}
			// 驗證 Output schema（回應 body）
			if s.Output != nil {
				if err := validateResponse(w.Body.Bytes(), s.Output); err != nil {
					t.Errorf("[%s] response schema validation failed: %v", tc.Route, err)
				}
			}
		}
	}

	// 精確比對 body
	if tc.ExpectBody != "" {
		got := strings.TrimSpace(w.Body.String())
		want := strings.TrimSpace(tc.ExpectBody)
		if got != want {
			t.Errorf("[%s] body = %q, want %q", tc.Route, got, want)
		}
	}
}

// TestAll 自動測試所有 schema-registered 路由
// 為每個路由生成最小有效的測試案例並執行
func TestAll(t *testing.T, r *router.Router) {
	t.Helper()

	routes := schema.Global().All()
	if len(routes) == 0 {
		t.Skip("no schema-registered routes found")
	}

	for _, route := range routes {
		name := route.Method + " " + route.Path
		t.Run(name, func(t *testing.T) {
			tc := generateTestCase(route)
			// 使用原始 schema path 做 schema 查找
			tc.schemaPath = route.Path
			testInternal(t, r, tc)
		})
	}
}

// TestRoute 測試單一路由是否已註冊且可回應
func TestRoute(t *testing.T, r *router.Router, method, path string, expectStatus int) {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != expectStatus {
		t.Errorf("%s %s: status = %d, want %d", method, path, w.Code, expectStatus)
	}
}

// parseRoute 解析 "METHOD /path" 格式
func parseRoute(route string) (method, path string) {
	parts := strings.SplitN(route, " ", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
