package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteError_ContentType(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusBadRequest, "bad_request", "something went wrong")

	got := w.Header().Get("Content-Type")
	if got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
}

func TestWriteError_StatusCode(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusNotFound, "not_found", "resource not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestWriteError_Body(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusForbidden, "forbidden", "access denied")

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp.Error != "access denied" {
		t.Errorf("Error = %q, want %q", resp.Error, "access denied")
	}
	if resp.Code != "forbidden" {
		t.Errorf("Code = %q, want %q", resp.Code, "forbidden")
	}
}

func TestWriteError_CodeOmittedWhenEmpty(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusInternalServerError, "", "internal error")

	var raw map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if _, exists := raw["code"]; exists {
		t.Errorf("expected code field to be omitted when empty, but it was present: %v", raw["code"])
	}
	if raw["error"] != "internal error" {
		t.Errorf("error = %q, want %q", raw["error"], "internal error")
	}
}

func TestWriteJSON_ContentType(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, map[string]string{"key": "value"})

	got := w.Header().Get("Content-Type")
	if got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
}

func TestWriteJSON_CacheControl(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, map[string]string{"key": "value"})

	got := w.Header().Get("Cache-Control")
	if got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestWriteJSON_StatusCode(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusCreated, map[string]string{"id": "123"})

	if w.Code != http.StatusCreated {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestWriteJSON_BodyMap(t *testing.T) {
	w := httptest.NewRecorder()
	input := map[string]string{"name": "tardi", "status": "running"}
	WriteJSON(w, http.StatusOK, input)

	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if got["name"] != "tardi" {
		t.Errorf("name = %q, want %q", got["name"], "tardi")
	}
	if got["status"] != "running" {
		t.Errorf("status = %q, want %q", got["status"], "running")
	}
}

func TestWriteJSON_BodyStruct(t *testing.T) {
	type Agent struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	w := httptest.NewRecorder()
	input := Agent{ID: "a-1", Name: "my-agent"}
	WriteJSON(w, http.StatusOK, input)

	var got Agent
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if got.ID != "a-1" {
		t.Errorf("ID = %q, want %q", got.ID, "a-1")
	}
	if got.Name != "my-agent" {
		t.Errorf("Name = %q, want %q", got.Name, "my-agent")
	}
}
