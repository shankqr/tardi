package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/shanq/tardi/internal/db"
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

func extractAgentToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
