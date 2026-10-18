// Package middleware 提供 HTTP/3 優化的中間件系統
//
// @chris
package middleware

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// ===== 安全頭中間件 =====

// SecurityConfig 安全配置
type SecurityConfig struct {
	XSSProtection         string // 預設 "0"（見 Security）
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
	// X-XSS-Protection 預設 "0"：舊版瀏覽器的 XSS 過濾器本身可被利用來製造漏洞，
	// 現代瀏覽器已移除該功能，OWASP 建議送 0 並改用 Content-Security-Policy
	xss := config.XSSProtection
	if xss == "" {
		xss = "0"
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
	challenge := fmt.Sprintf(`Basic realm=%q`, config.Realm)

	// 每個 401 都帶 WWW-Authenticate（RFC 7235）：先前只有沒帶 Authorization 時才帶，
	// 帳密錯誤時瀏覽器不會再次跳出登入框
	unauthorized := func(c *hypcontext.Context) {
		c.Header("WWW-Authenticate", challenge)
		c.AbortWithStatus(http.StatusUnauthorized)
	}

	return func(c *hypcontext.Context) {
		auth := c.GetHeader("Authorization")
		const prefix = "Basic "
		if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
			unauthorized(c)
			return
		}

		decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
		if err != nil {
			unauthorized(c)
			return
		}
		parts := bytes.SplitN(decoded, []byte(":"), 2)
		if len(parts) != 2 {
			unauthorized(c)
			return
		}
		username := string(parts[0])
		password := string(parts[1])

		valid := false
		if config.Validator != nil {
			valid = config.Validator(username, password, c)
		} else {
			expectedPass, ok := config.Authorized[username]
			// 帳號不存在時仍做一次比較，不讓回應時間洩漏帳號是否存在
			match := subtle.ConstantTimeCompare([]byte(password), []byte(expectedPass)) == 1
			valid = ok && match
		}

		if !valid {
			unauthorized(c)
			return
		}

		c.Set("user", username)
		c.Next()
	}
}

// ===== JWT 中間件 =====

// JWTConfig JWT 配置
type JWTConfig struct {
	ContextKey    string
	TokenLookup   string                                  // "header:Authorization" or "query:token" or "cookie:token"
	TokenHeadName string                                  // "Bearer"
	Validator     func(token string) (interface{}, error) // 必須提供：驗證 token 並回傳 claims
	ErrorHandler  func(c *hypcontext.Context, err error)
}

// JWT 創建 JWT 中間件。未提供 Validator 時於建立時 panic：
// 先前要等到第一個請求才發現（每個請求都回 401，原因只在 ErrorHandler 看得到）
func JWT(config JWTConfig) hypcontext.HandlerFunc {
	if config.Validator == nil {
		panic("middleware: JWTConfig.Validator is required (use middleware.HS256Validator(secret) for HS256)")
	}
	if config.ContextKey == "" {
		config.ContextKey = "user"
	}
	if config.TokenLookup == "" {
		config.TokenLookup = "header:Authorization"
	}
	if config.TokenHeadName == "" {
		config.TokenHeadName = "Bearer"
	}
	src := parseValueSource(config.TokenLookup, "header", "query", "cookie")

	return func(c *hypcontext.Context) {
		token := src.get(c)
		if src.kind == "header" {
			token = stripAuthScheme(token, config.TokenHeadName)
		}
		if token == "" {
			jwtError(c, config, fmt.Errorf("token not found"))
			return
		}

		claims, err := config.Validator(token)
		if err != nil {
			jwtError(c, config, fmt.Errorf("token validation failed: %w", err))
			return
		}

		// 設置使用者資訊到 context
		c.Set(config.ContextKey, claims)
		// 內建 HS256 claims：同步角色到 c.SetRoles，供 RequireRole / c.HasRole 使用
		if jc, ok := claims.(*JWTClaims); ok {
			c.SetRoles(jc.Roles)
		}
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

// stripAuthScheme 去掉標頭值開頭的驗證方案名稱（"Bearer xxx" → "xxx"）；
// 方案名稱不分大小寫（RFC 7235），沒有方案前綴時原樣回傳
func stripAuthScheme(value, scheme string) string {
	if s, rest, ok := strings.Cut(value, " "); ok && strings.EqualFold(s, scheme) {
		return rest
	}
	return value
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
	src := parseValueSource(config.KeyLookup, "header", "query", "form")
	// 以常數時間逐一比對：先前用 map 查找，比對時間隨 key 內容而異
	keys := make([][]byte, 0, len(config.Keys))
	for k, enabled := range config.Keys {
		if enabled {
			keys = append(keys, []byte(k))
		}
	}

	return func(c *hypcontext.Context) {
		key := src.get(c)
		if key == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		valid := false
		if config.Validator != nil {
			valid = config.Validator(key)
		} else {
			for _, k := range keys {
				if subtle.ConstantTimeCompare([]byte(key), k) == 1 {
					valid = true
				}
			}
		}

		if !valid {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set(config.ContextKey, key)
		c.Next()
	}
}

// ===== IP 白名單中間件 =====

// IPWhitelistConfig IP 白名單配置
type IPWhitelistConfig struct {
	AllowedIPs   []string
	AllowedCIDRs []string
	// TrustProxy 為 false 時一律以實際連線來源（RemoteAddr）判斷；為 true 時用 c.ClientIP()，
	// 只採信 SetTrustedProxies 設定的代理所帶的轉發標頭。先前此欄位完全未被讀取
	TrustProxy   bool
	ErrorHandler func(c *hypcontext.Context)
}

// IPWhitelist 創建 IP 白名單中間件。IP 與 CIDR 於建立時解析，格式錯誤直接 panic：
// 先前每個請求都重新解析 CIDR，寫錯的項目被靜默忽略
func IPWhitelist(config IPWhitelistConfig) hypcontext.HandlerFunc {
	nets := make([]*net.IPNet, 0, len(config.AllowedIPs)+len(config.AllowedCIDRs))
	for _, s := range config.AllowedIPs {
		ip := net.ParseIP(strings.TrimSpace(s))
		if ip == nil {
			panic(fmt.Sprintf("middleware: IPWhitelist: invalid IP %q", s))
		}
		bits := 128
		if ip.To4() != nil {
			ip, bits = ip.To4(), 32
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	for _, s := range config.AllowedCIDRs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(s))
		if err != nil {
			panic(fmt.Sprintf("middleware: IPWhitelist: invalid CIDR %q: %v", s, err))
		}
		nets = append(nets, n)
	}

	return func(c *hypcontext.Context) {
		client := c.RemoteIP()
		if config.TrustProxy {
			client = c.ClientIP()
		}
		if ip := net.ParseIP(client); ip != nil {
			for _, n := range nets {
				if n.Contains(ip) {
					c.Next()
					return
				}
			}
		}

		if config.ErrorHandler != nil {
			config.ErrorHandler(c)
		} else {
			c.AbortWithStatus(http.StatusForbidden)
		}
	}
}
