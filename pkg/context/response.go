package context

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ===== JSON 響應 =====

// JSON 回應 JSON 資料
func (c *Context) JSON(code int, obj interface{}) {
	c.Render(code, jsonRender{obj})
}

// IndentedJSON 回應格式化的 JSON
func (c *Context) IndentedJSON(code int, obj interface{}) {
	c.Render(code, indentedJSONRender{Data: obj})
}

// SecureJSON 回應安全的 JSON（防止 JSON 劫持）
func (c *Context) SecureJSON(code int, obj interface{}) {
	c.Render(code, secureJSONRender{Data: obj})
}

// JSONP 回應 JSONP
func (c *Context) JSONP(code int, obj interface{}) {
	callback := c.DefaultQuery("callback", "")
	if callback == "" {
		c.JSON(code, obj)
	} else {
		c.Render(code, jsonpJSONRender{Data: callback})
	}
}

// AsciiJSON 回應 ASCII JSON
func (c *Context) AsciiJSON(code int, obj interface{}) {
	c.Render(code, asciiJSONRender{Data: obj})
}

// PureJSON 回應純 JSON（不轉義 HTML）
func (c *Context) PureJSON(code int, obj interface{}) {
	c.Render(code, pureJSONRender{Data: obj})
}

// WriteJSON 直接寫入 JSON（低層級）
func (c *Context) WriteJSON(code int, obj interface{}) error {
	c.WriteHeader(code)
	c.Header("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(c.Writer).Encode(obj)
}

// ===== XML 響應 =====

// XML 回應 XML
func (c *Context) XML(code int, obj interface{}) {
	c.Render(code, xmlRender{Data: obj})
}

// WriteXML 直接寫入 XML（低層級）
func (c *Context) WriteXML(code int, obj interface{}) error {
	c.WriteHeader(code)
	c.Header("Content-Type", "application/xml; charset=utf-8")
	return xml.NewEncoder(c.Writer).Encode(obj)
}

// ===== YAML 響應 =====

// YAML 回應 YAML
func (c *Context) YAML(code int, obj interface{}) {
	c.Render(code, yamlRender{Data: obj})
}

// ===== ProtoBuf 響應 =====

// ProtoBuf 回應 ProtoBuf
func (c *Context) ProtoBuf(code int, obj interface{}) {
	c.Render(code, protoBufRender{Data: obj})
}

// ===== 文本響應 =====

// String 回應字串
func (c *Context) String(code int, format string, values ...interface{}) {
	c.Render(code, stringRender{Format: format, Data: values})
}

// HTML 回應 HTML
func (c *Context) HTML(code int, name string, obj interface{}) {
	if c.routerGroup != nil && c.routerGroup.engine != nil {
		instance := c.routerGroup.engine.HTMLRender.Instance(name, obj)
		c.Render(code, instance)
	} else {
		// 備用：直接渲染 HTML 字串
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(code)
		if htmlStr, ok := obj.(string); ok {
			io.WriteString(c.Writer, htmlStr)
		}
	}
}

// ===== 文件響應 =====

// File 回應檔案
func (c *Context) File(filepath string) {
	http.ServeFile(c.Writer, c.Request, filepath)
}

// FileFromFS 從檔案系統回應檔案
func (c *Context) FileFromFS(filepath string, fs http.FileSystem) {
	defer func(old string) {
		c.Request.URL.Path = old
	}(c.Request.URL.Path)

	c.Request.URL.Path = filepath
	http.FileServer(fs).ServeHTTP(c.Writer, c.Request)
}

// FileAttachment 回應檔案作為附件下載
func (c *Context) FileAttachment(filepath, filename string) {
	c.Writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	http.ServeFile(c.Writer, c.Request, filepath)
}

// ===== 數據響應 =====

// Data 回應原始資料
func (c *Context) Data(code int, contentType string, data []byte) {
	c.Render(
		code, dataRender{
			ContentType: contentType,
			Data:        data,
		})
}

// DataFromReader 從 Reader 回應資料
func (c *Context) DataFromReader(code int, contentLength int64, contentType string, reader io.Reader, extraHeaders map[string]string) {
	c.Render(code, readerRender{
		Headers:       extraHeaders,
		ContentType:   contentType,
		ContentLength: contentLength,
		Reader:        reader,
	})
}

// ===== Server-Sent Events =====

// SSEvent 發送 Server-Sent Event
func (c *Context) SSEvent(name string, message interface{}) {
	c.Render(-1, sseventRender{
		Event: name,
		Data:  message,
	})
}

// ===== 重定向 =====

// Redirect 重定向
func (c *Context) Redirect(code int, location string) {
	c.Render(-1, redirectRender{
		Code:     code,
		Location: location,
		Request:  c.Request,
	})
}

// ===== 渲染 =====

// Render 渲染回應
func (c *Context) Render(code int, r Render) {
	c.Status(code)

	if !bodyAllowedForStatus(code) {
		r.WriteContentType(c.Writer)
		c.Writer.WriteHeaderNow()
		return
	}

	if err := r.Render(c.Writer); err != nil {
		panic(err)
	}
}

// ===== 內容協商 =====

// Negotiate 內容協商
func (c *Context) Negotiate(code int, config Negotiate) {
	switch c.NegotiateFormat(config.Offered...) {
	case MIMEJSON:
		c.JSON(code, config.JSONData)
	case MIMEHTML:
		c.HTML(code, config.HTMLName, config.HTMLData)
	case MIMEXML:
		c.XML(code, config.XMLData)
	case MIMEYAML:
		c.YAML(code, config.YAMLData)
	default:
		c.AbortWithError(http.StatusNotAcceptable, errors.New("the accepted formats are not offered by the server"))
	}
}

// NegotiateFormat 協商格式
func (c *Context) NegotiateFormat(offered ...string) string {
	if c.Accepted == nil {
		c.Accepted = parseAccept(c.GetHeader("Accept"))
	}

	if len(c.Accepted) == 0 {
		return offered[0]
	}

	for _, accept := range c.Accepted {
		for _, offer := range offered {
			if accept == offer || accept == "*/*" {
				return offer
			}
		}
	}

	return ""
}

// SetAccepted 設置接受的內容類型
func (c *Context) SetAccepted(formats ...string) {
	c.Accepted = formats
}

// ===== Header 操作 =====

// Header 設置回應頭
func (c *Context) Header(key, value string) {
	if c.Writer.Written() {
		return
	}
	c.Writer.Header().Set(key, value)
}

// GetHeader 獲取請求頭
func (c *Context) GetHeader(key string) string {
	return c.Request.Header.Get(key)
}

// RequestHeader 獲取所有請求頭（別名）
func (c *Context) RequestHeader(key string) string {
	return c.Request.Header.Get(key)
}

// Status 設置狀態碼
func (c *Context) Status(code int) {
	c.Writer.WriteHeader(code)
}

// WriteHeader 寫入狀態碼（別名）
func (c *Context) WriteHeader(code int) {
	c.Status(code)
}

// ===== 直接寫入 =====

// Write 寫入數據
func (c *Context) Write(data []byte) (int, error) {
	return c.Writer.Write(data)
}

// WriteString 寫入字符串
func (c *Context) WriteString(s string) (int, error) {
	return c.Writer.WriteString(s)
}

// ===== Cookie 操作 =====

// SetCookie 設置 Cookie
func (c *Context) SetCookie(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
	if path == "" {
		path = "/"
	}
	cookie := &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		MaxAge:   maxAge,
		Path:     path,
		Domain:   domain,
		SameSite: c.sameSite,
		Secure:   secure,
		HttpOnly: httpOnly,
	}
	http.SetCookie(c.Writer, cookie)
}

// SetSameSite 設置 SameSite
func (c *Context) SetSameSite(sameSite http.SameSite) {
	c.sameSite = sameSite
}

// Cookie 獲取 Cookie
func (c *Context) Cookie(name string) (string, error) {
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return "", err
	}
	val, _ := url.QueryUnescape(cookie.Value)
	return val, nil
}

// ===== 快取控制 =====

// SetCacheControl 設置快取控制
func (c *Context) SetCacheControl(value string) {
	c.Header("Cache-Control", value)
}

// NoCache 設置不快取
func (c *Context) NoCache() {
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

// SetETag 設置 ETag
func (c *Context) SetETag(value string) {
	c.Header("ETag", value)
}

// CheckETag 檢查 ETag 是否匹配
func (c *Context) CheckETag(value string) bool {
	match := c.GetHeader("If-None-Match")
	return match != "" && match == value
}

// SetLastModified 設置最後修改時間
func (c *Context) SetLastModified(t time.Time) {
	c.Header("Last-Modified", t.UTC().Format(http.TimeFormat))
}

// ===== CORS 支援 =====

// SetCORS 設置 CORS 頭
func (c *Context) SetCORS(origin string) {
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Credentials", "true")
}

// SetCORSHeaders 設置完整的 CORS 頭
func (c *Context) SetCORSHeaders(origin, methods, headers string) {
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Methods", methods)
	c.Header("Access-Control-Allow-Headers", headers)
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Header("Access-Control-Max-Age", "86400")
}

// ===== 安全相關 Header =====

// SetSecurityHeaders 設置安全頭
func (c *Context) SetSecurityHeaders() {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("X-XSS-Protection", "1; mode=block")
	c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
}

// SetCSP 設置內容安全策略
func (c *Context) SetCSP(policy string) {
	c.Header("Content-Security-Policy", policy)
}

// ===== Negotiate 結構 =====

// Negotiate 內容協商配置
type Negotiate struct {
	Offered  []string
	HTMLName string
	HTMLData interface{}
	JSONData interface{}
	XMLData  interface{}
	YAMLData interface{}
	TOMLData interface{}
	Data     interface{}
}

// TOML 回應 TOML
func (c *Context) TOML(code int, obj interface{}) {
	c.Render(code, tomlRender{Data: obj})
}
