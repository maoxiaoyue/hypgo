package context

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ===== 錯誤類型定義 =====

// Error 錯誤結構
type Error struct {
	Err  error
	Type ErrorType
	Meta interface{}
}

// errorMsgs 錯誤訊息集合
type errorMsgs []*Error

// ErrorType 錯誤類型
type ErrorType uint64

// ===== Context 錯誤處理方法 =====

// Error 添加錯誤
func (c *Context) Error(err error) *Error {
	if err == nil {
		return nil
	}

	parsedError, ok := err.(*Error)
	if !ok {
		parsedError = &Error{
			Err:  err,
			Type: ErrorTypePrivate,
		}
	}

	c.Errors = append(c.Errors, parsedError)
	return parsedError
}

// ===== Error 方法 =====

// SetType 設置錯誤類型
func (msg *Error) SetType(flags ErrorType) *Error {
	msg.Type = flags
	return msg
}

// SetMeta 設置錯誤元數據
func (msg *Error) SetMeta(data interface{}) *Error {
	msg.Meta = data
	return msg
}

// JSON 將錯誤轉換為 JSON
func (msg *Error) JSON() interface{} {
	jsonData := map[string]interface{}{}
	if msg.Meta != nil {
		switch value := msg.Meta.(type) {
		case map[string]interface{}:
			return value
		default:
			jsonData["meta"] = msg.Meta
		}
	}
	jsonData["error"] = msg.Error()
	return jsonData
}

// MarshalJSON 實現 json.Marshaler
func (msg *Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(msg.JSON())
}

// Error 實現 error 介面
func (msg *Error) Error() string {
	return msg.Err.Error()
}

// IsType 檢查錯誤類型
func (msg *Error) IsType(flags ErrorType) bool {
	return (msg.Type & flags) > 0
}

// Unwrap 返回包裝的錯誤
func (msg *Error) Unwrap() error {
	return msg.Err
}

// ===== errorMsgs 方法 =====

// ByType 按類型返回錯誤
func (a errorMsgs) ByType(typ ErrorType) errorMsgs {
	if len(a) == 0 {
		return nil
	}
	if typ == ErrorTypeAny {
		return a
	}
	var result errorMsgs
	for _, msg := range a {
		if msg.IsType(typ) {
			result = append(result, msg)
		}
	}
	return result
}

// Last 返回最後一個錯誤
func (a errorMsgs) Last() *Error {
	if length := len(a); length > 0 {
		return a[length-1]
	}
	return nil
}

// Errors 返回錯誤字串陣列
func (a errorMsgs) Errors() []string {
	if len(a) == 0 {
		return nil
	}
	errors := make([]string, len(a))
	for i, err := range a {
		errors[i] = err.Error()
	}
	return errors
}

// JSON 返回 JSON 格式的錯誤
func (a errorMsgs) JSON() interface{} {
	switch length := len(a); length {
	case 0:
		return nil
	case 1:
		return a.Last().JSON()
	default:
		jsonData := make([]interface{}, length)
		for i, err := range a {
			jsonData[i] = err.JSON()
		}
		return jsonData
	}
}

// MarshalJSON 實現 json.Marshaler
func (a errorMsgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.JSON())
}

// String 實現 fmt.Stringer
func (a errorMsgs) String() string {
	if len(a) == 0 {
		return ""
	}
	var buffer strings.Builder
	for i, msg := range a {
		fmt.Fprintf(&buffer, "Error #%02d: %s\n", i+1, msg.Err)
		if msg.Meta != nil {
			fmt.Fprintf(&buffer, "     Meta: %v\n", msg.Meta)
		}
	}
	return buffer.String()
}

// ===== 額外的錯誤處理輔助方法 =====

// HasErrors 檢查是否有錯誤
func (c *Context) HasErrors() bool {
	return len(c.Errors) > 0
}

// GetErrors 獲取所有錯誤
func (c *Context) GetErrors() errorMsgs {
	return c.Errors
}

// GetLastError 獲取最後一個錯誤
func (c *Context) GetLastError() *Error {
	return c.Errors.Last()
}

// GetErrorsByType 按類型獲取錯誤
func (c *Context) GetErrorsByType(typ ErrorType) errorMsgs {
	return c.Errors.ByType(typ)
}

// ClearErrors 清除所有錯誤
func (c *Context) ClearErrors() {
	c.Errors = c.Errors[:0]
}

// AddError 添加錯誤（帶類型和元數據）
func (c *Context) AddError(err error, typ ErrorType, meta interface{}) *Error {
	parsedError := &Error{
		Err:  err,
		Type: typ,
		Meta: meta,
	}
	c.Errors = append(c.Errors, parsedError)
	return parsedError
}

// AddPublicError 添加公開錯誤（可以顯示給用戶）
func (c *Context) AddPublicError(err error) *Error {
	return c.AddError(err, ErrorTypePublic, nil)
}

// AddPrivateError 添加私有錯誤（僅用於內部日誌）
func (c *Context) AddPrivateError(err error) *Error {
	return c.AddError(err, ErrorTypePrivate, nil)
}

// ErrorJSON 返回錯誤的 JSON 響應
func (c *Context) ErrorJSON(code int) {
	c.JSON(code, c.Errors.JSON())
}

// ErrorString 返回錯誤的字符串響應
func (c *Context) ErrorString(code int) {
	c.String(code, c.Errors.String())
}
