package api

import (
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

// configSyncScript is the inline config-sync logic run via SSH.
// It mirrors the heartbeat script's config-sync section but is always
// up-to-date (not baked in at provisioning time).
const configSyncScript = `#!/bin/bash
set -euo pipefail
source /opt/openclaw/.env

CONFIG=$(curl -sf "${API_URL}/api/agent/config" \
    -H "Authorization: Bearer ${AGENT_TOKEN}" 2>/dev/null)
if [ $? -ne 0 ] || [ -z "$CONFIG" ]; then
    echo "ERROR: failed to fetch config from API"
    exit 1
fi

NEW_OR_KEY=$(echo "$CONFIG" | jq -r '.config.openrouter_api_key // empty')
NEW_AN_KEY=$(echo "$CONFIG" | jq -r '.config.anthropic_api_key // empty')
NEW_OA_KEY=$(echo "$CONFIG" | jq -r '.config.openai_api_key // empty')
NEW_TG_TOKEN=$(echo "$CONFIG" | jq -r '.config.telegram_bot_token // empty')
NEW_PROVIDER=$(echo "$CONFIG" | jq -r '.config.provider // empty')
NEW_MODEL=$(echo "$CONFIG" | jq -r '.config.model // empty')
REMOTE_VERSION=$(echo "$CONFIG" | jq -r '.version // 0')

# Rebuild .env preserving non-key/token vars
grep -v -E '_API_KEY=|TELEGRAM_BOT_TOKEN=' /opt/openclaw/.env > /opt/openclaw/.env.tmp
[ -n "$NEW_OR_KEY" ] && echo "OPENROUTER_API_KEY=$NEW_OR_KEY" >> /opt/openclaw/.env.tmp
[ -n "$NEW_AN_KEY" ] && echo "ANTHROPIC_API_KEY=$NEW_AN_KEY" >> /opt/openclaw/.env.tmp
[ -n "$NEW_OA_KEY" ] && echo "OPENAI_API_KEY=$NEW_OA_KEY" >> /opt/openclaw/.env.tmp
[ -n "$NEW_TG_TOKEN" ] && echo "TELEGRAM_BOT_TOKEN=$NEW_TG_TOKEN" >> /opt/openclaw/.env.tmp
mv /opt/openclaw/.env.tmp /opt/openclaw/.env
chmod 600 /opt/openclaw/.env

# Update openclaw.json to enable/disable telegram channel
OC_CONFIG="/opt/openclaw/data/openclaw/openclaw.json"
if [ -n "$NEW_TG_TOKEN" ]; then
    jq '.channels.telegram = {"enabled": true}' "$OC_CONFIG" > "${OC_CONFIG}.tmp" && mv "${OC_CONFIG}.tmp" "$OC_CONFIG"
    echo "telegram: enabled"
else
    jq 'del(.channels.telegram)' "$OC_CONFIG" > "${OC_CONFIG}.tmp" && mv "${OC_CONFIG}.tmp" "$OC_CONFIG"
    echo "telegram: disabled"
fi
chown 1000:1000 "$OC_CONFIG"

# Recreate container to pick up new env and config
cd /opt/openclaw && docker compose up -d --force-recreate openclaw-gateway

# Wait for healthy, then update default model if provider+model are set
if [ -n "$NEW_PROVIDER" ] && [ -n "$NEW_MODEL" ]; then
    for i in $(seq 1 12); do
        sleep 5
        if docker exec openclaw-gateway curl -sf http://localhost:18789/health >/dev/null 2>&1; then
            docker exec openclaw-gateway openclaw models set "${NEW_PROVIDER}/${NEW_MODEL}" 2>/dev/null
            break
        fi
    done
fi

echo "$REMOTE_VERSION" > /opt/openclaw/.config_version
echo "config sync complete (version=$REMOTE_VERSION)"
`

// SyncConfigHandler triggers an immediate config sync on the VPS by
// SSH-ing in and running the config sync commands directly. This avoids
// relying on the heartbeat script (which may be an older version baked
// in at provisioning time).
func SyncConfigHandler(deps Dependencies) http.HandlerFunc {
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
			slog.Error("sync config: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.Status != "active" {
			WriteError(w, http.StatusConflict, "conflict", "instance is not active")
			return
		}

		if inst.IPv4 == nil || *inst.IPv4 == "" {
			WriteError(w, http.StatusConflict, "conflict", "instance has no IP address")
			return
		}

		if inst.RootPassword == nil || *inst.RootPassword == "" {
			WriteError(w, http.StatusConflict, "conflict", "instance has no root password")
			return
		}

		slog.Info("sync config: triggering config sync via SSH",
			"instance_id", instanceID,
			"ip", *inst.IPv4,
		)

		// Fire-and-forget: config sync can take 60-90s (docker recreate, health wait).
		ip := *inst.IPv4
		pw := *inst.RootPassword
		deps.BGTasks.Add(1)
		go func() {
			defer deps.BGTasks.Done()
			// Write the sync script and run it
			cmd := fmt.Sprintf("cat > /tmp/config-sync.sh << 'SYNCEOF'\n%sSYNCEOF\nchmod +x /tmp/config-sync.sh && /tmp/config-sync.sh", configSyncScript)
			output, err := sshexec.RunCommand(ip, pw, cmd, 120*time.Second)
			if err != nil {
				slog.Error("sync config: SSH command failed",
					"instance_id", instanceID,
					"error", err,
					"output", output,
				)
				return
			}
			slog.Info("sync config: completed", "instance_id", instanceID, "output", output)
		}()

		WriteJSON(w, http.StatusOK, map[string]any{
			"synced": true,
		})
	}
}
