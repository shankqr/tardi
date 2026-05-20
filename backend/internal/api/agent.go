package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/crypto"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/scripts"
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

		// Inject Google OAuth credentials if available
		respConfig := make(map[string]any, len(config.Config))
		for k, v := range config.Config {
			respConfig[k] = v
		}
		googleCreds := buildGoogleCredentials(r.Context(), deps, inst.ID)
		if googleCreds != nil {
			for k, v := range googleCreds {
				respConfig[k] = v
			}
		}

		// Include the full model catalog so sync scripts can register all
		// models in OpenClaw (the OC dashboard dropdown only shows models
		// that have been explicitly added via `openclaw models set`).
		var modelIDs []string
		if allModels, err := db.ListEnabledModels(r.Context(), deps.Pool); err == nil {
			for _, m := range allModels {
				modelIDs = append(modelIDs, m.ID)
			}
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"config":    respConfig,
			"version":   config.Version,
			"model_ids": modelIDs,
		})
	}
}

// buildGoogleCredentials returns the base64-encoded credential fields for gog CLI,
// or nil if no valid Google OAuth token exists.
func buildGoogleCredentials(ctx context.Context, deps Dependencies, instanceID uuid.UUID) map[string]any {
	if deps.Config.TokenEncryptionKey == "" {
		return nil
	}

	gToken, err := db.GetGoogleOAuthTokenByInstanceID(ctx, deps.Pool, instanceID)
	if err != nil {
		return nil
	}

	encKey, err := crypto.ParseKey(deps.Config.TokenEncryptionKey)
	if err != nil {
		slog.Error("google creds: invalid encryption key", "error", err)
		return nil
	}

	accessToken, err := crypto.Decrypt(gToken.AccessTokenEnc, encKey)
	if err != nil {
		slog.Error("google creds: decrypt access token", "error", err)
		return nil
	}

	refreshToken, err := crypto.Decrypt(gToken.RefreshTokenEnc, encKey)
	if err != nil {
		slog.Error("google creds: decrypt refresh token", "error", err)
		return nil
	}

	// Build OAuth client credentials JSON (for gog auth credentials)
	clientJSON := fmt.Sprintf(`{"installed":{"client_id":%q,"client_secret":%q,"auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token"}}`,
		deps.Config.GoogleOAuthClientID, deps.Config.GoogleOAuthClientSecret)

	// Build user token JSON (for gog auth import)
	tokenJSON := fmt.Sprintf(`{"access_token":%q,"refresh_token":%q,"token_type":"Bearer","expiry":%q}`,
		string(accessToken), string(refreshToken), gToken.TokenExpiry.Format("2006-01-02T15:04:05Z"))

	return map[string]any{
		"google_client_b64": base64.StdEncoding.EncodeToString([]byte(clientJSON)),
		"google_token_b64":  base64.StdEncoding.EncodeToString([]byte(tokenJSON)),
		"google_email":      gToken.GoogleEmail,
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

		// Parse status and version info from body
		var body struct {
			Status               string `json:"status"`
			OpenClawVersion      string `json:"openclaw_version"`
			OpenClawUpdateStatus string `json:"openclaw_update_status"`
			OpenClawUpdateError  string `json:"openclaw_update_error"`
			AgentError           string `json:"agent_error"`
			CodexAuthPresent     bool   `json:"codex_auth_present"`
			CodexConfigActive    bool   `json:"codex_config_active"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		var agentStatus *string
		if body.Status != "" {
			agentStatus = &body.Status
		}

		var agentError *string
		if body.AgentError != "" {
			agentError = &body.AgentError
		}

		if err := db.UpdateInstanceHeartbeat(r.Context(), deps.Pool, inst.ID, agentStatus, agentError); err != nil {
			slog.Error("agent heartbeat: update", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to record heartbeat")
			return
		}

		// Record OpenClaw version and update status if reported
		if body.OpenClawVersion != "" {
			var updateStatus *string
			var updateError *string
			if body.OpenClawUpdateStatus != "" {
				updateStatus = &body.OpenClawUpdateStatus
			}
			if body.OpenClawUpdateError != "" {
				updateError = &body.OpenClawUpdateError
			}
			if err := db.UpdateInstanceOpenClawVersion(r.Context(), deps.Pool, inst.ID, body.OpenClawVersion, updateStatus, updateError); err != nil {
				slog.Error("agent heartbeat: update openclaw version", "error", err)
			}
		}

		if inst.Framework == models.FrameworkHermes &&
			body.CodexAuthPresent &&
			body.CodexConfigActive &&
			body.AgentError != "codex_reauth_required" &&
			inst.CodexLinkedAt == nil {
			if err := setHermesCodexAgentConfig(r.Context(), deps, inst.ID); err != nil {
				slog.Error("agent heartbeat: self-heal Hermes Codex config", "error", err, "instance_id", inst.ID)
			} else {
				linkedAt := time.Now().UTC()
				if err := db.SetCodexLinkState(r.Context(), deps.Pool, inst.ID, &linkedAt); err != nil {
					slog.Error("agent heartbeat: self-heal Hermes Codex link state", "error", err, "instance_id", inst.ID)
				}
			}
		}

		// Return current config version so agent can detect changes
		var configVersion int
		config, err := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, inst.ID)
		if err == nil && config != nil {
			configVersion = config.Version
		}

		// Compute effective target version: per-instance override > global (framework-aware)
		var targetVersion string
		if inst.TargetOpenClawVersion != nil && *inst.TargetOpenClawVersion != "" {
			targetVersion = *inst.TargetOpenClawVersion
		} else if inst.Framework == "hermes" {
			targetVersion, _ = db.GetGlobalHermesVersion(r.Context(), deps.Pool)
		} else {
			targetVersion, _ = db.GetGlobalTargetVersion(r.Context(), deps.Pool)
		}

		// Include preview_domain so the heartbeat script can configure
		// Caddy hostname-based routing (preview domain → port 3000).
		var previewDomain string
		if inst.PreviewDomain != nil {
			previewDomain = *inst.PreviewDomain
		}

		// Include custom_caddyfile so the heartbeat script uses it instead
		// of the auto-generated Caddyfile (for users with custom web apps).
		var customCaddyfile string
		if inst.CustomCaddyfile != nil {
			customCaddyfile = *inst.CustomCaddyfile
		}

		resp := map[string]any{
			"config_version":          configVersion,
			"target_openclaw_version": targetVersion,
			"preview_domain":          previewDomain,
			"custom_caddyfile":        customCaddyfile,
		}
		if inst.Framework == models.FrameworkHermes && inst.OpenClawAuthToken != nil && *inst.OpenClawAuthToken != "" {
			resp["dashboard_token"] = *inst.OpenClawAuthToken
		}

		WriteJSON(w, http.StatusOK, resp)
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
		for _, keyField := range []string{"openrouter_api_key", "anthropic_api_key", "openai_api_key", "telegram_bot_token"} {
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

		// Validate model against catalog if provided
		if modelID, ok := body.Config["model"].(string); ok && modelID != "" {
			valid, err := db.IsModelValid(r.Context(), deps.Pool, modelID)
			if err != nil {
				slog.Error("update agent config: validate model", "error", err)
				WriteError(w, http.StatusInternalServerError, "internal_error", "failed to validate model")
				return
			}
			if !valid {
				WriteError(w, http.StatusBadRequest, "bad_request", "model is not available: "+modelID)
				return
			}
		}

		// Atomic read-merge-write to prevent concurrent updates from losing changes
		preserveKeys := []string{"openrouter_api_key", "anthropic_api_key", "openai_api_key", "provider", "model", "telegram_bot_token", "telegram_allowed_users"}
		saved, err := db.UpdateAgentConfigAtomic(r.Context(), deps.Pool, inst.ID, body.Config, preserveKeys)
		if err != nil {
			slog.Error("update agent config: save", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to save config")
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"config":  saved.Config,
			"version": saved.Version,
		})
	}
}

// AgentHeartbeatScriptHandler serves the latest heartbeat bash script.
// Called by cloud-init at boot and by config sync to keep the script fresh.
// Authenticated by agent token.
func AgentHeartbeatScriptHandler(deps Dependencies) http.HandlerFunc {
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

		var script string
		switch inst.Framework {
		case models.FrameworkHermes:
			script = scripts.HermesHeartbeatScript
		default:
			script = scripts.HeartbeatScript
		}

		w.Header().Set("Content-Type", "text/x-shellscript")
		w.Write([]byte(script))
	}
}

// AgentHostAdminScriptHandler serves the installer for the VPS-side host
// admin helper. Called by OpenClaw cloud-init and the heartbeat drift guard.
// Authenticated by agent token.
func AgentHostAdminScriptHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractAgentToken(r)
		if token == "" {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing agent token")
			return
		}

		if _, err := db.GetInstanceByAgentToken(r.Context(), deps.Pool, token); err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid agent token")
			return
		}

		w.Header().Set("Content-Type", "text/x-shellscript")
		w.Write([]byte(scripts.HostAdminInstallScript))
	}
}

// AgentDashboardShimHandler serves the compiled tardi-dashboard-shim binary.
// Cloud-init downloads it during provisioning and the heartbeat refreshes it
// when the sha256 changes. Authenticated by agent token.
func AgentDashboardShimHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractAgentToken(r)
		if token == "" {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing agent token")
			return
		}
		if _, err := db.GetInstanceByAgentToken(r.Context(), deps.Pool, token); err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid agent token")
			return
		}

		bin := scripts.DashboardShimBinary()
		if len(bin) == 0 {
			WriteError(w, http.StatusServiceUnavailable, "shim_unavailable", "dashboard-shim binary not loaded")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", scripts.DashboardShimSHA256())
		w.Write(bin)
	}
}

// AgentDashboardShimSHAHandler returns the hex sha256 of the current shim
// binary so the heartbeat can cheaply check for drift before re-downloading.
func AgentDashboardShimSHAHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractAgentToken(r)
		if token == "" {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing agent token")
			return
		}
		if _, err := db.GetInstanceByAgentToken(r.Context(), deps.Pool, token); err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid agent token")
			return
		}

		sha := scripts.DashboardShimSHA256()
		if sha == "" {
			WriteError(w, http.StatusServiceUnavailable, "shim_unavailable", "dashboard-shim binary not loaded")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(sha))
	}
}

func extractAgentToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
