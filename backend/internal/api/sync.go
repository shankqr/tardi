package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/sshexec"
)

// buildConfigSyncScript returns the inline config-sync script run via SSH.
// It downloads the latest heartbeat script from the backend API so that
// old VPSes get updated heartbeat code (with Telegram drift guard) on every sync.
func buildConfigSyncScript() string {
	return `#!/bin/bash
set -euo pipefail

# Source only the vars we need (full source breaks on unquoted values with spaces)
export API_URL=$(grep '^API_URL=' /opt/openclaw/.env | cut -d= -f2-)
export AGENT_TOKEN=$(grep '^AGENT_TOKEN=' /opt/openclaw/.env | cut -d= -f2-)

# Update heartbeat script to latest version (downloaded from backend API)
curl -sf -H "Authorization: Bearer ${AGENT_TOKEN}" "${API_URL}/api/agent/heartbeat-script" -o /opt/openclaw/heartbeat.sh
chmod +x /opt/openclaw/heartbeat.sh

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
NEW_GOOGLE_CLIENT=$(echo "$CONFIG" | jq -r '.config.google_client_b64 // empty')
NEW_GOOGLE_TOKEN=$(echo "$CONFIG" | jq -r '.config.google_token_b64 // empty')
NEW_GOOGLE_EMAIL=$(echo "$CONFIG" | jq -r '.config.google_email // empty')
REMOTE_VERSION=$(echo "$CONFIG" | jq -r '.version // 0')
ALL_MODEL_IDS=$(echo "$CONFIG" | jq -r '.model_ids // [] | .[]' 2>/dev/null)

# Snapshot current .env so we can detect whether env actually changed
cp /opt/openclaw/.env /opt/openclaw/.env.bak

# Rebuild .env preserving non-key/token vars
grep -v -E '_API_KEY=|TELEGRAM_BOT_TOKEN=' /opt/openclaw/.env > /opt/openclaw/.env.tmp
[ -n "$NEW_OR_KEY" ] && echo "OPENROUTER_API_KEY=$NEW_OR_KEY" >> /opt/openclaw/.env.tmp
[ -n "$NEW_AN_KEY" ] && echo "ANTHROPIC_API_KEY=$NEW_AN_KEY" >> /opt/openclaw/.env.tmp
[ -n "$NEW_OA_KEY" ] && echo "OPENAI_API_KEY=$NEW_OA_KEY" >> /opt/openclaw/.env.tmp
[ -n "$NEW_TG_TOKEN" ] && echo "TELEGRAM_BOT_TOKEN=$NEW_TG_TOKEN" >> /opt/openclaw/.env.tmp
mv /opt/openclaw/.env.tmp /opt/openclaw/.env
chmod 600 /opt/openclaw/.env

# Only recreate container if env vars actually changed. Google OAuth
# credentials are files on the host volume — no restart needed for those.
ENV_CHANGED=false
if ! diff -q /opt/openclaw/.env /opt/openclaw/.env.bak >/dev/null 2>&1; then
    ENV_CHANGED=true
fi
rm -f /opt/openclaw/.env.bak

if [ "$ENV_CHANGED" = true ]; then
    # Recreate container to pick up new env
    # NOTE: Do NOT edit openclaw.json — OpenClaw owns that file and overwrites it
    # on startup. Config changes must go through config.patch RPC (the /telegram/cleanup
    # endpoint does this after the container is healthy).
    cd /opt/openclaw && docker compose up -d --force-recreate openclaw-gateway

    # Wait for healthy, then apply post-startup config before reporting completion
    HEALTHY=false
    for i in $(seq 1 12); do
        sleep 5
        if docker exec openclaw-gateway curl -sf http://localhost:18789/health >/dev/null 2>&1; then
            HEALTHY=true
            break
        fi
    done
else
    echo "env unchanged, skipping container recreate"
    HEALTHY=true
fi

if [ "$HEALTHY" = true ]; then
    # Fix Telegram config if bot token is set:
    # - streaming:"off" prevents double replies (OpenClaw defaults to "partial")
    # - dmPolicy:"open" + allowFrom:["*"] allows anyone to message the bot
    # - groupPolicy:"disabled" ignores group messages
    # Must set allowFrom before dmPolicy (validation requires it)
    if [ -n "$NEW_TG_TOKEN" ]; then
        docker exec openclaw-gateway openclaw config set channels.telegram.enabled true 2>/dev/null
        docker exec openclaw-gateway openclaw config set channels.telegram.streaming off 2>/dev/null
        docker exec openclaw-gateway openclaw config set channels.telegram.allowFrom '["*"]' 2>/dev/null
        docker exec openclaw-gateway openclaw config set channels.telegram.dmPolicy open 2>/dev/null
        docker exec openclaw-gateway openclaw config set channels.telegram.groupPolicy disabled 2>/dev/null
        echo "telegram config patched"
    fi

    # Register all available models in OpenClaw so the OC dashboard dropdown
    # shows the full Tardi model catalog. OpenClaw only shows models that have
    # been explicitly added via "openclaw models set".
    # Register non-active models first, then set the user's selected model last
    # so it becomes the active/default model.
    # OpenRouter model IDs contain a slash (e.g. "anthropic/claude-sonnet-4.6")
    # which OpenClaw interprets as a provider prefix. Prepend "openrouter/" so
    # OpenClaw routes through OpenRouter instead of the native provider.
    if [ -n "$ALL_MODEL_IDS" ]; then
        for MID in $ALL_MODEL_IDS; do
            [ "$MID" = "$NEW_MODEL" ] && continue
            if [ "$NEW_PROVIDER" = "openrouter" ]; then
                docker exec openclaw-gateway openclaw models set "openrouter/${MID}" 2>/dev/null
            else
                docker exec openclaw-gateway openclaw models set "${MID}" 2>/dev/null
            fi
        done
        echo "registered $(echo "$ALL_MODEL_IDS" | wc -w | tr -d ' ') models in OpenClaw"
    fi

    # Set the user's selected model last so it becomes the active default
    if [ -n "$NEW_MODEL" ]; then
        if [ "$NEW_PROVIDER" = "openrouter" ]; then
            docker exec openclaw-gateway openclaw models set "openrouter/${NEW_MODEL}"
            echo "model set to openrouter/${NEW_MODEL}"
        else
            docker exec openclaw-gateway openclaw models set "${NEW_MODEL}"
            echo "model set to ${NEW_MODEL}"
        fi
    fi

    # Write Google OAuth credential files for gog CLI
    GOG_DIR="/opt/openclaw/data/openclaw/.config/gogcli"
    if [ -n "$NEW_GOOGLE_TOKEN" ] && [ -n "$NEW_GOOGLE_EMAIL" ]; then
        mkdir -p "$GOG_DIR/tokens"
        [ -n "$NEW_GOOGLE_CLIENT" ] && printf '%s' "$NEW_GOOGLE_CLIENT" | base64 -d > "$GOG_DIR/credentials.json"
        printf '%s' "$NEW_GOOGLE_TOKEN" | base64 -d > "$GOG_DIR/tokens/${NEW_GOOGLE_EMAIL}.json"
        chmod -R 600 "$GOG_DIR"
        chmod 700 "$GOG_DIR" "$GOG_DIR/tokens"
        echo "google credentials written for ${NEW_GOOGLE_EMAIL}"
    else
        rm -rf "$GOG_DIR"
    fi

    # Write config version AFTER all patches applied so heartbeat retries
    # if this script is interrupted before reaching this point
    echo "$REMOTE_VERSION" > /opt/openclaw/.config_version

    # Trigger an immediate heartbeat so agent_status in the DB updates to
    # "running" right away instead of waiting up to 5 minutes for the timer.
    bash /opt/openclaw/heartbeat.sh >/dev/null 2>&1 &

    # Report completion AFTER all config patches are applied so the frontend
    # does not show success before Telegram dmPolicy/streaming are set
    echo "config sync complete (version=$REMOTE_VERSION)"
else
    echo "ERROR: container did not become healthy after recreate"
    exit 1
fi
`
}

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

		ip := *inst.IPv4
		pw := *inst.RootPassword
		sshKey := deps.Config.SSHPrivateKey

		// Deploy the script and launch it in the background via nohup.
		// The script takes 60-90s (docker recreate + health wait) which
		// exceeds Cloud Run's effective connection timeout (~60s), so we
		// cannot wait for it synchronously. Instead we:
		//   1. Upload the script (fast, <2s)
		//   2. Launch it detached with nohup (returns immediately)
		//   3. Return success to the frontend
		encoded := base64.StdEncoding.EncodeToString([]byte(buildConfigSyncScript()))
		cmd := fmt.Sprintf(
			"echo %s | base64 -d > /tmp/config-sync.sh && chmod +x /tmp/config-sync.sh && systemctl stop tardi-config-sync 2>/dev/null; systemctl reset-failed tardi-config-sync 2>/dev/null; systemd-run --unit=tardi-config-sync --no-block --collect bash /tmp/config-sync.sh",
			encoded,
		)
		_, err = sshexec.RunCommand(ip, sshKey, pw, cmd, 30*time.Second)
		if err != nil {
			slog.Error("sync config: failed to launch script",
				"instance_id", instanceID,
				"error", err,
			)
			WriteJSON(w, http.StatusOK, map[string]any{
				"synced": false,
				"error":  "Could not connect to your agent",
			})
			return
		}

		slog.Info("sync config: script launched", "instance_id", instanceID)
		WriteJSON(w, http.StatusOK, map[string]any{
			"synced": true,
		})
	}
}

// SyncStatusHandler checks the status of a running config sync by
// querying the systemd transient unit on the VPS via SSH.
func SyncStatusHandler(deps Dependencies) http.HandlerFunc {
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
			WriteJSON(w, http.StatusOK, map[string]any{
				"status": "unknown",
				"error":  "instance not reachable",
			})
			return
		}

		// Check systemd unit state + grab last 5 lines from journal
		cmd := `STATE=$(systemctl show tardi-config-sync --property=ActiveState --value 2>/dev/null || echo "not-found"); RESULT=$(systemctl show tardi-config-sync --property=Result --value 2>/dev/null || echo ""); LOG=$(journalctl -u tardi-config-sync --no-pager -n 5 -o cat 2>/dev/null || echo ""); printf '{"state":"%s","result":"%s","log":"%s"}' "$STATE" "$RESULT" "$LOG"`
		out, err := sshexec.RunCommand(*inst.IPv4, deps.Config.SSHPrivateKey, *inst.RootPassword, cmd, 10*time.Second)
		if err != nil {
			WriteJSON(w, http.StatusOK, map[string]any{
				"status": "unknown",
				"error":  "could not check sync status",
			})
			return
		}

		// Parse the state from the output
		// ActiveState: active (running), inactive (finished), failed, not-found
		status := "running"
		message := ""
		if strings.Contains(out, "config sync complete") {
			// Config applied — unit may still be active (model-set running) or inactive
			status = "completed"
			message = "Configuration applied successfully"
		} else if strings.Contains(out, `"state":"inactive"`) {
			status = "completed"
			message = "Sync finished"
		} else if strings.Contains(out, `"state":"failed"`) {
			status = "failed"
			message = "Config sync failed on your agent"
		} else if strings.Contains(out, `"state":"not-found"`) {
			status = "unknown"
			message = "No sync in progress"
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"status":  status,
			"message": message,
			"raw":     out,
		})
	}
}

