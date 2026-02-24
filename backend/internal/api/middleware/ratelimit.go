package middleware

import (
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

func RateLimit(requestsPerMinute int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	limiters := make(map[string]*rate.Limiter)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr

			mu.Lock()
			limiter, exists := limiters[ip]
			if !exists {
				limiter = rate.NewLimiter(rate.Limit(requestsPerMinute)/60, requestsPerMinute)
				limiters[ip] = limiter
			}
			mu.Unlock()

			if !limiter.Allow() {
				http.Error(w, `{"error":"rate limit exceeded","code":"rate_limited"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
