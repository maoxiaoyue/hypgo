// Package errors 提供 Typed Error Catalog
// 預定義結構化錯誤碼，讓 AI 與人都能一致理解和使用錯誤
package errors

import (
	"fmt"
	"net/http"
	"sync"
)

// AppError 描述一個預定義的應用程式錯誤
// AppError 是不可變的定義，使用 With() 產生帶上下文的副本
type AppError struct {
	Code       string         `json:"code" yaml:"code"`
	HTTPStatus int            `json:"http_status" yaml:"http_status"`
	Message    string         `json:"message" yaml:"message"`
	Category   string         `json:"category" yaml:"category"`
	Details    map[string]any `json:"details,omitempty" yaml:"details,omitempty"`
}

// Error 實作 error 介面
func (e *AppError) Error() string {
	if len(e.Details) > 0 {
		return fmt.Sprintf("[%s] %s %v", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// With 產生帶上下文細節的副本（原 AppError 不變）
func (e *AppError) With(key string, value any) *AppError {
	cp := e.clone()
	if cp.Details == nil {
		cp.Details = make(map[string]any)
	}
	cp.Details[key] = value
	return cp
}

// WithDetail 同 With，語義更明確
func (e *AppError) WithDetail(key string, value any) *AppError {
	return e.With(key, value)
}

// WithDetails 一次附加多個 key-value
func (e *AppError) WithDetails(kvs map[string]any) *AppError {
	cp := e.clone()
	if cp.Details == nil {
		cp.Details = make(map[string]any)
	}
	for k, v := range kvs {
		cp.Details[k] = v
	}
	return cp
}

// WithMessage 覆蓋預設訊息
func (e *AppError) WithMessage(msg string) *AppError {
	cp := e.clone()
	cp.Message = msg
	return cp
}

// Is 判斷是否為同一錯誤碼（忽略 Details）
func (e *AppError) Is(target error) bool {
	t, ok := target.(*AppError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// JSON 回傳適合做 HTTP response 的結構
func (e *AppError) JSON() map[string]any {
	result := map[string]any{
		"code":    e.Code,
		"message": e.Message,
	}
	if len(e.Details) > 0 {
		result["details"] = e.Details
	}
	return result
}

// clone 建立淺拷貝
func (e *AppError) clone() *AppError {
	cp := *e
	if e.Details != nil {
		cp.Details = make(map[string]any, len(e.Details))
		for k, v := range e.Details {
			cp.Details[k] = v
		}
	}
	return &cp
}

// --- 全域 Catalog ---

// Catalog 儲存所有已定義的 AppError
type Catalog struct {
	mu     sync.RWMutex
	errors map[string]*AppError // code → error
}

var globalCatalog = &Catalog{
	errors: make(map[string]*AppError),
}

// Define 定義並註冊一個新的 AppError
// 通常在 package-level var 區塊中使用
func Define(code string, httpStatus int, message, category string) *AppError {
	e := &AppError{
		Code:       code,
		HTTPStatus: httpStatus,
		Message:    message,
		Category:   category,
	}
	globalCatalog.register(e)
	return e
}

// register 內部註冊方法
func (c *Catalog) register(e *AppError) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errors[e.Code] = e
}

// GlobalCatalog 返回全域 Catalog
func GlobalCatalog() *Catalog {
	return globalCatalog
}

// All 返回所有已定義的錯誤（副本）
func (c *Catalog) All() []*AppError {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*AppError, 0, len(c.errors))
	for _, e := range c.errors {
		result = append(result, e)
	}
	return result
}

// Get 根據 code 查詢錯誤
func (c *Catalog) Get(code string) (*AppError, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.errors[code]
	return e, ok
}

// ByCategory 根據分類查詢
func (c *Catalog) ByCategory(category string) []*AppError {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*AppError
	for _, e := range c.errors {
		if e.Category == category {
			result = append(result, e)
		}
	}
	return result
}

// Len 返回已定義的錯誤數量
func (c *Catalog) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.errors)
}

// Reset 清空（測試用）
func (c *Catalog) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errors = make(map[string]*AppError)
}

// --- 常用預定義錯誤 ---

var (
	// 通用
	ErrNotFound         = Define("E0001", http.StatusNotFound, "Resource not found", "general")
	ErrBadRequest       = Define("E0002", http.StatusBadRequest, "Bad request", "general")
	ErrInternalError    = Define("E0003", http.StatusInternalServerError, "Internal server error", "general")
	ErrMethodNotAllowed = Define("E0004", http.StatusMethodNotAllowed, "Method not allowed", "general")

	// 驗證
	ErrValidationFailed = Define("E1001", http.StatusUnprocessableEntity, "Validation failed", "validation")
	ErrMissingField     = Define("E1002", http.StatusBadRequest, "Missing required field", "validation")
	ErrInvalidFormat    = Define("E1003", http.StatusBadRequest, "Invalid format", "validation")

	// 認證
	ErrUnauthorized = Define("E2001", http.StatusUnauthorized, "Authentication required", "auth")
	ErrForbidden    = Define("E2002", http.StatusForbidden, "Permission denied", "auth")
	ErrTokenExpired = Define("E2003", http.StatusUnauthorized, "Token expired", "auth")
)
