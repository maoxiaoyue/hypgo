package fixture

import (
	"encoding/json"
	"net/http"
	"strings"
)

// TestResult 封裝測試回應，提供便捷的解析方法
type TestResult struct {
	Status  int
	Body    []byte
	Headers http.Header
}

// BodyString 回傳 body 字串（trim 空白）
func (r *TestResult) BodyString() string {
	return strings.TrimSpace(string(r.Body))
}

// JSON 將 body 解析為指定的結構
func (r *TestResult) JSON(v interface{}) error {
	return json.Unmarshal(r.Body, v)
}

// JSONMap 將 body 解析為 map
func (r *TestResult) JSONMap() (map[string]interface{}, error) {
	var m map[string]interface{}
	err := json.Unmarshal(r.Body, &m)
	return m, err
}

// Header 取得指定的 response header
func (r *TestResult) Header(key string) string {
	return r.Headers.Get(key)
}

// HasHeader 檢查是否有指定的 response header
func (r *TestResult) HasHeader(key string) bool {
	return r.Headers.Get(key) != ""
}
