package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/sshexec"
)

// openclawRPC connects to an OpenClaw gateway via WebSocket (direct to port 18789),
// handles the connect handshake, sends an RPC method, and returns the result.
// OpenClaw uses a custom protocol: requests are {"type":"req","id":"...","method":"...","params":{...}}
// and responses are {"type":"res","id":"...","ok":true/false,"payload":{...},"error":{...}}.
func openclawRPC(ctx context.Context, ipv4, authToken, method string, params any) (json.RawMessage, error) {
	url := fmt.Sprintf("ws://%s:18789/?token=%s", ipv4, authToken)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	// Origin header is required for admin RPC methods (config.get, config.patch).
	// OpenClaw checks it against gateway.controlUi.allowedOrigins which includes "*".
	// We send the gateway's own address as origin to satisfy the check.
	headers := http.Header{}
	headers.Set("Origin", fmt.Sprintf("http://%s:18789", ipv4))
	conn, _, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	defer conn.Close()

	// Step 1: Read connect.challenge event
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read challenge: %w", err)
	}

	var event struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(msg, &event); err != nil {
		return nil, fmt.Errorf("parse challenge: %w", err)
	}
	if event.Type != "event" || event.Event != "connect.challenge" {
		return nil, fmt.Errorf("expected connect.challenge, got %s/%s", event.Type, event.Event)
	}

	// Step 2: Send connect request (OpenClaw native protocol)
	connectReq := map[string]any{
		"type":   "req",
		"id":     "connect",
		"method": "connect",
		"params": map[string]any{
			"minProtocol": 3,
			"maxProtocol": 3,
			"client": map[string]any{
				"id":       "openclaw-control-ui",
				"version":  "1.0",
				"platform": "linux",
				"mode":     "webchat",
			},
			"auth": map[string]any{
				"token": authToken,
			},
			"role":   "operator",
			"scopes": []string{"operator.read", "operator.write", "operator.admin", "operator.approvals", "operator.pairing"},
			"caps":   []string{"tool-events"},
		},
	}
	if err := conn.WriteJSON(connectReq); err != nil {
		return nil, fmt.Errorf("send connect: %w", err)
	}

	// Step 3: Read connect response
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	_, msg, err = conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read connect response: %w", err)
	}

	var connectResp struct {
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		OK      bool            `json:"ok"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(msg, &connectResp); err != nil {
		return nil, fmt.Errorf("parse connect response: %w", err)
	}
	if !connectResp.OK {
		return nil, fmt.Errorf("connect error: %s", string(connectResp.Error))
	}

	// Step 4: Send the actual RPC method
	rpcID := uuid.New().String()
	rpcReq := map[string]any{
		"type":   "req",
		"id":     rpcID,
		"method": method,
		"params": params,
	}
	if err := conn.WriteJSON(rpcReq); err != nil {
		return nil, fmt.Errorf("send rpc: %w", err)
	}

	// Step 5: Read RPC response (skip events)
	deadline := time.Now().Add(35 * time.Second)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	for {
		_, msg, err = conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("read rpc response: %w", err)
		}

		var resp struct {
			Type    string          `json:"type"`
			ID      string          `json:"id"`
			OK      bool            `json:"ok"`
			Payload json.RawMessage `json:"payload"`
			Error   json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue // skip malformed messages
		}

		// Skip events, wait for our response
		if resp.Type == "event" {
			continue
		}
		if resp.Type == "res" && resp.ID == rpcID {
			if !resp.OK {
				return nil, fmt.Errorf("rpc error: %s", string(resp.Error))
			}
			return resp.Payload, nil
		}
	}
}

// buildConfigSyncScript returns the inline config-sync script run via SSH.
// It downloads the latest heartbeat script from the backend API so that
// old VPSes get updated heartbeat code on every sync.
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
grep -v -E '_API_KEY=' /opt/openclaw/.env > /opt/openclaw/.env.tmp
[ -n "$NEW_OR_KEY" ] && echo "OPENROUTER_API_KEY=$NEW_OR_KEY" >> /opt/openclaw/.env.tmp
[ -n "$NEW_AN_KEY" ] && echo "ANTHROPIC_API_KEY=$NEW_AN_KEY" >> /opt/openclaw/.env.tmp
[ -n "$NEW_OA_KEY" ] && echo "OPENAI_API_KEY=$NEW_OA_KEY" >> /opt/openclaw/.env.tmp
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
    # on startup. Config changes must go through config.patch RPC.
    cd /opt/openclaw && docker compose up -d --force-recreate openclaw-gateway

    # Wait for healthy, then apply post-startup config before reporting completion.
    # OpenClaw takes ~70s to start listening; Docker healthcheck has start_period=60s
    # + interval=30s. Wait up to 120s (24 x 5s) to cover slow starts.
    HEALTHY=false
    for i in $(seq 1 24); do
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
    # Model primary is set by config.patch RPC from the backend (atomic, no
    # restart). As a fallback in case the RPC failed, also set it here via CLI.
    # Only "config set" (not "models set") — avoids the primary flip-flop side
    # effect of model registration.
    if [ -n "$NEW_MODEL" ]; then
        if [ "$NEW_PROVIDER" = "openrouter" ]; then
            FULL_MODEL="openrouter/${NEW_MODEL}"
        else
            FULL_MODEL="${NEW_MODEL}"
        fi
        CURRENT_PRIMARY=$(docker exec openclaw-gateway cat /tmp/openclaw/openclaw.json 2>/dev/null | jq -r '.agents.defaults.model.primary // empty' 2>/dev/null || true)
        if [ -z "$CURRENT_PRIMARY" ]; then
            CURRENT_PRIMARY=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null | jq -r '.agents.defaults.model.primary // empty' 2>/dev/null || true)
        fi
        if [ "$CURRENT_PRIMARY" != "$FULL_MODEL" ]; then
            docker exec openclaw-gateway openclaw config set agents.defaults.model.primary "$FULL_MODEL" 2>/dev/null || true
            echo "model primary set to $FULL_MODEL (was $CURRENT_PRIMARY)"
        fi
    fi

    # Write Google OAuth credential files for gog CLI
    GOG_DIR="/opt/openclaw/data/gogcli"
    if [ -n "$NEW_GOOGLE_TOKEN" ] && [ -n "$NEW_GOOGLE_EMAIL" ]; then
        mkdir -p "$GOG_DIR/tokens"
        [ -n "$NEW_GOOGLE_CLIENT" ] && printf '%s' "$NEW_GOOGLE_CLIENT" | base64 -d > "$GOG_DIR/credentials.json"
        printf '%s' "$NEW_GOOGLE_TOKEN" | base64 -d > "$GOG_DIR/tokens/${NEW_GOOGLE_EMAIL}.json"
        chown -R 1000:1000 "$GOG_DIR"
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

    echo "config sync complete (version=$REMOTE_VERSION)"
else
    echo "ERROR: container did not become healthy after recreate"
    exit 1
fi
`
}

// buildHermesConfigSyncScript returns the config-sync script for Hermes instances.
// It fetches config from the Tardi API and writes to .env, config.yaml, SOUL.md,
// then restarts the container if env vars changed.
func buildHermesConfigSyncScript() string {
	return "#!/bin/bash\n" +
		"set -euo pipefail\n" +
		"\n" +
		"export API_URL=$(grep '^API_URL=' /opt/hermes/.env | cut -d= -f2-)\n" +
		"export AGENT_TOKEN=$(grep '^AGENT_TOKEN=' /opt/hermes/.env | cut -d= -f2-)\n" +
		"\n" +
		"# Update heartbeat script\n" +
		"curl -sf -H \"Authorization: Bearer ${AGENT_TOKEN}\" \"${API_URL}/api/agent/heartbeat-script\" -o /opt/hermes/heartbeat.sh\n" +
		"chmod +x /opt/hermes/heartbeat.sh\n" +
		"\n" +
		"CONFIG=$(curl -sf \"${API_URL}/api/agent/config\" -H \"Authorization: Bearer ${AGENT_TOKEN}\")\n" +
		"if [ $? -ne 0 ] || [ -z \"$CONFIG\" ]; then\n" +
		"    echo 'ERROR: failed to fetch config from API'\n" +
		"    exit 1\n" +
		"fi\n" +
		"\n" +
		"NEW_OR_KEY=$(echo \"$CONFIG\" | jq -r '.config.openrouter_api_key // empty')\n" +
		"NEW_AN_KEY=$(echo \"$CONFIG\" | jq -r '.config.anthropic_api_key // empty')\n" +
		"NEW_OA_KEY=$(echo \"$CONFIG\" | jq -r '.config.openai_api_key // empty')\n" +
		"NEW_PROVIDER=$(echo \"$CONFIG\" | jq -r '.config.provider // \"openrouter\"')\n" +
		"NEW_MODEL=$(echo \"$CONFIG\" | jq -r '.config.model // empty')\n" +
		"REMOTE_VERSION=$(echo \"$CONFIG\" | jq -r '.version // 0')\n" +
		"\n" +
		"cp /opt/hermes/.env /opt/hermes/.env.bak\n" +
		"grep -v -E '_API_KEY=' /opt/hermes/.env > /opt/hermes/.env.tmp\n" +
		"[ -n \"$NEW_OR_KEY\" ] && echo \"OPENROUTER_API_KEY=$NEW_OR_KEY\" >> /opt/hermes/.env.tmp\n" +
		"[ -n \"$NEW_AN_KEY\" ] && echo \"ANTHROPIC_API_KEY=$NEW_AN_KEY\" >> /opt/hermes/.env.tmp\n" +
		"[ -n \"$NEW_OA_KEY\" ] && echo \"OPENAI_API_KEY=$NEW_OA_KEY\" >> /opt/hermes/.env.tmp\n" +
		"mv /opt/hermes/.env.tmp /opt/hermes/.env\n" +
		"chmod 600 /opt/hermes/.env\n" +
		"\n" +
		"# Update config.yaml with new model\n" +
		"if [ -n \"$NEW_MODEL\" ]; then\n" +
		"    cat > /opt/hermes/data/config.yaml <<CFGEOF\n" +
		"model:\n" +
		"  default: \"${NEW_PROVIDER}/${NEW_MODEL}\"\n" +
		"terminal:\n" +
		"  backend: docker\n" +
		"api_server:\n" +
		"  enabled: true\n" +
		"  host: \"0.0.0.0\"\n" +
		"  port: 8642\n" +
		"CFGEOF\n" +
		"    chown 1000:1000 /opt/hermes/data/config.yaml\n" +
		"fi\n" +
		"\n" +
		"# Update SOUL.md if provided\n" +
		"NEW_SOUL=$(echo \"$CONFIG\" | jq -r '.config.soul_md // empty')\n" +
		"if [ -n \"$NEW_SOUL\" ]; then\n" +
		"    echo \"$NEW_SOUL\" > /opt/hermes/data/SOUL.md\n" +
		"    chown 1000:1000 /opt/hermes/data/SOUL.md\n" +
		"fi\n" +
		"\n" +
		"ENV_CHANGED=false\n" +
		"if ! diff -q /opt/hermes/.env /opt/hermes/.env.bak >/dev/null 2>&1; then\n" +
		"    ENV_CHANGED=true\n" +
		"fi\n" +
		"rm -f /opt/hermes/.env.bak\n" +
		"\n" +
		"if [ \"$ENV_CHANGED\" = true ]; then\n" +
		"    cd /opt/hermes && docker compose up -d --force-recreate hermes-agent\n" +
		"    HEALTHY=false\n" +
		"    for i in $(seq 1 24); do\n" +
		"        sleep 5\n" +
		"        if curl -sf http://localhost:8642/health >/dev/null 2>&1; then\n" +
		"            HEALTHY=true\n" +
		"            break\n" +
		"        fi\n" +
		"    done\n" +
		"else\n" +
		"    echo 'env unchanged, skipping container recreate'\n" +
		"    # Restart anyway to pick up config.yaml/SOUL.md changes\n" +
		"    cd /opt/hermes && docker compose restart hermes-agent\n" +
		"fi\n" +
		"\n" +
		"echo \"$REMOTE_VERSION\" > /opt/hermes/.config_version\n"
}

// patchModelConfig uses OpenClaw's config.patch RPC to atomically set the
// primary model AND register all catalog models in one call. This avoids
// the CLI "openclaw models set" loop which changes the primary as a side
// effect of each registration, causing race conditions with the RPC.
// config.patch applies live (no restart) and persists to the JSON file.
//
// Retries up to 3 times with backoff because the gateway may be briefly
// unreachable during a reload.
func patchModelConfig(ctx context.Context, ipv4, authToken, provider, model string, allModelIDs []string) error {
	fullModel := model
	if provider == "openrouter" {
		fullModel = "openrouter/" + model
	}

	// Build the models map for registration in OC dashboard dropdown
	modelsMap := map[string]any{}
	for _, mid := range allModelIDs {
		fmid := mid
		if provider == "openrouter" {
			fmid = "openrouter/" + mid
		}
		modelsMap[fmid] = map[string]any{}
	}

	patch := map[string]any{
		"agents": map[string]any{
			"defaults": map[string]any{
				"model": map[string]any{
					"primary": fullModel,
				},
				"models": modelsMap,
			},
		},
	}
	patchJSON, _ := json.Marshal(patch)

	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		if attempt > 1 {
			slog.Info("patchModelConfig: retrying", "attempt", attempt, "ip", ipv4)
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}

		// Get the current config hash (required by config.patch)
		getResult, err := openclawRPC(ctx, ipv4, authToken, "config.get", map[string]any{})
		if err != nil {
			lastErr = fmt.Errorf("config.get (attempt %d): %w", attempt, err)
			continue
		}

		var configResp struct {
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal(getResult, &configResp); err != nil || configResp.Hash == "" {
			lastErr = fmt.Errorf("config.get: missing hash (attempt %d)", attempt)
			continue
		}

		// Single atomic patch: set primary + register all models.
		_, err = openclawRPC(ctx, ipv4, authToken, "config.patch", map[string]any{
			"raw":      string(patchJSON),
			"baseHash": configResp.Hash,
		})
		if err != nil {
			lastErr = fmt.Errorf("config.patch (attempt %d): %w", attempt, err)
			continue
		}
		return nil
	}
	return lastErr
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

		// For OpenClaw: patch the model via config.patch RPC before SSH script.
		// Hermes uses file-based config — no RPC needed.
		if inst.Framework != models.FrameworkHermes {
			if inst.OpenClawAuthToken != nil && *inst.OpenClawAuthToken != "" {
				agentCfg, cfgErr := db.GetAgentConfigByInstanceID(r.Context(), deps.Pool, inst.ID)
				if cfgErr == nil && agentCfg != nil {
					model, _ := agentCfg.Config["model"].(string)
					provider, _ := agentCfg.Config["provider"].(string)
					if model != "" {
						var modelIDs []string
						if allModels, mErr := db.ListEnabledModels(r.Context(), deps.Pool); mErr == nil {
							for _, m := range allModels {
								modelIDs = append(modelIDs, m.ID)
							}
						}
						if err := patchModelConfig(r.Context(), *inst.IPv4, *inst.OpenClawAuthToken, provider, model, modelIDs); err != nil {
							slog.Warn("sync config: model RPC patch failed (script will handle it)",
								"error", err, "instance_id", instanceID)
						} else {
							slog.Info("sync config: model+catalog patched via RPC", "model", model, "instance_id", instanceID)
						}
					}
				}
			}
		}

		// Deploy the script and launch it in the background.
		var syncScript string
		if inst.Framework == models.FrameworkHermes {
			syncScript = buildHermesConfigSyncScript()
		} else {
			syncScript = buildConfigSyncScript()
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(syncScript))
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
		// Default to "unknown" (not "running") so that collected/missing transient
		// units don't falsely report a sync in progress — this caused the
		// "Applying Config" flash on hard page refresh (Cmd+Shift+R).
		status := "unknown"
		message := "No sync in progress"
		if strings.Contains(out, `"state":"active"`) || strings.Contains(out, `"state":"activating"`) {
			status = "running"
			message = "Config sync in progress"
		} else if strings.Contains(out, "config sync complete") {
			status = "completed"
			message = "Configuration applied successfully"
		} else if strings.Contains(out, `"state":"inactive"`) {
			status = "completed"
			message = "Sync finished"
		} else if strings.Contains(out, `"state":"failed"`) {
			status = "failed"
			message = "Config sync failed on your agent"
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"status":  status,
			"message": message,
			"raw":     out,
		})
	}
}

