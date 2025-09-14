package context

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ===== 路由參數 =====

// Param 獲取路由參數
func (c *Context) Param(key string) string {
	return c.Params.ByName(key)
}

// SetParam 設置路由參數（用於測試）
func (c *Context) SetParam(key, value string) {
	for i, p := range c.Params {
		if p.Key == key {
			c.Params[i].Value = value
			return
		}
	}
	c.Params = append(c.Params, Param{Key: key, Value: value})
}

// ===== 查詢參數 =====

// Query 獲取查詢參數
func (c *Context) Query(key string) string {
	value, _ := c.GetQuery(key)
	return value
}

// DefaultQuery 獲取查詢參數，帶默認值
func (c *Context) DefaultQuery(key, defaultValue string) string {
	if value, ok := c.GetQuery(key); ok {
		return value
	}
	return defaultValue
}

// GetQuery 獲取查詢參數，返回是否存在
func (c *Context) GetQuery(key string) (string, bool) {
	if c.queryCache == nil {
		c.queryCache = c.Request.URL.Query()
	}
	values := c.queryCache[key]
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// QueryArray 獲取查詢參數陣列
func (c *Context) QueryArray(key string) []string {
	if c.queryCache == nil {
		c.queryCache = c.Request.URL.Query()
	}
	return c.queryCache[key]
}

// GetQueryArray 獲取查詢參數陣列，返回是否存在
func (c *Context) GetQueryArray(key string) ([]string, bool) {
	values := c.QueryArray(key)
	return values, len(values) > 0
}

// QueryMap 獲取查詢參數 map
func (c *Context) QueryMap(key string) map[string]string {
	if c.queryCache == nil {
		c.queryCache = c.Request.URL.Query()
	}
	return c.getFormMap(c.queryCache, key)
}

// GetQueryMap 獲取查詢參數 map，返回是否存在
func (c *Context) GetQueryMap(key string) (map[string]string, bool) {
	result := c.QueryMap(key)
	return result, len(result) > 0
}

// SetQuery 設置查詢參數（用於測試）
func (c *Context) SetQuery(key, value string) {
	if c.queryCache == nil {
		c.queryCache = c.Request.URL.Query()
	}
	c.queryCache.Set(key, value)
	c.Request.URL.RawQuery = c.queryCache.Encode()
}

// ===== 表單數據 =====

// PostForm 獲取表單資料
func (c *Context) PostForm(key string) string {
	value, _ := c.GetPostForm(key)
	return value
}

// DefaultPostForm 獲取表單資料，帶默認值
func (c *Context) DefaultPostForm(key, defaultValue string) string {
	if value, ok := c.GetPostForm(key); ok {
		return value
	}
	return defaultValue
}

// GetPostForm 獲取表單資料，返回是否存在
func (c *Context) GetPostForm(key string) (string, bool) {
	if c.formCache == nil {
		c.initFormCache()
	}
	values := c.formCache[key]
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// PostFormArray 獲取表單資料陣列
func (c *Context) PostFormArray(key string) []string {
	if c.formCache == nil {
		c.initFormCache()
	}
	return c.formCache[key]
}

// GetPostFormArray 獲取表單資料陣列，返回是否存在
func (c *Context) GetPostFormArray(key string) ([]string, bool) {
	values := c.PostFormArray(key)
	return values, len(values) > 0
}

// PostFormMap 獲取表單資料 map
func (c *Context) PostFormMap(key string) map[string]string {
	if c.formCache == nil {
		c.initFormCache()
	}
	return c.getFormMap(c.formCache, key)
}

// GetPostFormMap 獲取表單資料 map，返回是否存在
func (c *Context) GetPostFormMap(key string) (map[string]string, bool) {
	result := c.PostFormMap(key)
	return result, len(result) > 0
}

// DefaultFormValue 獲取表單值或默認值
func (c *Context) DefaultFormValue(key, defaultValue string) string {
	if value := c.Request.FormValue(key); value != "" {
		return value
	}
	return defaultValue
}

// GetFormValue 獲取表單值
func (c *Context) GetFormValue(key string) string {
	return c.Request.FormValue(key)
}

// initFormCache 初始化表單快取
func (c *Context) initFormCache() {
	c.Request.ParseForm()
	c.Request.ParseMultipartForm(defaultMemory)
	c.formCache = c.Request.PostForm
}

// getFormMap 從表單數據中提取 map
func (c *Context) getFormMap(values url.Values, key string) map[string]string {
	result := make(map[string]string)
	for k, v := range values {
		if strings.HasPrefix(k, key+"[") && strings.HasSuffix(k, "]") {
			mapKey := k[len(key)+1 : len(k)-1]
			if len(v) > 0 {
				result[mapKey] = v[0]
			}
		}
	}
	return result
}

// ===== 原始數據 =====

// GetRawData 獲取原始請求體資料
func (c *Context) GetRawData() ([]byte, error) {
	if c.rawData != nil {
		return c.rawData, nil
	}

	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}

	c.rawData = body
	c.Request.Body = ioutil.NopCloser(bytes.NewReader(body))

	return body, nil
}

// SetRawData 設置原始請求體資料
func (c *Context) SetRawData(data []byte) {
	c.rawData = data
	c.Request.Body = ioutil.NopCloser(bytes.NewReader(data))
}

// ===== 文件上傳 =====

// FormFile 獲取上傳的檔案
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	if c.Request.MultipartForm == nil {
		if err := c.Request.ParseMultipartForm(defaultMemory); err != nil {
			return nil, err
		}
	}
	file, header, err := c.Request.FormFile(name)
	if err != nil {
		return nil, err
	}
	file.Close()
	return header, nil
}

// GetFormFile 獲取上傳的文件（別名）
func (c *Context) GetFormFile(name string) (*multipart.FileHeader, error) {
	return c.FormFile(name)
}

// GetFormFiles 獲取多個上傳的文件
func (c *Context) GetFormFiles(name string) ([]*multipart.FileHeader, error) {
	if c.Request.MultipartForm == nil {
		if err := c.Request.ParseMultipartForm(defaultMemory); err != nil {
			return nil, err
		}
	}
	if c.Request.MultipartForm != nil && c.Request.MultipartForm.File != nil {
		return c.Request.MultipartForm.File[name], nil
	}
	return nil, http.ErrMissingFile
}

// MultipartForm 獲取多部分表單
func (c *Context) MultipartForm() (*multipart.Form, error) {
	err := c.Request.ParseMultipartForm(defaultMemory)
	return c.Request.MultipartForm, err
}

// SaveUploadedFile 保存上傳的檔案
func (c *Context) SaveUploadedFile(file *multipart.FileHeader, dst string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	// 創建目標檔案目錄
	if err = os.MkdirAll(filepath.Dir(dst), 0750); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}

// ===== 客戶端信息 =====

// ClientIP 獲取客戶端 IP
func (c *Context) ClientIP() string {
	// 檢查 X-Forwarded-For
	if xForwardedFor := c.GetHeader("X-Forwarded-For"); xForwardedFor != "" {
		parts := strings.Split(xForwardedFor, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			if ip != "" && isValidIP(ip) {
				return ip
			}
		}
	}

	// 檢查 X-Real-IP
	if xRealIP := c.GetHeader("X-Real-IP"); xRealIP != "" {
		if isValidIP(xRealIP) {
			return xRealIP
		}
	}

	// 檢查 X-Appengine-Remote-Addr (App Engine)
	if appEngine := c.GetHeader("X-Appengine-Remote-Addr"); appEngine != "" {
		return appEngine
	}

	// 從連接獲取
	if ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr)); err == nil {
		return ip
	}

	return ""
}

// GetClientIP 獲取客戶端 IP（別名）
func (c *Context) GetClientIP() string {
	return c.ClientIP()
}

// GetClientIPFromXForwardedFor 從 X-Forwarded-For 獲取客戶端 IP
func (c *Context) GetClientIPFromXForwardedFor() string {
	xff := c.GetHeader("X-Forwarded-For")
	if xff == "" {
		return ""
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip != "" && isValidIP(ip) {
			return ip
		}
	}
	return ""
}

// GetClientIPFromXRealIP 從 X-Real-IP 獲取客戶端 IP
func (c *Context) GetClientIPFromXRealIP() string {
	xRealIP := c.GetHeader("X-Real-IP")
	if xRealIP != "" && isValidIP(xRealIP) {
		return xRealIP
	}
	return ""
}

// RemoteIP 獲取遠端 IP（解析代理）
func (c *Context) RemoteIP() string {
	ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err != nil {
		return ""
	}
	return ip
}

// IsFromTrustedProxy 檢查請求是否來自可信代理
func (c *Context) IsFromTrustedProxy() bool {
	// 實現可信代理檢查邏輯
	// 這裡可以維護一個可信代理 IP 列表
	return false
}

// ContentType 獲取內容類型
func (c *Context) ContentType() string {
	return filterFlags(c.GetHeader("Content-Type"))
}

// IsWebsocket 檢查是否為 WebSocket 請求
func (c *Context) IsWebsocket() bool {
	return strings.Contains(strings.ToLower(c.GetHeader("Connection")), "upgrade") &&
		strings.EqualFold(c.GetHeader("Upgrade"), "websocket")
}

// IsAjax 檢查是否為 AJAX 請求
func (c *Context) IsAjax() bool {
	return c.GetHeader("X-Requested-With") == "XMLHttpRequest"
}

// ===== 請求方法檢查 =====

// IsGet 檢查是否為 GET 請求
func (c *Context) IsGet() bool {
	return c.Request.Method == "GET"
}

// IsPost 檢查是否為 POST 請求
func (c *Context) IsPost() bool {
	return c.Request.Method == "POST"
}

// IsPut 檢查是否為 PUT 請求
func (c *Context) IsPut() bool {
	return c.Request.Method == "PUT"
}

// IsDelete 檢查是否為 DELETE 請求
func (c *Context) IsDelete() bool {
	return c.Request.Method == "DELETE"
}

// IsPatch 檢查是否為 PATCH 請求
func (c *Context) IsPatch() bool {
	return c.Request.Method == "PATCH"
}

// IsOptions 檢查是否為 OPTIONS 請求
func (c *Context) IsOptions() bool {
	return c.Request.Method == "OPTIONS"
}

// IsHead 檢查是否為 HEAD 請求
func (c *Context) IsHead() bool {
	return c.Request.Method == "HEAD"
}

// ===== 請求信息 =====

// Method 返回請求方法
func (c *Context) Method() string {
	return c.Request.Method
}

// Path 返回請求路徑
func (c *Context) Path() string {
	return c.Request.URL.Path
}

// RawPath 返回原始路徑
func (c *Context) RawPath() string {
	return c.Request.URL.RawPath
}

// RequestURI 返回請求 URI
func (c *Context) RequestURI() string {
	return c.Request.RequestURI
}

// Scheme 返回請求協議（http 或 https）
func (c *Context) Scheme() string {
	if c.Request.TLS != nil {
		return "https"
	}
	if scheme := c.GetHeader("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	if scheme := c.GetHeader("X-Forwarded-Protocol"); scheme != "" {
		return scheme
	}
	if ssl := c.GetHeader("X-Forwarded-Ssl"); ssl == "on" {
		return "https"
	}
	if scheme := c.GetHeader("X-Url-Scheme"); scheme != "" {
		return scheme
	}
	return "http"
}

// Host 返回請求的主機名
func (c *Context) Host() string {
	if host := c.GetHeader("X-Forwarded-Host"); host != "" {
		return host
	}
	return c.Request.Host
}

// ===== 分頁輔助 =====

// GetPage 獲取頁碼（默認為 1）
func (c *Context) GetPage() int {
	page := c.DefaultQuery("page", "1")
	p, err := strconv.Atoi(page)
	if err != nil || p < 1 {
		return 1
	}
	return p
}

// GetPageSize 獲取每頁大小（默認為 10）
func (c *Context) GetPageSize() int {
	size := c.DefaultQuery("page_size", "10")
	s, err := strconv.Atoi(size)
	if err != nil || s < 1 {
		return 10
	}
	if s > 100 {
		return 100 // 最大限制
	}
	return s
}

// GetOffset 獲取偏移量
func (c *Context) GetOffset() int {
	page := c.GetPage()
	pageSize := c.GetPageSize()
	return (page - 1) * pageSize
}

// ===== 請求 ID =====

// GetRequestID 獲取請求 ID
func (c *Context) GetRequestID() string {
	if id := c.GetHeader("X-Request-Id"); id != "" {
		return id
	}
	if id := c.GetHeader("X-Request-ID"); id != "" {
		return id
	}
	// 生成新的請求 ID
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// SetRequestID 設置請求 ID
func (c *Context) SetRequestID(id string) {
	c.Header("X-Request-Id", id)
	c.Set("request_id", id)
}
