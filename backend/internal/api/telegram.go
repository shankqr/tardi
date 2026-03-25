package api

import (
	"context"
	"encoding/json"
	"fmt"
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

		// Ensure model/provider are always present so the sync script sets the
		// correct default model after container recreation. Without these, the
		// sync script registers all catalog models but never sets an explicit
		// default — causing the last model in sort order to become active.
		if _, ok := config["provider"].(string); !ok || config["provider"] == "" {
			config["provider"] = "openrouter"
		}
		if _, ok := config["model"].(string); !ok || config["model"] == "" {
			if m, err := db.GetDefaultModel(r.Context(), deps.Pool); err == nil && m != nil {
				config["model"] = m.ID
			}
		}

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

// patchTelegramConfig uses OpenClaw's config.patch RPC to set the correct
// Telegram channel settings. OpenClaw auto-detects TELEGRAM_BOT_TOKEN from
// the env var and defaults to streaming:"partial", which causes double replies
// (initial chunk + full response sent as separate messages). This patches the
// config to streaming:"off" and dmPolicy:"open".
// NOTE: OpenClaw owns openclaw.json and overwrites it on startup — editing
// the file directly is useless. Config changes MUST go through config.patch RPC.
func patchTelegramConfig(ctx context.Context, ipv4, authToken string) error {
	// First, get the current config hash (required by config.patch)
	getResult, err := openclawRPC(ctx, ipv4, authToken, "config.get", map[string]any{})
	if err != nil {
		return fmt.Errorf("config.get: %w", err)
	}

	var configResp struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(getResult, &configResp); err != nil || configResp.Hash == "" {
		return fmt.Errorf("config.get: missing hash")
	}

	// config.patch requires {raw: "<json string>", hash: "<current hash>"}.
	// - streaming:"off" prevents double replies (the actual root cause)
	// - dmPolicy:"open" + allowFrom:["*"] allows anyone to message the bot
	// Also fix account-level overrides: OpenClaw auto-creates account entries
	// with dmPolicy:"pairing" and streaming:"partial" which override top-level settings.
	telegramPatch := map[string]any{
		"enabled":     true,
		"streaming":   "off",
		"dmPolicy":    "open",
		"allowFrom":   []string{"*"},
		"groupPolicy": "disabled",
	}

	// Parse current config to find and fix account-level overrides.
	// config.get response may have config at top level or nested under "config" key.
	var resp map[string]any
	if err := json.Unmarshal(getResult, &resp); err == nil {
		// Try top-level first, then nested "config" key
		cfg := resp
		if nested, ok := resp["config"].(map[string]any); ok {
			cfg = nested
		}
		if channels, ok := cfg["channels"].(map[string]any); ok {
			if tg, ok := channels["telegram"].(map[string]any); ok {
				if accts, ok := tg["accounts"].(map[string]any); ok && len(accts) > 0 {
					accounts := make(map[string]any)
					for name := range accts {
						accounts[name] = map[string]any{
							"dmPolicy":  "open",
							"streaming": "off",
							"allowFrom": []string{"*"},
						}
					}
					telegramPatch["accounts"] = accounts
				}
			}
		}
	}

	patch := map[string]any{
		"channels": map[string]any{
			"telegram": telegramPatch,
		},
	}
	patchJSON, _ := json.Marshal(patch)

	_, err = openclawRPC(ctx, ipv4, authToken, "config.patch", map[string]any{
		"raw":      string(patchJSON),
		"baseHash": configResp.Hash,
	})
	return err
}

// TelegramCleanupHandler patches Telegram channel settings in OpenClaw's
// internal config via RPC. The frontend calls this after a config sync
// completes as a safety net to ensure correct settings (streaming:off,
// dmPolicy:open, enabled:true) even if the CLI commands in the sync script
// were interrupted or failed.
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

		if err := patchTelegramConfig(ctx, *inst.IPv4, *inst.OpenClawAuthToken); err != nil {
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
