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

// TelegramConnectHandler saves a Telegram bot token and tells OpenClaw to enable Telegram.
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

		// Save token into agent config
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

		// Tell OpenClaw to enable Telegram channel with this bot token
		if inst.IPv4 != nil && inst.OpenClawAuthToken != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()

			_, err := openclawRPC(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "channels.telegram.connect", map[string]any{
				"botToken": body.BotToken,
			})
			if err != nil {
				slog.Warn("telegram connect: rpc failed (token saved, will retry on next config sync)", "error", err, "instance_id", instanceID)
				// Token is saved — agent will pick it up on next config sync even if RPC fails
			}
		}

		slog.Info("telegram connect: token saved", "instance_id", instanceID)
		WriteJSON(w, http.StatusOK, map[string]any{
			"connected": true,
		})
	}
}

// TelegramDisconnectHandler removes the Telegram bot token and tells OpenClaw to disable Telegram.
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

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			WriteError(w, http.StatusNotFound, "not_found", "instance not found")
			return
		}

		// Remove token from agent config
		existing, _ := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, inst.ID)
		if existing != nil {
			config := make(map[string]any)
			for k, v := range existing.Config {
				config[k] = v
			}
			delete(config, "telegram_bot_token")

			ac := &models.AgentConfig{
				ID:            uuid.New(),
				VpsInstanceID: inst.ID,
				Config:        config,
				Version:       1,
			}
			if err := db.CreateAgentConfig(r.Context(), deps.Pool, ac); err != nil {
				slog.Error("telegram disconnect: save config", "error", err, "instance_id", instanceID)
				WriteError(w, http.StatusInternalServerError, "internal_error", "failed to remove telegram token")
				return
			}
		}

		// Tell OpenClaw to disable Telegram
		if inst.IPv4 != nil && inst.OpenClawAuthToken != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()

			_, err := openclawRPC(ctx, *inst.IPv4, *inst.OpenClawAuthToken, "channels.telegram.disconnect", nil)
			if err != nil {
				slog.Warn("telegram disconnect: rpc failed", "error", err, "instance_id", instanceID)
			}
		}

		slog.Info("telegram disconnect: token removed", "instance_id", instanceID)
		WriteJSON(w, http.StatusOK, map[string]any{
			"connected": false,
		})
	}
}
