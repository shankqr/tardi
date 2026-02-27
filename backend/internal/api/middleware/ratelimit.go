package middleware

import (
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// RateLimit applies a per-IP rate limit with the given requests per minute.
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
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"rate limit exceeded","code":"rate_limited"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitProvisioning applies a stricter 10 req/min limit for provisioning actions.
func RateLimitProvisioning() func(http.Handler) http.Handler {
	var mu sync.Mutex
	limiters := make(map[string]*rate.Limiter)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only apply to POST/DELETE on instance endpoints
			if !isProvisioningAction(r) {
				next.ServeHTTP(w, r)
				return
			}

			ip := r.RemoteAddr

			mu.Lock()
			limiter, exists := limiters[ip]
			if !exists {
				// 10 requests per minute for provisioning actions
				limiter = rate.NewLimiter(rate.Limit(10.0/60.0), 10)
				limiters[ip] = limiter
			}
			mu.Unlock()

			if !limiter.Allow() {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"rate limit exceeded for provisioning actions","code":"rate_limited"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isProvisioningAction(r *http.Request) bool {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/instances") && (r.Method == http.MethodPost || r.Method == http.MethodDelete) {
		return true
	}
	return false
}
