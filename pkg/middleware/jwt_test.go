package middleware

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

var testSecret = []byte("test-secret-at-least-32-bytes-long!!")

func TestHS256RoundTrip(t *testing.T) {
	token, err := SignHS256(JWTClaims{
		Subject: "42",
		Issuer:  "hypgo",
		Roles:   []string{"admin"},
		Data:    map[string]interface{}{"email": "a@b.c"},
	}, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("SignHS256: %v", err)
	}
	if strings.Count(token, ".") != 2 {
		t.Fatalf("token should have 3 segments: %q", token)
	}

	claims, err := VerifyHS256(token, testSecret)
	if err != nil {
		t.Fatalf("VerifyHS256: %v", err)
	}
	if claims.Subject != "42" || claims.Issuer != "hypgo" || claims.Roles[0] != "admin" || claims.Data["email"] != "a@b.c" {
		t.Errorf("claims mismatch: %+v", claims)
	}
	if claims.IssuedAt == 0 || claims.ExpiresAt <= claims.IssuedAt {
		t.Errorf("iat/exp should be auto-filled: iat=%d exp=%d", claims.IssuedAt, claims.ExpiresAt)
	}
}

func TestHS256RejectsTampering(t *testing.T) {
	token, _ := SignHS256(JWTClaims{Subject: "42"}, testSecret, time.Hour)
	parts := strings.Split(token, ".")

	// 竄改 payload（把 sub 換成 1）
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"1"}`))
	if _, err := VerifyHS256(parts[0]+"."+forged+"."+parts[2], testSecret); !errors.Is(err, ErrJWTSignature) {
		t.Errorf("tampered payload: got %v, want ErrJWTSignature", err)
	}

	// 錯的 secret
	if _, err := VerifyHS256(token, []byte("wrong")); !errors.Is(err, ErrJWTSignature) {
		t.Errorf("wrong secret: got %v, want ErrJWTSignature", err)
	}

	// alg=none 攻擊：header 改為 none、簽章留空
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	if _, err := VerifyHS256(noneHeader+"."+parts[1]+".", testSecret); !errors.Is(err, ErrJWTAlgorithm) {
		t.Errorf("alg=none: got %v, want ErrJWTAlgorithm", err)
	}

	// 格式錯誤
	for _, bad := range []string{"", "a.b", "a.b.c.d", "!!!.!!!.!!!"} {
		if _, err := VerifyHS256(bad, testSecret); !errors.Is(err, ErrJWTMalformed) {
			t.Errorf("malformed %q: got %v, want ErrJWTMalformed", bad, err)
		}
	}

	// 空 secret 兩端都拒絕
	if _, err := SignHS256(JWTClaims{}, nil, 0); !errors.Is(err, ErrJWTNoSecret) {
		t.Errorf("sign empty secret: got %v", err)
	}
	if _, err := VerifyHS256(token, nil); !errors.Is(err, ErrJWTNoSecret) {
		t.Errorf("verify empty secret: got %v", err)
	}
}

func TestHS256Expiry(t *testing.T) {
	expired, _ := SignHS256(JWTClaims{Subject: "42", ExpiresAt: time.Now().Add(-time.Minute).Unix()}, testSecret, 0)
	if _, err := VerifyHS256(expired, testSecret); !errors.Is(err, ErrJWTExpired) {
		t.Errorf("expired: got %v, want ErrJWTExpired", err)
	}

	// ttl=0 且未設 exp → 不過期
	forever, _ := SignHS256(JWTClaims{Subject: "42"}, testSecret, 0)
	claims, err := VerifyHS256(forever, testSecret)
	if err != nil || claims.ExpiresAt != 0 {
		t.Errorf("no-expiry token: err=%v exp=%d", err, claims.ExpiresAt)
	}
}

// TestRequireRole 端到端：JWT 之後掛 RequireRole，依 claims.Roles 放行／403，
// 未經 JWT 中間件 → 401，自訂 ErrorHandler 收到可判別的 error
func TestRequireRole(t *testing.T) {
	r := router.New()
	api := r.NewGroup("/api", JWT(JWTConfig{Validator: HS256Validator(testSecret)}))
	admin := api.NewGroup("/admin", RequireRole("admin", "root"))
	admin.GET("/ping", func(c *context.Context) { c.String(200, "pong") })

	// 未掛 JWT 直接用 RequireRole → 401
	var gotErr error
	r.GET("/bare", RequireRoleWith(RequireRoleConfig{
		Roles:        []string{"admin"},
		ErrorHandler: func(c *context.Context, err error) { gotErr = err; c.AbortWithStatus(418) },
	}), func(c *context.Context) { c.String(200, "never") })

	do := func(path, token string) int {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		r.ServeHTTP(w, req)
		return w.Code
	}

	adminTok, _ := SignHS256(JWTClaims{Subject: "1", Roles: []string{"admin"}}, testSecret, time.Hour)
	rootTok, _ := SignHS256(JWTClaims{Subject: "2", Roles: []string{"root"}}, testSecret, time.Hour)
	userTok, _ := SignHS256(JWTClaims{Subject: "3", Roles: []string{"user"}}, testSecret, time.Hour)
	noRoleTok, _ := SignHS256(JWTClaims{Subject: "4"}, testSecret, time.Hour)

	if code := do("/api/admin/ping", adminTok); code != 200 {
		t.Errorf("admin: %d, want 200", code)
	}
	if code := do("/api/admin/ping", rootTok); code != 200 {
		t.Errorf("root (any-of): %d, want 200", code)
	}
	if code := do("/api/admin/ping", userTok); code != 403 {
		t.Errorf("user: %d, want 403", code)
	}
	if code := do("/api/admin/ping", noRoleTok); code != 403 {
		t.Errorf("no roles: %d, want 403", code)
	}
	if code := do("/api/admin/ping", ""); code != 401 {
		t.Errorf("no token (JWT layer): %d, want 401", code)
	}
	if code := do("/bare", adminTok); code != 418 || !errors.Is(gotErr, ErrRoleUnauthenticated) {
		t.Errorf("without JWT middleware: code %d err %v, want 418 + ErrRoleUnauthenticated", code, gotErr)
	}
}

// TestJWTMiddlewareWithHS256Validator 端到端：Group 掛 JWT 中間件，
// 無 token → 401；合法 token → handler 可用 JWTClaimsFrom 取回 claims
func TestJWTMiddlewareWithHS256Validator(t *testing.T) {
	r := router.New()
	api := r.NewGroup("/api", JWT(JWTConfig{Validator: HS256Validator(testSecret)}))
	api.GET("/me", func(c *context.Context) {
		claims, ok := JWTClaimsFrom(c, "")
		if !ok {
			c.String(500, "no claims")
			return
		}
		c.String(200, claims.Subject)
	})

	// 無 token
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/me", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", w.Code)
	}

	// 合法 token
	token, _ := SignHS256(JWTClaims{Subject: "42"}, testSecret, time.Hour)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "42" {
		t.Errorf("valid token: status = %d body = %q, want 200 \"42\"", w.Code, w.Body.String())
	}

	// 錯的 secret 簽的 token
	bad, _ := SignHS256(JWTClaims{Subject: "42"}, []byte("other-secret"), time.Hour)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+bad)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("bad signature: status = %d, want 401", w.Code)
	}
}
