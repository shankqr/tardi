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
// gateway auth token. The token now serves dual purpose — gateway auth AND
// operator scope authorization.
//
// We use `openclaw gateway restart` (internal process restart) to apply the
// new token, which preserves the config unlike `docker compose restart`.
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

		// 1. Read current gateway token from openclaw.json
		// 2. Rotate paired device token with all operator scopes
		// 3. Set rotated token as gateway auth token
		// 4. Internal gateway restart to apply (preserves config, unlike container restart)
		// 5. Wait for healthy, then return the scoped token
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
# Set rotated device token as gateway token and restart to apply
docker exec openclaw-gateway openclaw config set gateway.auth.token "$NEW_TOKEN" >/dev/null 2>&1
docker exec openclaw-gateway openclaw gateway restart >/dev/null 2>&1 || true
# Wait for gateway to come back healthy
for i in 1 2 3 4 5 6 7 8 9 10; do
    sleep 3
    if curl -sf http://localhost:18789/health >/dev/null 2>&1; then break; fi
done
echo "$RESULT"
`
		out, err := sshexec.RunCommand(*inst.IPv4, *inst.RootPassword, script, 60*time.Second)
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
