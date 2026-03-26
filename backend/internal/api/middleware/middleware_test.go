package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/shanq/tardi/internal/models"
)

// ---------------------------------------------------------------------------
// extractBearerToken
// ---------------------------------------------------------------------------

func TestExtractBearerToken_Valid(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer my-secret-token")

	got := extractBearerToken(r)
	if got != "my-secret-token" {
		t.Errorf("extractBearerToken = %q, want %q", got, "my-secret-token")
	}
}

func TestExtractBearerToken_NoHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	got := extractBearerToken(r)
	if got != "" {
		t.Errorf("extractBearerToken = %q, want empty string", got)
	}
}

func TestExtractBearerToken_WrongScheme(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Basic abc123")

	got := extractBearerToken(r)
	if got != "" {
		t.Errorf("extractBearerToken = %q, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// UserFromContext
// ---------------------------------------------------------------------------

func TestUserFromContext_WithUser(t *testing.T) {
	user := &models.User{
		ID:          uuid.New(),
		FirebaseUID: "firebase-123",
		Email:       "test@example.com",
	}

	ctx := context.WithValue(context.Background(), UserKey, user)
	got := UserFromContext(ctx)
	if got == nil {
		t.Fatal("UserFromContext returned nil, want user")
	}
	if got.ID != user.ID {
		t.Errorf("UserFromContext().ID = %v, want %v", got.ID, user.ID)
	}
	if got.Email != user.Email {
		t.Errorf("UserFromContext().Email = %q, want %q", got.Email, user.Email)
	}
}

func TestUserFromContext_NoUser(t *testing.T) {
	ctx := context.Background()
	got := UserFromContext(ctx)
	if got != nil {
		t.Errorf("UserFromContext = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// RateLimit
// ---------------------------------------------------------------------------

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	mw := RateLimit(60) // 60 req/min → burst of 60
	handler := mw(okHandler())

	for i := 0; i < 5; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		r.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: got status %d, want 200", i, w.Code)
		}
	}
}

func TestRateLimit_Returns429WhenExceeded(t *testing.T) {
	mw := RateLimit(5) // 5 req/min → burst of 5
	handler := mw(okHandler())

	var got429 bool
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		r.RemoteAddr = "10.0.0.2:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("expected 429 after exceeding rate limit, but all requests succeeded")
	}
}

func TestRateLimit_ExemptsHealthEndpoints(t *testing.T) {
	mw := RateLimit(1) // very low limit
	handler := mw(okHandler())

	// Exhaust the limiter with a normal request.
	for i := 0; i < 10; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		r.RemoteAddr = "10.0.0.3:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	// Health endpoints should still work.
	for _, path := range []string{"/healthz", "/readyz"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "10.0.0.3:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("%s: got status %d, want 200", path, w.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// RateLimitProvisioning
// ---------------------------------------------------------------------------

func TestRateLimitProvisioning_AppliesToPostInstances(t *testing.T) {
	mw := RateLimitProvisioning()
	handler := mw(okHandler())

	var got429 bool
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/instances", nil)
		r.RemoteAddr = "10.0.0.4:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("expected 429 for POST /api/instances after exceeding limit")
	}
}

func TestRateLimitProvisioning_SkipsGetInstances(t *testing.T) {
	mw := RateLimitProvisioning()
	handler := mw(okHandler())

	// Even many GET requests should all pass through (not a provisioning action).
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
		r.RemoteAddr = "10.0.0.5:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("GET /api/instances request %d: got status %d, want 200", i, w.Code)
		}
	}
}

func TestRateLimitProvisioning_SkipsNonInstancePaths(t *testing.T) {
	mw := RateLimitProvisioning()
	handler := mw(okHandler())

	for i := 0; i < 20; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/dashboard/state", nil)
		r.RemoteAddr = "10.0.0.6:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("POST /api/dashboard/state request %d: got status %d, want 200", i, w.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// clientIP
// ---------------------------------------------------------------------------

func TestClientIP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1, 10.0.0.2")
	r.RemoteAddr = "127.0.0.1:9999"

	got := clientIP(r)
	if got != "203.0.113.1" {
		t.Errorf("clientIP = %q, want %q", got, "203.0.113.1")
	}
}

func TestClientIP_FallsBackToRemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1:54321"

	got := clientIP(r)
	if got != "192.168.1.1" {
		t.Errorf("clientIP = %q, want %q", got, "192.168.1.1")
	}
}

func TestClientIP_RemoteAddrWithoutPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1"

	got := clientIP(r)
	if got != "192.168.1.1" {
		t.Errorf("clientIP = %q, want %q", got, "192.168.1.1")
	}
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

func TestCORS_ReturnsMiddleware(t *testing.T) {
	mw := CORS([]string{"https://example.com"})
	if mw == nil {
		t.Fatal("CORS returned nil")
	}

	handler := mw(okHandler())
	if handler == nil {
		t.Fatal("CORS middleware returned nil handler")
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("CORS-wrapped handler: got status %d, want 200", w.Code)
	}
}
