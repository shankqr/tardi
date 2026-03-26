package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		code       string
		message    string
		wantStatus int
	}{
		{
			name:       "bad request",
			status:     http.StatusBadRequest,
			code:       "bad_request",
			message:    "invalid input",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unauthorized",
			status:     http.StatusUnauthorized,
			code:       "unauthorized",
			message:    "missing token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "internal error",
			status:     http.StatusInternalServerError,
			code:       "internal_error",
			message:    "something broke",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "empty code",
			status:     http.StatusBadRequest,
			code:       "",
			message:    "no code",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteError(w, tt.status, tt.code, tt.message)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			var resp ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			if resp.Error != tt.message {
				t.Errorf("error = %q, want %q", resp.Error, tt.message)
			}
			if resp.Code != tt.code {
				t.Errorf("code = %q, want %q", resp.Code, tt.code)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	t.Run("map data", func(t *testing.T) {
		w := httptest.NewRecorder()
		data := map[string]string{"status": "ok"}
		WriteJSON(w, http.StatusOK, data)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}

		ct := w.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}

		cc := w.Header().Get("Cache-Control")
		if cc != "no-store" {
			t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
		}

		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if resp["status"] != "ok" {
			t.Errorf("status = %q, want %q", resp["status"], "ok")
		}
	})

	t.Run("struct data", func(t *testing.T) {
		w := httptest.NewRecorder()
		data := struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}{Name: "test", Count: 42}

		WriteJSON(w, http.StatusCreated, data)

		if w.Code != http.StatusCreated {
			t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if resp["name"] != "test" {
			t.Errorf("name = %v, want %q", resp["name"], "test")
		}
		if resp["count"] != float64(42) {
			t.Errorf("count = %v, want %v", resp["count"], 42)
		}
	})

	t.Run("nil data", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteJSON(w, http.StatusOK, nil)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}

		body := w.Body.String()
		// json.Encode(nil) produces "null\n"
		if body != "null\n" {
			t.Errorf("body = %q, want %q", body, "null\n")
		}
	})
}
