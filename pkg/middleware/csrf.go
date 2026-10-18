// @chris
package middleware

import (
	crand "crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// ===== CSRF 中間件 =====

// CSRFConfig CSRF 配置
type CSRFConfig struct {
	TokenLength    int
	TokenLookup    string // "header:X-CSRF-Token" or "form:csrf_token"
	CookieName     string
	CookieDomain   string
	CookiePath     string
	CookieMaxAge   int
	CookieSecure   bool
	CookieHTTPOnly bool
	ErrorHandler   func(c *hypcontext.Context)
	Skipper        func(c *hypcontext.Context) bool
}

// CSRF 創建 CSRF 保護中間件（double-submit cookie）。
// token 在 cookie 存在期間固定不變：先前每個 GET 與每次驗證成功都換新 token，
// 多分頁或頁面上其他請求一來，先前頁面表單帶的 token 就失效
func CSRF(config CSRFConfig) hypcontext.HandlerFunc {
	if config.TokenLength == 0 {
		config.TokenLength = 32
	}
	if config.TokenLookup == "" {
		config.TokenLookup = "header:X-CSRF-Token"
	}
	if config.CookieName == "" {
		config.CookieName = "_csrf"
	}
	if config.CookiePath == "" {
		config.CookiePath = "/"
	}
	src := parseValueSource(config.TokenLookup, "header", "form", "query")

	return func(c *hypcontext.Context) {
		// 檢查是否跳過
		if config.Skipper != nil && config.Skipper(c) {
			c.Next()
			return
		}

		cookie, _ := c.Cookie(config.CookieName)

		// 對於安全的方法（GET, HEAD, OPTIONS）：沿用既有 token，沒有才產生
		if isSafeMethod(c.Request.Method) {
			if cookie == "" {
				cookie = generateCSRFToken(config.TokenLength)
				setCSRFCookie(c, config, cookie)
			}
			c.Set("csrf_token", cookie)
			c.Next()
			return
		}

		// 對於不安全的方法，驗證 token
		if cookie == "" {
			handleCSRFError(c, config)
			return
		}
		token := src.get(c)
		if token == "" || !validateCSRFToken(token, cookie) {
			handleCSRFError(c, config)
			return
		}

		c.Set("csrf_token", cookie)
		c.Next()
	}
}

// isSafeMethod 檢查是否為安全的 HTTP 方法
func isSafeMethod(method string) bool {
	return method == http.MethodGet ||
		method == http.MethodHead ||
		method == http.MethodOptions
}

// generateCSRFToken 使用 crypto/rand 生成安全 CSRF token
func generateCSRFToken(length int) string {
	b := make([]byte, length)
	crand.Read(b) // Go 1.24 起保證不回傳錯誤
	// RawURLEncoding（無 '=' 補位）：補位的 '=' 寫進 cookie 時被跳脫成 %3D，前端從 cookie 讀出
	// 放進標頭送回的值與伺服器端解碼後的 cookie 不同，double-submit 驗證必定失敗
	return base64.RawURLEncoding.EncodeToString(b)
}

// validateCSRFToken 驗證 CSRF token
func validateCSRFToken(token, cookie string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(cookie)) == 1
}

// setCSRFCookie 設置 CSRF cookie
func setCSRFCookie(c *hypcontext.Context, config CSRFConfig, token string) {
	c.SetCookie(
		config.CookieName,
		token,
		config.CookieMaxAge,
		config.CookiePath,
		config.CookieDomain,
		config.CookieSecure,
		config.CookieHTTPOnly,
	)
}

// handleCSRFError 處理 CSRF 錯誤
func handleCSRFError(c *hypcontext.Context, config CSRFConfig) {
	if config.ErrorHandler != nil {
		config.ErrorHandler(c)
	} else {
		c.AbortWithStatus(http.StatusForbidden)
	}
}

// ===== CORS 中間件 =====

// CORSConfig CORS 配置
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string // 空時回應 preflight 請求的 Access-Control-Request-Headers
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

// CORS 創建 CORS 中間件。
//
// 先前的實作：沒帶 Origin 的一般 OPTIONS 也一律回 204（使用者註冊的 OPTIONS 路由永遠到不了）；
// 反射 Origin 卻沒有 Vary: Origin（中間快取可能把 A 網域的回應給 B 網域）；
// 設定 "*" 時也回傳請求的 Origin 而非字面的 "*"
func CORS(config CORSConfig) hypcontext.HandlerFunc {
	allowAll := false
	allowOrigins := make(map[string]bool)
	for _, origin := range config.AllowOrigins {
		if origin == "*" {
			allowAll = true
		} else {
			allowOrigins[origin] = true
		}
	}
	// 標頭值預先算好，不在每個 preflight 重新 Join
	methods := strings.Join(config.AllowMethods, ", ")
	headers := strings.Join(config.AllowHeaders, ", ")
	expose := strings.Join(config.ExposeHeaders, ", ")
	maxAge := ""
	if config.MaxAge > 0 {
		maxAge = strconv.Itoa(config.MaxAge)
	}

	return func(c *hypcontext.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next() // 非 CORS 請求（含一般 OPTIONS）不處理
			return
		}

		h := c.Writer.Header()
		h.Add("Vary", "Origin")
		if !allowAll && !allowOrigins[origin] {
			c.Next() // 不允許的來源：不給 CORS 標頭，由瀏覽器擋下
			return
		}

		// 允許所有來源且不帶憑證時回字面 "*"；帶憑證時規範禁止 "*"，只能反射 Origin
		if allowAll && !config.AllowCredentials {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		if config.AllowCredentials {
			h.Set("Access-Control-Allow-Credentials", "true")
		}
		if expose != "" {
			h.Set("Access-Control-Expose-Headers", expose)
		}

		// preflight：OPTIONS 且帶 Access-Control-Request-Method
		if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Methods", methods)
			if headers != "" {
				h.Set("Access-Control-Allow-Headers", headers)
			} else if req := c.GetHeader("Access-Control-Request-Headers"); req != "" {
				h.Set("Access-Control-Allow-Headers", req)
			}
			if maxAge != "" {
				h.Set("Access-Control-Max-Age", maxAge)
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
