package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

// AgentConfigHandler returns the agent configuration for a VPS instance.
// Authenticated by agent token (not Firebase JWT).
func AgentConfigHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractAgentToken(r)
		if token == "" {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing agent token")
			return
		}

		inst, err := db.GetInstanceByAgentToken(r.Context(), deps.Pool, token)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid agent token")
			return
		}

		config, err := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, inst.ID)
		if err != nil {
			slog.Error("agent config: get config", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get config")
			return
		}

		if config == nil {
			WriteJSON(w, http.StatusOK, map[string]any{"config": map[string]any{}, "version": 0})
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"config":  config.Config,
			"version": config.Version,
		})
	}
}

// AgentHeartbeatHandler records a heartbeat from an agent.
func AgentHeartbeatHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractAgentToken(r)
		if token == "" {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing agent token")
			return
		}

		inst, err := db.GetInstanceByAgentToken(r.Context(), deps.Pool, token)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid agent token")
			return
		}

		// Parse optional metrics from body
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		if err := db.UpdateInstanceHeartbeat(r.Context(), deps.Pool, inst.ID); err != nil {
			slog.Error("agent heartbeat: update", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to record heartbeat")
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// UpdateAgentConfigHandler allows a user to update the agent config for their instance.
// Authenticated by Firebase JWT (user-facing endpoint).
func UpdateAgentConfigHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			slog.Error("update agent config: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		var body struct {
			Config map[string]any `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid request body")
			return
		}
		if body.Config == nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "config is required")
			return
		}

		ac := &models.AgentConfig{
			ID:            uuid.New(),
			VpsInstanceID: inst.ID,
			Config:        body.Config,
			Version:       1,
		}
		if err := db.CreateAgentConfig(r.Context(), deps.Pool, ac); err != nil {
			slog.Error("update agent config: save", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to save config")
			return
		}

		// Read back the saved config to get the actual version
		saved, err := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, inst.ID)
		if err != nil {
			slog.Error("update agent config: read back", "error", err)
			WriteJSON(w, http.StatusOK, map[string]any{"config": body.Config, "version": 1})
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"config":  saved.Config,
			"version": saved.Version,
		})
	}
}

func extractAgentToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
