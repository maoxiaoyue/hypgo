package context

import (
	"encoding/base64"
	"strings"
)

// ===== 認證相關方法 =====

// BasicAuth 獲取 Basic 認證信息
func (c *Context) BasicAuth() (username, password string, ok bool) {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return
	}
	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return
	}

	cs := string(decoded)
	s := strings.IndexByte(cs, ':')
	if s < 0 {
		return
	}
	return cs[:s], cs[s+1:], true
}

// GetAuthToken 獲取 Bearer Token
func (c *Context) GetAuthToken() string {
	auth := c.GetHeader("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(auth, prefix) {
		return auth[len(prefix):]
	}
	return ""
}

// SetAuthToken 設置認證 Token（用於測試或內部使用）
func (c *Context) SetAuthToken(token string) {
	c.Header("Authorization", "Bearer "+token)
}

// GetAPIKey 從 Header 獲取 API Key
func (c *Context) GetAPIKey(headerName string) string {
	if headerName == "" {
		headerName = "X-API-Key"
	}
	return c.GetHeader(headerName)
}

// CheckAPIKey 檢查 API Key 是否匹配
func (c *Context) CheckAPIKey(headerName, expectedKey string) bool {
	return c.GetAPIKey(headerName) == expectedKey
}

// GetJWT 獲取 JWT Token（從 Authorization header 或 cookie）
func (c *Context) GetJWT() string {
	// 先嘗試從 Authorization header 獲取
	if token := c.GetAuthToken(); token != "" {
		return token
	}

	// 嘗試從 cookie 獲取
	if token, err := c.Cookie("jwt"); err == nil && token != "" {
		return token
	}

	// 嘗試從查詢參數獲取（某些場景下使用）
	if token := c.Query("token"); token != "" {
		return token
	}

	return ""
}

// SetJWT 設置 JWT Token 到 cookie
func (c *Context) SetJWT(token string, maxAge int) {
	c.SetCookie("jwt", token, maxAge, "/", "", false, true)
}

// ClearJWT 清除 JWT Token
func (c *Context) ClearJWT() {
	c.SetCookie("jwt", "", -1, "/", "", false, true)
}

// GetOAuth2Token 獲取 OAuth2 Token
func (c *Context) GetOAuth2Token() string {
	// OAuth2 規範：Bearer token in Authorization header
	return c.GetAuthToken()
}

// RequireAuth 要求認證（輔助方法）
func (c *Context) RequireAuth() bool {
	if !c.IsAuthenticated() {
		c.AbortWithStatusJSON(401, map[string]string{
			"error": "authentication required",
		})
		return false
	}
	return true
}

// RequireRole 要求特定角色（輔助方法）
func (c *Context) RequireRole(role string) bool {
	if !c.HasRole(role) {
		c.AbortWithStatusJSON(403, map[string]string{
			"error": "insufficient permissions",
		})
		return false
	}
	return true
}

// HasRole 檢查是否有特定角色
func (c *Context) HasRole(role string) bool {
	if roles, exists := c.Get("roles"); exists {
		switch v := roles.(type) {
		case []string:
			for _, r := range v {
				if r == role {
					return true
				}
			}
		case string:
			return v == role
		}
	}
	return false
}

// SetRoles 設置用戶角色
func (c *Context) SetRoles(roles []string) {
	c.Set("roles", roles)
}

// GetRoles 獲取用戶角色
func (c *Context) GetRoles() []string {
	if roles, exists := c.Get("roles"); exists {
		if r, ok := roles.([]string); ok {
			return r
		}
	}
	return nil
}

// HasPermission 檢查是否有特定權限
func (c *Context) HasPermission(permission string) bool {
	if permissions, exists := c.Get("permissions"); exists {
		switch v := permissions.(type) {
		case []string:
			for _, p := range v {
				if p == permission {
					return true
				}
			}
		case map[string]bool:
			return v[permission]
		}
	}
	return false
}

// SetPermissions 設置用戶權限
func (c *Context) SetPermissions(permissions []string) {
	c.Set("permissions", permissions)
}

// GetPermissions 獲取用戶權限
func (c *Context) GetPermissions() []string {
	if permissions, exists := c.Get("permissions"); exists {
		if p, ok := permissions.([]string); ok {
			return p
		}
	}
	return nil
}

// SetTokenClaims 設置 Token Claims（JWT 等）
func (c *Context) SetTokenClaims(claims interface{}) {
	c.Set("token_claims", claims)
}

// GetTokenClaims 獲取 Token Claims
func (c *Context) GetTokenClaims() interface{} {
	claims, _ := c.Get("token_claims")
	return claims
}

// GetTokenClaim 獲取特定的 Token Claim
func (c *Context) GetTokenClaim(key string) interface{} {
	if claims, exists := c.Get("token_claims"); exists {
		if claimsMap, ok := claims.(map[string]interface{}); ok {
			return claimsMap[key]
		}
	}
	return nil
}

// IsGuest 檢查是否為訪客（未認證）
func (c *Context) IsGuest() bool {
	return !c.IsAuthenticated()
}

// GetClientCredentials 獲取客戶端憑證（用於 OAuth2 Client Credentials flow）
func (c *Context) GetClientCredentials() (clientID, clientSecret string, ok bool) {
	// 先嘗試從 Basic Auth 獲取
	if id, secret, hasBasic := c.BasicAuth(); hasBasic {
		return id, secret, true
	}

	// 嘗試從表單獲取
	clientID = c.PostForm("client_id")
	clientSecret = c.PostForm("client_secret")
	if clientID != "" && clientSecret != "" {
		return clientID, clientSecret, true
	}

	return "", "", false
}

// SetAuthError 設置認證錯誤
func (c *Context) SetAuthError(err string) {
	c.Set("auth_error", err)
	c.Header("WWW-Authenticate", `Bearer error="`+err+`"`)
}

// GetAuthError 獲取認證錯誤
func (c *Context) GetAuthError() string {
	return c.GetString("auth_error")
}
