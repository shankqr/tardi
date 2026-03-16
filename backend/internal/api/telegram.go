package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

// TelegramConnectHandler saves a Telegram bot token into agent config.
// The VPS heartbeat script detects the config version change, pulls the new config,
// updates openclaw.json and .env, then recreates the container.
// POST /api/instances/{id}/telegram/connect
func TelegramConnectHandler(deps Dependencies) http.HandlerFunc {
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
			WriteError(w, http.StatusNotFound, "not_found", "instance not found")
			return
		}

		var body struct {
			BotToken string `json:"bot_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid request body")
			return
		}
		if body.BotToken == "" {
			WriteError(w, http.StatusBadRequest, "bad_request", "bot_token is required")
			return
		}

		// Save token into agent config (bumps version, triggers heartbeat sync)
		existing, _ := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, inst.ID)
		config := make(map[string]any)
		if existing != nil {
			for k, v := range existing.Config {
				config[k] = v
			}
		}
		config["telegram_bot_token"] = body.BotToken

		ac := &models.AgentConfig{
			ID:            uuid.New(),
			VpsInstanceID: inst.ID,
			Config:        config,
			Version:       1,
		}
		if err := db.CreateAgentConfig(r.Context(), deps.Pool, ac); err != nil {
			slog.Error("telegram connect: save config", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to save telegram token")
			return
		}

		slog.Info("telegram connect: token saved, will propagate on next heartbeat sync", "instance_id", instanceID)
		WriteJSON(w, http.StatusOK, map[string]any{
			"connected": true,
		})
	}
}

// TelegramDisconnectHandler removes the Telegram bot token from agent config.
// POST /api/instances/{id}/telegram/disconnect
func TelegramDisconnectHandler(deps Dependencies) http.HandlerFunc {
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
			WriteError(w, http.StatusNotFound, "not_found", "instance not found")
			return
		}

		// Remove token from agent config (bumps version, triggers heartbeat sync)
		existing, _ := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, instanceID)
		if existing != nil {
			config := make(map[string]any)
			for k, v := range existing.Config {
				config[k] = v
			}
			delete(config, "telegram_bot_token")

			ac := &models.AgentConfig{
				ID:            uuid.New(),
				VpsInstanceID: instanceID,
				Config:        config,
				Version:       1,
			}
			if err := db.CreateAgentConfig(r.Context(), deps.Pool, ac); err != nil {
				slog.Error("telegram disconnect: save config", "error", err, "instance_id", instanceID)
				WriteError(w, http.StatusInternalServerError, "internal_error", "failed to remove telegram token")
				return
			}
		}

		slog.Info("telegram disconnect: token removed, will propagate on next heartbeat sync", "instance_id", instanceID)
		WriteJSON(w, http.StatusOK, map[string]any{
			"connected": false,
		})
	}
}

// removeTelegramChannelConfig uses OpenClaw's config.patch RPC to remove
// channels.telegram from the internal persistent config. Editing openclaw.json
// on disk is NOT sufficient — OpenClaw merges the file into its own internal
// config store on first boot and uses that going forward. Having channels.telegram
// in the internal config AND TELEGRAM_BOT_TOKEN in the env var causes OpenClaw
// to create two Telegram handlers, resulting in double replies.
func removeTelegramChannelConfig(ctx context.Context, ipv4, authToken string) error {
	// Step 1: config.get to obtain the concurrency hash
	getResult, err := openclawRPC(ctx, ipv4, authToken, "config.get", map[string]any{})
	if err != nil {
		return err
	}

	var configResp struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(getResult, &configResp); err != nil || configResp.Hash == "" {
		return err
	}

	// Step 2: config.patch to explicitly disable channels.telegram.
	// Setting to null doesn't persist — OpenClaw's internal config reload
	// revives it. Setting enabled:false is a persistent disable.
	patchJSON := `{"channels":{"telegram":{"enabled":false}}}`
	_, err = openclawRPC(ctx, ipv4, authToken, "config.patch", map[string]any{
		"raw":  patchJSON,
		"hash": configResp.Hash,
	})
	return err
}

// TelegramCleanupHandler removes channels.telegram from OpenClaw's internal
// config via RPC. The frontend calls this after a config sync completes to
// ensure the env var auto-detection is the sole Telegram handler.
// POST /api/instances/{id}/telegram/cleanup
func TelegramCleanupHandler(deps Dependencies) http.HandlerFunc {
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
			WriteError(w, http.StatusNotFound, "not_found", "instance not found")
			return
		}

		if inst.IPv4 == nil || *inst.IPv4 == "" || inst.OpenClawAuthToken == nil || *inst.OpenClawAuthToken == "" {
			WriteError(w, http.StatusConflict, "conflict", "instance not ready")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		if err := removeTelegramChannelConfig(ctx, *inst.IPv4, *inst.OpenClawAuthToken); err != nil {
			slog.Warn("telegram cleanup: config.patch failed",
				"error", err,
				"instance_id", instanceID,
			)
			// Non-fatal — return success anyway, the env var handler will still work
			WriteJSON(w, http.StatusOK, map[string]any{
				"cleaned": false,
				"error":   "could not patch OpenClaw config",
			})
			return
		}

		slog.Info("telegram cleanup: channels.telegram removed from internal config", "instance_id", instanceID)
		WriteJSON(w, http.StatusOK, map[string]any{
			"cleaned": true,
		})
	}
}
