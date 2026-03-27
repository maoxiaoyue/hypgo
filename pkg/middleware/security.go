// Package middleware 提供 HTTP/3 優化的中間件系統
package middleware

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// ===== 安全頭中間件 =====

// SecurityConfig 安全配置
type SecurityConfig struct {
	XSSProtection         string
	ContentTypeNosniff    string
	XFrameOptions         string
	HSTSMaxAge            int
	HSTSIncludeSubdomains bool
	ContentSecurityPolicy string
	ReferrerPolicy        string
}

// Security 創建安全頭中間件
// GC 優化：所有 header 值在初始化時預計算，避免每請求的 fmt.Sprintf 和字串拼接
func Security(config SecurityConfig) hypcontext.HandlerFunc {
	// GC 優化：預計算所有 header 值（閉包捕獲不可變值）
	xss := config.XSSProtection
	if xss == "" {
		xss = "1; mode=block"
	}

	nosniff := config.ContentTypeNosniff
	if nosniff == "" {
		nosniff = "nosniff"
	}

	frame := config.XFrameOptions
	if frame == "" {
		frame = "DENY"
	}

	// HSTS 值預計算：避免每請求 fmt.Sprintf + 字串拼接
	var hstsValue string
	if config.HSTSMaxAge > 0 {
		hstsValue = fmt.Sprintf("max-age=%d", config.HSTSMaxAge)
		if config.HSTSIncludeSubdomains {
			hstsValue += "; includeSubDomains"
		}
	}

	csp := config.ContentSecurityPolicy
	referrer := config.ReferrerPolicy

	return func(c *hypcontext.Context) {
		c.Header("X-XSS-Protection", xss)
		c.Header("X-Content-Type-Options", nosniff)
		c.Header("X-Frame-Options", frame)

		if hstsValue != "" {
			c.Header("Strict-Transport-Security", hstsValue)
		}
		if csp != "" {
			c.Header("Content-Security-Policy", csp)
		}
		if referrer != "" {
			c.Header("Referrer-Policy", referrer)
		}

		c.Next()
	}
}

// ===== 認證中間件 =====

// AuthConfig 認證配置
type AuthConfig struct {
	Realm      string
	Authorized map[string]string // username -> password
	Validator  AuthValidator
}

// AuthValidator 認證驗證器
type AuthValidator func(username, password string, c *hypcontext.Context) bool

// BasicAuth 創建基本認證中間件
func BasicAuth(config AuthConfig) hypcontext.HandlerFunc {
	if config.Realm == "" {
		config.Realm = "Restricted"
	}

	return func(c *hypcontext.Context) {
		// 獲取認證頭
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.Header("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, config.Realm))
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 解析認證資訊
		const prefix = "Basic "
		if !strings.HasPrefix(auth, prefix) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 解碼認證資訊
		decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 分離使用者名稱和密碼
		parts := bytes.SplitN(decoded, []byte(":"), 2)
		if len(parts) != 2 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		username := string(parts[0])
		password := string(parts[1])

		// 驗證認證資訊
		valid := false
		if config.Validator != nil {
			valid = config.Validator(username, password, c)
		} else if expectedPass, ok := config.Authorized[username]; ok {
			valid = subtle.ConstantTimeCompare([]byte(password), []byte(expectedPass)) == 1
		}

		if !valid {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 設置使用者資訊
		c.Set("user", username)
		c.Next()
	}
}

// ===== JWT 中間件 =====

// JWTConfig JWT 配置
type JWTConfig struct {
	SigningKey    []byte
	SigningMethod string
	ContextKey    string
	TokenLookup   string // "header:Authorization" or "query:token" or "cookie:token"
	TokenHeadName string // "Bearer"
	Claims        interface{}
	Validator     func(token string) (interface{}, error) // 必須提供：驗證 token 並回傳 claims
	ErrorHandler  func(c *hypcontext.Context, err error)
}

// JWT 創建 JWT 中間件
// 需要提供 Validator 函式來驗證 token，否則一律拒絕（安全預設）
func JWT(config JWTConfig) hypcontext.HandlerFunc {
	if config.ContextKey == "" {
		config.ContextKey = "user"
	}

	if config.TokenLookup == "" {
		config.TokenLookup = "header:Authorization"
	}

	if config.TokenHeadName == "" {
		config.TokenHeadName = "Bearer"
	}

	return func(c *hypcontext.Context) {
		// 提取 token
		token := extractToken(c, config.TokenLookup, config.TokenHeadName)
		if token == "" {
			jwtError(c, config, fmt.Errorf("token not found"))
			return
		}

		// 使用 Validator 驗證 token（必須由使用者提供）
		if config.Validator == nil {
			jwtError(c, config, fmt.Errorf("JWT validator not configured"))
			return
		}

		claims, err := config.Validator(token)
		if err != nil {
			jwtError(c, config, fmt.Errorf("token validation failed: %w", err))
			return
		}

		// 設置使用者資訊到 context
		c.Set(config.ContextKey, claims)
		c.Next()
	}
}

// jwtError 統一 JWT 錯誤處理
func jwtError(c *hypcontext.Context, config JWTConfig, err error) {
	if config.ErrorHandler != nil {
		config.ErrorHandler(c, err)
	} else {
		c.AbortWithStatus(http.StatusUnauthorized)
	}
}

// extractToken 從請求中提取 token
func extractToken(c *hypcontext.Context, lookup, headName string) string {
	parts := strings.Split(lookup, ":")
	if len(parts) != 2 {
		return ""
	}

	switch parts[0] {
	case "header":
		token := c.GetHeader(parts[1])
		if token != "" && headName != "" {
			parts := strings.SplitN(token, " ", 2)
			if len(parts) == 2 && parts[0] == headName {
				return parts[1]
			}
		}
		return token
	case "query":
		return c.Query(parts[1])
	case "cookie":
		cookie, _ := c.Cookie(parts[1])
		return cookie
	}

	return ""
}

// ===== API Key 認證中間件 =====

// APIKeyConfig API Key 配置
type APIKeyConfig struct {
	KeyLookup  string // "header:X-API-Key" or "query:api_key"
	Validator  func(key string) bool
	Keys       map[string]bool
	ContextKey string
}

// APIKey 創建 API Key 認證中間件
func APIKey(config APIKeyConfig) hypcontext.HandlerFunc {
	if config.KeyLookup == "" {
		config.KeyLookup = "header:X-API-Key"
	}

	if config.ContextKey == "" {
		config.ContextKey = "api_key"
	}

	return func(c *hypcontext.Context) {
		// 提取 API Key
		key := extractAPIKey(c, config.KeyLookup)
		if key == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 驗證 API Key
		valid := false
		if config.Validator != nil {
			valid = config.Validator(key)
		} else if config.Keys != nil {
			valid = config.Keys[key]
		}

		if !valid {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 設置 API Key 到上下文
		c.Set(config.ContextKey, key)
		c.Next()
	}
}

// extractAPIKey 從請求中提取 API Key
func extractAPIKey(c *hypcontext.Context, lookup string) string {
	parts := strings.Split(lookup, ":")
	if len(parts) != 2 {
		return ""
	}

	switch parts[0] {
	case "header":
		return c.GetHeader(parts[1])
	case "query":
		return c.Query(parts[1])
	case "form":
		return c.PostForm(parts[1])
	default:
		return ""
	}
}

// ===== IP 白名單中間件 =====

// IPWhitelistConfig IP 白名單配置
type IPWhitelistConfig struct {
	AllowedIPs   []string
	AllowedCIDRs []string
	TrustProxy   bool
	ErrorHandler func(c *hypcontext.Context)
}

// IPWhitelist 創建 IP 白名單中間件
func IPWhitelist(config IPWhitelistConfig) hypcontext.HandlerFunc {
	// 預處理允許的 IP
	allowedIPs := make(map[string]bool)
	for _, ip := range config.AllowedIPs {
		allowedIPs[ip] = true
	}

	return func(c *hypcontext.Context) {
		// 獲取客戶端 IP
		clientIP := c.ClientIP()

		// 檢查是否在白名單中
		if !allowedIPs[clientIP] {
			// 檢查 CIDR
			allowed := false
			for _, cidr := range config.AllowedCIDRs {
				// 這裡需要實際的 CIDR 匹配邏輯
				_ = cidr
				// if matchCIDR(clientIP, cidr) {
				//     allowed = true
				//     break
				// }
			}

			if !allowed {
				if config.ErrorHandler != nil {
					config.ErrorHandler(c)
				} else {
					c.AbortWithStatus(http.StatusForbidden)
				}
				return
			}
		}

		c.Next()
	}
}

// ===== Session 中間件 =====

// SessionConfig Session 配置
type SessionConfig struct {
	Store      SessionStore
	CookieName string
	MaxAge     int
	Path       string
	Domain     string
	Secure     bool
	HTTPOnly   bool
	SameSite   http.SameSite
}

// SessionStore Session 存儲介面
type SessionStore interface {
	Get(sessionID string) (map[string]interface{}, error)
	Set(sessionID string, data map[string]interface{}, ttl int) error
	Delete(sessionID string) error
	Generate() string
}

// Session 創建 Session 中間件
func Session(config SessionConfig) hypcontext.HandlerFunc {
	if config.CookieName == "" {
		config.CookieName = "session_id"
	}

	if config.Path == "" {
		config.Path = "/"
	}

	return func(c *hypcontext.Context) {
		// 獲取或創建 session ID
		sessionID, err := c.Cookie(config.CookieName)
		if err != nil || sessionID == "" {
			sessionID = config.Store.Generate()
			c.SetCookie(
				config.CookieName,
				sessionID,
				config.MaxAge,
				config.Path,
				config.Domain,
				config.Secure,
				config.HTTPOnly,
			)
		}

		// 載入 session 資料
		sessionData, err := config.Store.Get(sessionID)
		if err != nil {
			sessionData = make(map[string]interface{})
		}

		// 設置到上下文
		c.Set("session_id", sessionID)
		c.Set("session", sessionData)

		// 處理請求
		c.Next()

		// 保存 session 資料
		if data, exists := c.Get("session"); exists {
			if sessionMap, ok := data.(map[string]interface{}); ok {
				config.Store.Set(sessionID, sessionMap, config.MaxAge)
			}
		}
	}
}
