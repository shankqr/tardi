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

		// Parse optional status from body
		var body struct {
			Status string `json:"status"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		var agentStatus *string
		if body.Status != "" {
			agentStatus = &body.Status
		}

		if err := db.UpdateInstanceHeartbeat(r.Context(), deps.Pool, inst.ID, agentStatus); err != nil {
			slog.Error("agent heartbeat: update", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to record heartbeat")
			return
		}

		// Return current config version so agent can detect changes
		var configVersion int
		config, err := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, inst.ID)
		if err == nil && config != nil {
			configVersion = config.Version
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"config_version": configVersion,
		})
	}
}

// GetAgentConfigHandler returns the agent configuration for a user's instance.
// Authenticated by Firebase JWT. API keys are masked for display.
func GetAgentConfigHandler(deps Dependencies) http.HandlerFunc {
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

		_, err = db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			slog.Error("get agent config: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		config, err := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, instanceID)
		if err != nil {
			slog.Error("get agent config: get config", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get config")
			return
		}

		if config == nil {
			WriteJSON(w, http.StatusOK, map[string]any{"config": map[string]any{}, "version": 0})
			return
		}

		// Mask API keys for display
		masked := make(map[string]any, len(config.Config))
		for k, v := range config.Config {
			masked[k] = v
		}
		for _, keyField := range []string{"openrouter_api_key", "anthropic_api_key", "openai_api_key"} {
			if v, ok := masked[keyField].(string); ok && len(v) > 4 {
				masked[keyField] = v[:3] + "..." + v[len(v)-4:]
			}
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"config":  masked,
			"version": config.Version,
		})
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

		// Preserve existing API keys when frontend sends null (unchanged)
		existing, _ := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, inst.ID)
		for _, keyField := range []string{"openrouter_api_key", "anthropic_api_key", "openai_api_key"} {
			if body.Config[keyField] == nil && existing != nil {
				if v, ok := existing.Config[keyField].(string); ok && v != "" {
					body.Config[keyField] = v
				}
			}
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
