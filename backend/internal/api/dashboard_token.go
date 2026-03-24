package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/sshexec"
)

// DashboardTokenHandler generates a scoped device token on-demand for the
// OpenClaw Control UI. The token is created via `openclaw devices rotate`
// which grants operator.read, operator.write, and other scopes that the
// static gateway token does not have (post OC v2026.2.14).
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

		// Get the gateway token, find the first paired device, rotate its token
		// with all operator scopes. The rotated token is what the Control UI
		// uses in the WebSocket connect message.
		script := `#!/bin/bash
set -e
GW_TOKEN=$(cat /opt/openclaw/data/openclaw/openclaw.json | jq -r '.gateway.auth.token // empty')
if [ -z "$GW_TOKEN" ]; then
    echo '{"error":"no gateway token found"}' && exit 0
fi
DEVICE_ID=$(docker exec openclaw-gateway openclaw devices list --token "$GW_TOKEN" --json 2>/dev/null | jq -r '.paired[0].deviceId // empty')
if [ -z "$DEVICE_ID" ]; then
    echo '{"error":"no paired device found"}' && exit 0
fi
docker exec openclaw-gateway openclaw devices rotate \
    --token "$GW_TOKEN" \
    --device "$DEVICE_ID" \
    --role operator \
    --scope operator.read \
    --scope operator.write \
    --scope operator.admin \
    --scope operator.approvals \
    --scope operator.pairing \
    --json 2>/dev/null
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
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			slog.Error("dashboard-token: invalid response", "error", err, "raw", out, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "invalid response from agent")
			return
		}

		if result.Error != "" {
			slog.Error("dashboard-token: agent error", "error", result.Error, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", fmt.Sprintf("agent error: %s", result.Error))
			return
		}

		if result.Token == "" {
			WriteError(w, http.StatusBadGateway, "gateway_error", "empty token returned")
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"token": result.Token,
		})
	}
}
