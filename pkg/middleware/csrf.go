package middleware

import (
	"net/http"
)

func CSRFProtection() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
				token := r.Header.Get("X-CSRF-Token")
				if token == "" {
					http.Error(w, "CSRF token missing", http.StatusForbidden)
					return
				}
				// 驗證 token
			}
			next.ServeHTTP(w, r)
		})
	}
}
