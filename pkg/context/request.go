// @chris
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
	"sync/atomic"
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

	body, err := readBodyWithHint(c.Request.Body, c.Request.ContentLength)
	if err != nil {
		return nil, err
	}

	c.rawData = body
	c.Request.Body = ioutil.NopCloser(bytes.NewReader(body))

	return body, nil
}

// maxBodyPreallocHint 依 Content-Length 預配置緩衝的上限。
// Content-Length 由客戶端宣告，不加上限會讓「宣告 1GB、只送 1B」
// 的請求直接配走 1GB 記憶體
const maxBodyPreallocHint = 1 << 20 // 1MB

// readBodyWithHint 以 Content-Length 預配置緩衝讀取 body。
// io.ReadAll 從 512B 起幾何成長：1MB body 要 ~12 次重配置與複製；
// ContentLength 幾乎總是已知，預配置後一次到位
func readBodyWithHint(r io.Reader, hint int64) ([]byte, error) {
	if hint > 0 {
		if hint > maxBodyPreallocHint {
			hint = maxBodyPreallocHint
		}
		buf := bytes.NewBuffer(make([]byte, 0, hint+1))
		if _, err := buf.ReadFrom(r); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return ioutil.ReadAll(r)
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

// trustedProxies 可信代理網段。只有當請求的直連來源（RemoteAddr）落在此清單內時，
// ClientIP 才會採信 X-Forwarded-For / X-Real-IP 等可偽造的轉發標頭。
// 未設定（預設）＝ 不信任任何代理 ＝ ClientIP 一律回傳 RemoteAddr。
var trustedProxies atomic.Pointer[[]*net.IPNet]

// SetTrustedProxies 設定可信代理網段（CIDR 或單一 IP，如 "10.0.0.0/8"、"127.0.0.1"）。
// 傳入空清單即回到「不信任任何代理」的安全預設。
//
// 安全模型：轉發標頭（X-Forwarded-For / X-Real-IP）由客戶端任意填寫，
// 若無條件採信，攻擊者只要自帶標頭就能偽裝成任意 IP，繞過 IPWhitelist
// 與 RateLimiter（兩者都以 ClientIP 為 key）。因此必須先確認「這一跳
// 確實來自我方代理」，才有理由相信它寫下的原始客戶端 IP。
//
// 一般部署（服務位於 nginx 之後）：SetTrustedProxies("127.0.0.1", "10.0.0.0/8")
func SetTrustedProxies(cidrs ...string) error {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, s := range cidrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			// 單一 IP → 轉成 /32（IPv4）或 /128（IPv6）
			ip := net.ParseIP(s)
			if ip == nil {
				return fmt.Errorf("context: invalid trusted proxy %q", s)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("context: invalid trusted proxy CIDR %q: %w", s, err)
		}
		nets = append(nets, n)
	}
	trustedProxies.Store(&nets)
	return nil
}

// isTrustedProxy 回報 ip 是否落在已設定的可信代理網段內
func isTrustedProxy(ip net.IP) bool {
	p := trustedProxies.Load()
	if p == nil || ip == nil {
		return false
	}
	for _, n := range *p {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP 獲取客戶端 IP。
//
// 僅當直連來源為可信代理（見 SetTrustedProxies）時才解析轉發標頭，
// 否則一律回傳實際的連線來源。這讓 IPWhitelist / RateLimiter 無法
// 被偽造的 X-Forwarded-For 繞過（未設定可信代理時為最嚴格的安全預設）。
func (c *Context) ClientIP() string {
	remote := c.RemoteIP()

	// 直連來源不是可信代理 → 轉發標頭一律不採信
	if !isTrustedProxy(net.ParseIP(remote)) {
		return remote
	}

	// 來自可信代理：由右往左取第一個「非可信代理」的位址，
	// 即最靠近我方、且不由我方代理鏈產生的那一跳
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			s := strings.TrimSpace(parts[i])
			if s == "" || !isValidIP(s) {
				continue
			}
			if ip := net.ParseIP(s); ip != nil && !isTrustedProxy(ip) {
				return s
			}
		}
	}

	if xRealIP := c.GetHeader("X-Real-IP"); xRealIP != "" && isValidIP(xRealIP) {
		return xRealIP
	}

	return remote
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

// IsFromTrustedProxy 檢查請求的直連來源是否為可信代理（見 SetTrustedProxies）。
// 未設定可信代理時恆為 false。
func (c *Context) IsFromTrustedProxy() bool {
	return isTrustedProxy(net.ParseIP(c.RemoteIP()))
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
