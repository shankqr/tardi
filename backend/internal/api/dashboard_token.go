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

// DashboardTokenHandler generates a scoped gateway token for the OpenClaw
// Control UI. OpenClaw v2026.2.14+ requires explicit operator scopes on
// tokens. The plain gateway token doesn't carry scopes, causing
// "GatewayRequestError: missing scope: operator.read".
//
// Fix: rotate a device token with all operator scopes, then set it as the
// gateway auth token. This way the token serves dual purpose — gateway
// authentication AND operator scope authorization.
//
// Also syncs the token back to DB so backend RPC calls stay in sync.
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

		// 1. Read current gateway token
		// 2. Get first paired device ID
		// 3. Rotate device token with all operator scopes
		// 4. Set the scoped token as the new gateway auth token
		// The token now serves dual purpose: gateway auth + operator scopes.
		script := `#!/bin/bash
set -e
GW_TOKEN=$(cat /opt/openclaw/data/openclaw/openclaw.json | jq -r '.gateway.auth.token // empty')
if [ -z "$GW_TOKEN" ]; then
    echo '{"error":"no gateway token"}' && exit 0
fi
DEVICE_ID=$(docker exec openclaw-gateway openclaw devices list --token "$GW_TOKEN" --json 2>/dev/null | jq -r '.paired[0].deviceId // empty')
if [ -z "$DEVICE_ID" ]; then
    echo '{"error":"no paired device"}' && exit 0
fi
RESULT=$(docker exec openclaw-gateway openclaw devices rotate \
    --token "$GW_TOKEN" \
    --device "$DEVICE_ID" \
    --role operator \
    --scope operator.read \
    --scope operator.write \
    --scope operator.admin \
    --scope operator.approvals \
    --scope operator.pairing \
    --json 2>/dev/null)
NEW_TOKEN=$(echo "$RESULT" | jq -r '.token // empty')
if [ -z "$NEW_TOKEN" ]; then
    echo '{"error":"rotate failed"}' && exit 0
fi
docker exec openclaw-gateway openclaw config set gateway.auth.token "$NEW_TOKEN" >/dev/null 2>&1
echo "$RESULT"
`
		out, err := sshexec.RunCommand(*inst.IPv4, *inst.RootPassword, script, 30*time.Second)
		if err != nil {
			slog.Error("dashboard-token: ssh failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to generate dashboard token")
			return
		}

		var result struct {
			Token  string   `json:"token"`
			Error  string   `json:"error"`
			Scopes []string `json:"scopes"`
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
