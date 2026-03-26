package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminTokenAuth(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})

	t.Run("valid token", func(t *testing.T) {
		mw := adminTokenAuth("secret-admin-token")
		handler := mw(dummyHandler)

		r := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
		r.Header.Set("X-Admin-Token", "secret-admin-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		mw := adminTokenAuth("secret-admin-token")
		handler := mw(dummyHandler)

		r := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
		r.Header.Set("X-Admin-Token", "wrong-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}

		var resp ErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if resp.Code != "unauthorized" {
			t.Errorf("code = %q, want %q", resp.Code, "unauthorized")
		}
	})

	t.Run("empty token header", func(t *testing.T) {
		mw := adminTokenAuth("secret-admin-token")
		handler := mw(dummyHandler)

		r := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("admin API not configured (empty server token)", func(t *testing.T) {
		mw := adminTokenAuth("")
		handler := mw(dummyHandler)

		r := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
		r.Header.Set("X-Admin-Token", "any-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}

		var resp ErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if resp.Code != "forbidden" {
			t.Errorf("code = %q, want %q", resp.Code, "forbidden")
		}
	})
}
