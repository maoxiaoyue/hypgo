// Package diagnostic 提供結構化的系統診斷端點
// 讓 AI 一個請求就能取得完整的應用程式狀態快照
//
// 安全考量：
//   - 必須搭配認證中間件使用（Auth 參數必填）
//   - Redact 模式預設開啟（遮蔽 DSN 密碼）
//   - 內建 rate limiting（每分鐘最多 10 次）
package diagnostic

import (
	"sync"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// Config 配置診斷端點
type Config struct {
	// Path 端點路徑（預設 /_debug/state）
	Path string

	// Auth 認證中間件（必填，不設則 panic）
	Auth hypcontext.HandlerFunc

	// Redact 是否遮蔽敏感資訊（預設 true）
	Redact bool

	// MaxRequestsPerMinute 每分鐘最大請求數（預設 10）
	MaxRequestsPerMinute int
}

// rateLimiter 簡易計數器式限流
type rateLimiter struct {
	mu       sync.Mutex
	count    int
	limit    int
	resetAt  time.Time
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{
		limit:   limit,
		resetAt: time.Now().Add(time.Minute),
	}
}

func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if time.Now().After(rl.resetAt) {
		rl.count = 0
		rl.resetAt = time.Now().Add(time.Minute)
	}

	if rl.count >= rl.limit {
		return false
	}
	rl.count++
	return true
}

// Register 在路由器上註冊診斷端點
//
// 使用範例：
//
//	diagnostic.Register(router, diagnostic.Config{
//	    Auth:   authMiddleware,
//	    Redact: true,
//	})
func Register(r *router.Router, cfg Config) {
	if cfg.Auth == nil {
		panic("diagnostic: Auth middleware is required (security)")
	}
	if cfg.Path == "" {
		cfg.Path = "/_debug/state"
	}
	if cfg.MaxRequestsPerMinute <= 0 {
		cfg.MaxRequestsPerMinute = 10
	}

	rl := newRateLimiter(cfg.MaxRequestsPerMinute)

	handler := func(c *hypcontext.Context) {
		// Rate limiting
		if !rl.allow() {
			c.AbortWithStatusJSON(429, map[string]string{
				"error": "too many diagnostic requests",
			})
			return
		}

		// 安全標頭
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("Cache-Control", "no-store")

		// 收集診斷資訊
		state := Collect(r, nil, cfg.Redact)
		c.JSON(200, state)
	}

	// 註冊時加上認證中間件
	r.GET(cfg.Path, cfg.Auth, handler)
}

// RegisterWithConfig 同 Register 但額外接收 config（用於包含伺服器資訊）
func RegisterWithConfig(r *router.Router, appCfg *config.Config, cfg Config) {
	if cfg.Auth == nil {
		panic("diagnostic: Auth middleware is required (security)")
	}
	if cfg.Path == "" {
		cfg.Path = "/_debug/state"
	}
	if cfg.MaxRequestsPerMinute <= 0 {
		cfg.MaxRequestsPerMinute = 10
	}

	rl := newRateLimiter(cfg.MaxRequestsPerMinute)

	handler := func(c *hypcontext.Context) {
		if !rl.allow() {
			c.AbortWithStatusJSON(429, map[string]string{
				"error": "too many diagnostic requests",
			})
			return
		}

		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("Cache-Control", "no-store")

		state := Collect(r, appCfg, cfg.Redact)
		c.JSON(200, state)
	}

	r.GET(cfg.Path, cfg.Auth, handler)
}
