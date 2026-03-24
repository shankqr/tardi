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

// DashboardTokenHandler reads the current gateway token from the VPS and
// ensures trustedProxies is configured so operator scopes work.
//
// Background: Cloudflare adds X-Forwarded-For headers. Without
// gateway.trustedProxies, OpenClaw treats the connection as coming from an
// untrusted proxy and won't grant operator scopes — causing
// "GatewayRequestError: missing scope: operator.read".
//
// Fix: set trustedProxies: ["0.0.0.0/0"] (safe because auth is token-based)
// and return the current gateway token for the frontend to use.
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
		// Also ensure trustedProxies is set so operator scopes work through Cloudflare.
		script := `#!/bin/bash
set -e
OC_CFG="/opt/openclaw/data/openclaw/openclaw.json"
# 1. Read from running container's process environment (most reliable)
GW_TOKEN=$(docker exec openclaw-gateway printenv OPENCLAW_GATEWAY_TOKEN 2>/dev/null || true)
# 2. Fallback: .env file
if [ -z "$GW_TOKEN" ]; then
    GW_TOKEN=$(grep '^OPENCLAW_GATEWAY_TOKEN=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2-)
fi
# 3. Fallback: openclaw.json
if [ -z "$GW_TOKEN" ]; then
    GW_TOKEN=$(cat "$OC_CFG" 2>/dev/null | jq -r '.gateway.auth.token // empty')
fi
if [ -z "$GW_TOKEN" ]; then
    echo '{"error":"no gateway token"}' && exit 0
fi
# Ensure trustedProxies is set (Cloudflare adds X-Forwarded-For)
TRUSTED=$(cat "$OC_CFG" 2>/dev/null | jq -r '.gateway.trustedProxies // empty')
if [ -z "$TRUSTED" ] || [ "$TRUSTED" = "null" ]; then
    python3 -c "
import json, datetime
with open('$OC_CFG') as f:
    cfg = json.load(f)
cfg['gateway']['trustedProxies'] = ['0.0.0.0/0']
cfg.setdefault('meta', {})['lastTouchedAt'] = datetime.datetime.utcnow().strftime('%Y-%m-%dT%H:%M:%S.000Z')
with open('$OC_CFG', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null
    # Wait a moment for OpenClaw to detect config change via lastTouchedAt
    sleep 2
fi
# Sync .env so it stays current for next time
ENV_TOKEN=$(grep '^OPENCLAW_GATEWAY_TOKEN=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2-)
if [ "$GW_TOKEN" != "$ENV_TOKEN" ]; then
    sed -i '/^OPENCLAW_GATEWAY_TOKEN=/d' /opt/openclaw/.env
    echo "OPENCLAW_GATEWAY_TOKEN=$GW_TOKEN" >> /opt/openclaw/.env
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
