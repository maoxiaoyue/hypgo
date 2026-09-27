package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// JWT（HS256）簽發／驗證。
//
// JWT 中間件本身只負責抽出 token 並交給 JWTConfig.Validator；這裡提供
// 純標準庫的 HS256 實作，讓 middleware.JWT 不需外部 JWT 函式庫即可開箱使用：
//
//	token, _ := middleware.SignHS256(middleware.JWTClaims{Subject: "42"}, secret, 24*time.Hour)
//	r.NewGroup("/api", middleware.JWT(middleware.JWTConfig{Validator: middleware.HS256Validator(secret)}))
//
// 只支援 HS256（對稱金鑰）。需要 RS256/ES256 或 JWKS 時，自行提供 Validator 即可。

// JWTClaims HS256 token 的宣告：標準欄位子集 + 自訂資料
type JWTClaims struct {
	Subject   string                 `json:"sub,omitempty"`   // 使用者識別
	Issuer    string                 `json:"iss,omitempty"`   // 簽發者
	IssuedAt  int64                  `json:"iat,omitempty"`   // 簽發時間（Unix 秒），SignHS256 自動填
	ExpiresAt int64                  `json:"exp,omitempty"`   // 到期時間（Unix 秒），0 = 不過期
	Roles     []string               `json:"roles,omitempty"` // 角色清單
	Data      map[string]interface{} `json:"data,omitempty"`  // 自訂欄位
}

// JWT 驗證錯誤（可用 errors.Is 判別）
var (
	ErrJWTMalformed = errors.New("jwt: malformed token")
	ErrJWTAlgorithm = errors.New("jwt: unsupported algorithm (only HS256)")
	ErrJWTSignature = errors.New("jwt: invalid signature")
	ErrJWTExpired   = errors.New("jwt: token expired")
	ErrJWTNoSecret  = errors.New("jwt: empty secret")
)

// hs256Header 固定 header；VerifyHS256 只接受 alg=HS256，杜絕 alg=none / 演算法混淆攻擊
const hs256Header = `{"alg":"HS256","typ":"JWT"}`

// SignHS256 以 HS256 簽發 token。ttl > 0 且 claims.ExpiresAt 未設時自動計算到期；
// IssuedAt 未設時自動填現在
func SignHS256(claims JWTClaims, secret []byte, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", ErrJWTNoSecret
	}
	now := time.Now()
	if claims.IssuedAt == 0 {
		claims.IssuedAt = now.Unix()
	}
	if ttl > 0 && claims.ExpiresAt == 0 {
		claims.ExpiresAt = now.Add(ttl).Unix()
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString([]byte(hs256Header)) + "." + enc.EncodeToString(payload)
	return signingInput + "." + enc.EncodeToString(hs256Sum(signingInput, secret)), nil
}

// VerifyHS256 驗證簽章與到期時間並回傳 claims
func VerifyHS256(token string, secret []byte) (*JWTClaims, error) {
	if len(secret) == 0 {
		return nil, ErrJWTNoSecret
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrJWTMalformed
	}
	enc := base64.RawURLEncoding

	headerJSON, err := enc.DecodeString(parts[0])
	if err != nil {
		return nil, ErrJWTMalformed
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, ErrJWTMalformed
	}
	if header.Alg != "HS256" {
		return nil, ErrJWTAlgorithm
	}

	// 先驗簽再解 payload：未通過簽章的內容一律不信任
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return nil, ErrJWTMalformed
	}
	if !hmac.Equal(sig, hs256Sum(parts[0]+"."+parts[1], secret)) {
		return nil, ErrJWTSignature
	}

	payload, err := enc.DecodeString(parts[1])
	if err != nil {
		return nil, ErrJWTMalformed
	}
	var claims JWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrJWTMalformed
	}
	if claims.ExpiresAt != 0 && time.Now().Unix() >= claims.ExpiresAt {
		return nil, ErrJWTExpired
	}
	return &claims, nil
}

// HS256Validator 回傳可直接填入 JWTConfig.Validator 的驗證函式；
// 驗證成功時 context 內（JWTConfig.ContextKey，預設 "user"）存的是 *JWTClaims
func HS256Validator(secret []byte) func(token string) (interface{}, error) {
	return func(token string) (interface{}, error) {
		return VerifyHS256(token, secret)
	}
}

// JWTClaimsFrom 從 context 取出 JWT 中間件存入的 *JWTClaims。
// contextKey 為空時用預設的 "user"；未經 JWT 中間件或 Validator 非 HS256Validator 時回 false
func JWTClaimsFrom(c *hypcontext.Context, contextKey string) (*JWTClaims, bool) {
	if contextKey == "" {
		contextKey = "user"
	}
	v, ok := c.Get(contextKey)
	if !ok {
		return nil, false
	}
	claims, ok := v.(*JWTClaims)
	return claims, ok
}

// ===== 角色檢查 =====

// RequireRole 錯誤（可用 errors.Is 判別；預設回 401 / 403）
var (
	ErrRoleUnauthenticated = errors.New("role: not authenticated")
	ErrRoleForbidden       = errors.New("role: insufficient permissions")
)

// RequireRoleConfig 角色檢查設定
type RequireRoleConfig struct {
	// Roles 允許的角色，符合任一即放行（OR）
	Roles []string
	// ErrorHandler 自訂錯誤回應；nil 時未認證回 401、角色不符回 403（無 body）。
	// err 為 ErrRoleUnauthenticated 或 ErrRoleForbidden
	ErrorHandler func(c *hypcontext.Context, err error)
}

// RequireRole 要求請求者具備任一指定角色。
// 角色來源為 c.GetRoles()：JWT 中間件搭配 HS256Validator 時會自動由 claims.Roles 填入，
// 自訂 Validator 則需自行 c.SetRoles。必須掛在 JWT 中間件之後：
//
//	admin := api.NewGroup("/admin", middleware.RequireRole("admin"))
func RequireRole(roles ...string) hypcontext.HandlerFunc {
	return RequireRoleWith(RequireRoleConfig{Roles: roles})
}

// RequireRoleWith 帶設定的 RequireRole（可自訂錯誤回應）
func RequireRoleWith(config RequireRoleConfig) hypcontext.HandlerFunc {
	fail := func(c *hypcontext.Context, err error) {
		if config.ErrorHandler != nil {
			config.ErrorHandler(c, err)
			return
		}
		if errors.Is(err, ErrRoleUnauthenticated) {
			c.AbortWithStatus(401)
			return
		}
		c.AbortWithStatus(403)
	}

	return func(c *hypcontext.Context) {
		// 未經 JWT 中間件（或 Validator 未 SetRoles）→ 視為未認證
		if _, ok := c.Get("roles"); !ok {
			fail(c, ErrRoleUnauthenticated)
			return
		}
		for _, role := range config.Roles {
			if c.HasRole(role) {
				c.Next()
				return
			}
		}
		fail(c, ErrRoleForbidden)
	}
}

func hs256Sum(signingInput string, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}
