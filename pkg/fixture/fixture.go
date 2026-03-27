// Package fixture 提供框架層級的測試工廠
// 讓 AI 和人都能用 fluent API 建立完整的測試請求
//
// 使用範例：
//
//	result := fixture.Request(router).
//	    POST("/api/users").
//	    WithJSON(map[string]string{"name": "test"}).
//	    WithHeader("Authorization", "Bearer token").
//	    Expect(201).
//	    Run(t)
package fixture

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// RequestBuilder 提供 fluent API 建立測試請求
type RequestBuilder struct {
	router       *router.Router
	method       string
	path         string
	body         string
	headers      map[string]string
	query        map[string]string
	expectStatus int
}

// Request 建立新的 RequestBuilder
func Request(r *router.Router) *RequestBuilder {
	return &RequestBuilder{
		router:  r,
		method:  "GET",
		headers: make(map[string]string),
		query:   make(map[string]string),
	}
}

// GET 設定 GET 請求
func (b *RequestBuilder) GET(path string) *RequestBuilder {
	b.method = "GET"
	b.path = path
	return b
}

// POST 設定 POST 請求
func (b *RequestBuilder) POST(path string) *RequestBuilder {
	b.method = "POST"
	b.path = path
	return b
}

// PUT 設定 PUT 請求
func (b *RequestBuilder) PUT(path string) *RequestBuilder {
	b.method = "PUT"
	b.path = path
	return b
}

// DELETE 設定 DELETE 請求
func (b *RequestBuilder) DELETE(path string) *RequestBuilder {
	b.method = "DELETE"
	b.path = path
	return b
}

// PATCH 設定 PATCH 請求
func (b *RequestBuilder) PATCH(path string) *RequestBuilder {
	b.method = "PATCH"
	b.path = path
	return b
}

// WithJSON 設定 JSON body（自動設定 Content-Type）
func (b *RequestBuilder) WithJSON(v interface{}) *RequestBuilder {
	data, err := json.Marshal(v)
	if err != nil {
		b.body = "{}"
	} else {
		b.body = string(data)
	}
	b.headers["Content-Type"] = "application/json"
	return b
}

// WithBody 設定原始 body
func (b *RequestBuilder) WithBody(body string) *RequestBuilder {
	b.body = body
	return b
}

// WithHeader 設定請求標頭
func (b *RequestBuilder) WithHeader(key, value string) *RequestBuilder {
	b.headers[key] = value
	return b
}

// WithQuery 設定 query 參數
func (b *RequestBuilder) WithQuery(key, value string) *RequestBuilder {
	b.query[key] = value
	return b
}

// Expect 設定期望的狀態碼
func (b *RequestBuilder) Expect(status int) *RequestBuilder {
	b.expectStatus = status
	return b
}

// Run 執行測試請求並回傳結果
func (b *RequestBuilder) Run(t *testing.T) *TestResult {
	t.Helper()

	// 建立請求
	bodyReader := strings.NewReader(b.body)
	req := httptest.NewRequest(b.method, b.path, bodyReader)

	// 設定 headers
	for k, v := range b.headers {
		req.Header.Set(k, v)
	}

	// 設定 query
	if len(b.query) > 0 {
		q := req.URL.Query()
		for k, v := range b.query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	// 執行
	w := httptest.NewRecorder()
	b.router.ServeHTTP(w, req)

	result := &TestResult{
		Status:  w.Code,
		Body:    w.Body.Bytes(),
		Headers: w.Header(),
	}

	// 驗證狀態碼
	if b.expectStatus > 0 && w.Code != b.expectStatus {
		t.Errorf("%s %s: status = %d, want %d\nBody: %s",
			b.method, b.path, w.Code, b.expectStatus, w.Body.String())
	}

	return result
}
