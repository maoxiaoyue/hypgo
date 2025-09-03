package middleware

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
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

// CSRF 創建 CSRF 保護中間件
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

	return func(c *hypcontext.Context) {
		// 檢查是否跳過
		if config.Skipper != nil && config.Skipper(c) {
			c.Next()
			return
		}

		// 對於安全的方法（GET, HEAD, OPTIONS），只生成 token
		if isSafeMethod(c.Request.Method) {
			token := generateCSRFToken(config.TokenLength)
			c.Set("csrf_token", token)
			setCSRFCookie(c, config, token)
			c.Next()
			return
		}

		// 對於不安全的方法，驗證 token
		cookie, err := c.Cookie(config.CookieName)
		if err != nil || cookie == "" {
			handleCSRFError(c, config)
			return
		}

		// 從請求中提取 token
		token := extractCSRFToken(c, config.TokenLookup)
		if token == "" {
			handleCSRFError(c, config)
			return
		}

		// 驗證 token
		if !validateCSRFToken(token, cookie) {
			handleCSRFError(c, config)
			return
		}

		// 設置新的 token
		newToken := generateCSRFToken(config.TokenLength)
		c.Set("csrf_token", newToken)
		setCSRFCookie(c, config, newToken)

		c.Next()
	}
}

// isSafeMethod 檢查是否為安全的 HTTP 方法
func isSafeMethod(method string) bool {
	return method == http.MethodGet ||
		method == http.MethodHead ||
		method == http.MethodOptions
}

// generateCSRFToken 生成 CSRF token
func generateCSRFToken(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = byte(fastrand() % 256)
	}
	return base64.URLEncoding.EncodeToString(b)
}

// extractCSRFToken 從請求中提取 CSRF token
func extractCSRFToken(c *hypcontext.Context, lookup string) string {
	parts := strings.Split(lookup, ":")
	if len(parts) != 2 {
		return ""
	}

	switch parts[0] {
	case "header":
		return c.GetHeader(parts[1])
	case "form":
		return c.PostForm(parts[1])
	case "query":
		return c.Query(parts[1])
	default:
		return ""
	}
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
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

// CORS 創建 CORS 中間件
func CORS(config CORSConfig) hypcontext.HandlerFunc {
	// 預處理允許的來源
	allowOrigins := make(map[string]bool)
	for _, origin := range config.AllowOrigins {
		allowOrigins[origin] = true
	}

	return func(c *hypcontext.Context) {
		origin := c.GetHeader("Origin")

		// 檢查來源是否允許
		if origin != "" {
			allowed := false
			if allowOrigins["*"] {
				allowed = true
			} else if allowOrigins[origin] {
				allowed = true
			}
			if allowed {
				c.Header("Access-Control-Allow-Origin", origin)

				if config.AllowCredentials {
					c.Header("Access-Control-Allow-Credentials", "true")
				}

				if len(config.ExposeHeaders) > 0 {
					c.Header("Access-Control-Expose-Headers", strings.Join(config.ExposeHeaders, ","))
				}
			}
		}

		// 處理預檢請求
		if c.Request.Method == "OPTIONS" {
			c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowMethods, ","))
			c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ","))

			if config.MaxAge > 0 {
				c.Header("Access-Control-Max-Age", fmt.Sprintf("%d", config.MaxAge))
			}

			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
