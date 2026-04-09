package interceptor

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	// MaxRequests 時間窗口內的最大請求數
	MaxRequests int

	// Window 時間窗口
	Window time.Duration

	// CleanupInterval 清理過期 key 的間隔（防止記憶體洩漏）
	CleanupInterval time.Duration
}

type rateLimitEntry struct {
	count     int
	windowEnd time.Time
}

// RateLimit 根據客戶端 IP 限流
func RateLimit(cfg RateLimitConfig) grpc.UnaryServerInterceptor {
	if cfg.MaxRequests <= 0 {
		cfg.MaxRequests = 100
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}

	var mu sync.Mutex
	clients := make(map[string]*rateLimitEntry)

	// 背景清理過期 key
	go func() {
		ticker := time.NewTicker(cfg.CleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			mu.Lock()
			for k, v := range clients {
				if now.After(v.windowEnd) {
					delete(clients, k)
				}
			}
			mu.Unlock()
		}
	}()

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 取得客戶端 IP
		clientIP := "unknown"
		if p, ok := peer.FromContext(ctx); ok {
			clientIP = p.Addr.String()
		}

		mu.Lock()
		entry, ok := clients[clientIP]
		now := time.Now()

		if !ok || now.After(entry.windowEnd) {
			// 新窗口
			clients[clientIP] = &rateLimitEntry{
				count:     1,
				windowEnd: now.Add(cfg.Window),
			}
			mu.Unlock()
			return handler(ctx, req)
		}

		entry.count++
		if entry.count > cfg.MaxRequests {
			mu.Unlock()
			return nil, status.Errorf(codes.ResourceExhausted,
				"rate limit exceeded: %d requests per %s", cfg.MaxRequests, cfg.Window)
		}
		mu.Unlock()

		return handler(ctx, req)
	}
}
