package middleware

import (
	"golang.org/x/time/rate"
	"net/http"
)

// RateLimiter 請求限流
func RateLimiter(rps int) func(http.Handler) http.Handler {
	limiter := rate.NewLimiter(rate.Limit(rps), rps)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
