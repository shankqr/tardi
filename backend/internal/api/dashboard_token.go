package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/sshexec"
)

// DashboardTokenHandler reads the current gateway token from the VPS.
// This is a read-only operation — it does NOT modify openclaw.json.
// trustedProxies and other config fixes are handled by the heartbeat
// drift guard (every 5 min) and the provisioner at initial setup.
func DashboardTokenHandler(deps Dependencies) http.HandlerFunc {
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
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.IPv4 == nil || *inst.IPv4 == "" || inst.RootPassword == nil || *inst.RootPassword == "" {
			WriteError(w, http.StatusConflict, "conflict", "instance not reachable")
			return
		}

		// Read the actual gateway token OpenClaw is using. Three possible sources:
		// 1. Running container's process env (most reliable — what OpenClaw actually uses)
		// 2. .env file (set at provisioning, may be stale after restart)
		// 3. openclaw.json gateway.auth.token (only if set via config commands)
		// This script is READ-ONLY — no modifications to openclaw.json.
		// Config fixes (trustedProxies, auth mode, etc.) are handled by the
		// heartbeat drift guard and provisioner.
		// Try DB-cached token first — avoids SSH entirely when available.
		// This is the fast path; SSH is only needed when the DB token is empty
		// (e.g. first boot before heartbeat syncs the token).
		if inst.OpenClawAuthToken != nil && *inst.OpenClawAuthToken != "" {
			WriteJSON(w, http.StatusOK, map[string]string{
				"token": *inst.OpenClawAuthToken,
			})
			return
		}

		// Fallback: read token from VPS via SSH
		script := `#!/bin/bash
# No set -e: we want to try all fallbacks even if earlier commands fail
OC_CFG="/opt/openclaw/data/openclaw/openclaw.json"
# 1. Read from running container's process environment (most reliable)
GW_TOKEN=$(docker exec openclaw-gateway printenv OPENCLAW_GATEWAY_TOKEN 2>/dev/null || true)
# 2. Fallback: .env file
if [ -z "$GW_TOKEN" ]; then
    GW_TOKEN=$(grep '^OPENCLAW_GATEWAY_TOKEN=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2- || true)
fi
# 3. Fallback: openclaw.json
if [ -z "$GW_TOKEN" ]; then
    GW_TOKEN=$(cat "$OC_CFG" 2>/dev/null | jq -r '.gateway.auth.token // empty' 2>/dev/null || true)
fi
if [ -z "$GW_TOKEN" ]; then
    echo '{"error":"no gateway token"}'
    exit 0
fi
echo "{\"token\":\"$GW_TOKEN\"}"
`
		out, err := sshexec.RunCommand(*inst.IPv4, deps.Config.SSHPrivateKey, *inst.RootPassword, script, 30*time.Second)
		if err != nil {
			slog.Error("dashboard-token: ssh failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to generate dashboard token")
			return
		}

		var result struct {
			Token string `json:"token"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
			slog.Error("dashboard-token: invalid response", "error", err, "raw", out, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "invalid response from agent")
			return
		}

		if result.Error != "" {
			slog.Error("dashboard-token: agent error", "error", result.Error, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", result.Error)
			return
		}

		if result.Token == "" {
			WriteError(w, http.StatusBadGateway, "gateway_error", "empty token returned")
			return
		}

		// Sync token to DB so backend RPC calls use the correct token
		if err := db.UpdateInstanceOpenClawAuthToken(r.Context(), deps.Pool, instanceID, result.Token); err != nil {
			slog.Warn("dashboard-token: failed to sync token to DB", "error", err, "instance_id", instanceID)
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"token": result.Token,
		})
	}
}
