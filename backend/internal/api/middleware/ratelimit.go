package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// clientIP extracts the real client IP from X-Forwarded-For (set by Cloud Run,
// load balancers, and reverse proxies), falling back to r.RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For is "client, proxy1, proxy2" — first entry is the client.
		if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
			return ip
		}
	}
	// Strip port from RemoteAddr (e.g. "1.2.3.4:54321" → "1.2.3.4").
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RateLimit applies a per-IP rate limit with the given requests per minute.
// Health endpoints (/healthz, /readyz) are exempt.
func RateLimit(requestsPerMinute int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	limiters := make(map[string]*rate.Limiter)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health endpoints must never be rate-limited (Cloud Run probes, monitoring).
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}

			ip := clientIP(r)

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

			ip := clientIP(r)

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
