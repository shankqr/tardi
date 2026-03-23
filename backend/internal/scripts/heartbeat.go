package scripts

// HeartbeatScript is the bash script that runs on each VPS every 5 minutes
// via a systemd timer. It sends health status to the Tardi API, syncs config
// when the version changes, handles OpenClaw version updates, and guards
// against Telegram config drift (which causes double replies).
//
// This constant is the single source of truth — used by both the cloud-init
// template (provisioner.go) and the SSH sync script (sync.go) to ensure
// all VPSes run the latest heartbeat code.
const HeartbeatScript = `#!/bin/bash
source /opt/openclaw/.env

# --- Fix SSH password auth (cloud-init's 50-cloud-init.conf disables it) ---
# This is idempotent — only writes if the drop-in is missing or wrong
if [ ! -f /etc/ssh/sshd_config.d/60-tardi.conf ] || ! grep -q "PasswordAuthentication yes" /etc/ssh/sshd_config.d/60-tardi.conf 2>/dev/null; then
    mkdir -p /etc/ssh/sshd_config.d
    echo "PasswordAuthentication yes" > /etc/ssh/sshd_config.d/60-tardi.conf
    systemctl restart sshd 2>/dev/null || systemctl restart ssh 2>/dev/null || true
fi

# --- Migrate from old 2-container Caddy setup to single container host networking ---
# If docker-compose.yml still has the caddy service or bridge network, rewrite it.
if grep -q 'openclaw-net\|caddy:' /opt/openclaw/docker-compose.yml 2>/dev/null; then
    CURRENT_IMAGE=$(grep 'image:.*openclaw/openclaw' /opt/openclaw/docker-compose.yml | sed 's/.*image: *//' | tr -d ' ')
    [ -z "$CURRENT_IMAGE" ] && CURRENT_IMAGE="ghcr.io/openclaw/openclaw:latest"
    DOCKER_GID=$(getent group docker | cut -d: -f3)
    cat > /opt/openclaw/docker-compose.yml <<MIGRATEEOF
services:
  openclaw-gateway:
    image: ${CURRENT_IMAGE}
    container_name: openclaw-gateway
    restart: unless-stopped
    network_mode: host
    user: "1000:1000"
    group_add:
      - "${DOCKER_GID}"
    volumes:
      - ./data/openclaw:/home/node/.openclaw:rw
      - /var/run/docker.sock:/var/run/docker.sock
    env_file:
      - .env
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:18789/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 60s

MIGRATEEOF
    # Set up iptables NAT if not already present
    if ! iptables -t nat -C PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789 2>/dev/null; then
        iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789
        netfilter-persistent save 2>/dev/null || true
    fi
    # Remove trustedProxies from openclaw.json (no longer behind reverse proxy)
    python3 -c "
import json
with open('/opt/openclaw/data/openclaw/openclaw.json') as f:
    cfg = json.load(f)
cfg.get('gateway', {}).pop('trustedProxies', None)
with open('/opt/openclaw/data/openclaw/openclaw.json', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null || true
    # Stop old stack (including caddy) and start new single-container stack
    cd /opt/openclaw && docker compose down 2>/dev/null; docker compose up -d 2>/dev/null || true
    # Clean up old Caddy files
    rm -f /opt/openclaw/Caddyfile
    rm -rf /opt/openclaw/certs /opt/openclaw/caddy
fi

# Check OpenClaw gateway health
HEALTH=$(curl -sf http://localhost:18789/health 2>/dev/null)
if [ $? -eq 0 ]; then
    STATUS="running"
else
    # Check if container exists but is unhealthy
    CONTAINER_STATE=$(docker inspect -f '{{.State.Status}}' openclaw-gateway 2>/dev/null)
    if [ "$CONTAINER_STATE" = "running" ]; then
        STATUS="unhealthy"
    elif [ -n "$CONTAINER_STATE" ]; then
        STATUS="stopped"
    else
        STATUS="not_found"
    fi
fi

# Detect current running OpenClaw version (image tag)
CURRENT_IMAGE=$(docker inspect --format='{{.Config.Image}}' openclaw-gateway 2>/dev/null)
CURRENT_TAG=$(echo "$CURRENT_IMAGE" | sed 's/.*://' | tr -d '[:space:]')
[ -z "$CURRENT_TAG" ] && CURRENT_TAG="unknown"

# Read update status if mid-update
UPDATE_STATUS=$(cat /opt/openclaw/.update_status 2>/dev/null || echo "")
UPDATE_ERROR=$(cat /opt/openclaw/.update_error 2>/dev/null || echo "")

# Send heartbeat with version info and capture response
RESPONSE=$(curl -sf -X POST "${API_URL}/api/agent/heartbeat" \
    -H "Authorization: Bearer ${AGENT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"status\":\"${STATUS}\",\"openclaw_version\":\"${CURRENT_TAG}\",\"openclaw_update_status\":\"${UPDATE_STATUS}\",\"openclaw_update_error\":\"${UPDATE_ERROR}\"}" 2>/dev/null)

# --- Telegram config drift guard (runs every heartbeat) ---
# OpenClaw resets Telegram config to bad defaults on every container restart
# (Docker auto-restarts from crashes/OOM bypass all other config-sync paths).
# Check actual config and fix if drifted. Cost: 1 cat+jq when config is fine.
TG_TOKEN_SET=$(grep -c '^TELEGRAM_BOT_TOKEN=.\+' /opt/openclaw/.env 2>/dev/null || echo "0")
if [ "$TG_TOKEN_SET" -gt 0 ] && [ "$STATUS" = "running" ]; then
    TG_CONFIG=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null)
    TG_STREAMING=$(echo "$TG_CONFIG" | jq -r '.channels.telegram.streaming // "unknown"' 2>/dev/null)
    TG_ENABLED=$(echo "$TG_CONFIG" | jq -r '.channels.telegram.enabled // false' 2>/dev/null)
    if [ "$TG_STREAMING" != "off" ] || [ "$TG_ENABLED" != "true" ]; then
        docker exec openclaw-gateway openclaw config set channels.telegram.enabled true 2>/dev/null
        docker exec openclaw-gateway openclaw config set channels.telegram.streaming off 2>/dev/null
        docker exec openclaw-gateway openclaw config set channels.telegram.allowFrom '["*"]' 2>/dev/null
        docker exec openclaw-gateway openclaw config set channels.telegram.dmPolicy open 2>/dev/null
        docker exec openclaw-gateway openclaw config set channels.telegram.groupPolicy disabled 2>/dev/null
    fi
fi

# --- Model drift guard (runs every heartbeat) ---
# OpenClaw loses the model setting on container restart (Docker auto-restarts
# from crashes/OOM). Re-apply the model from saved config if it's missing.
if [ "$STATUS" = "running" ]; then
    MODEL_OUT=$(docker exec openclaw-gateway openclaw models list 2>&1 || echo "")
    MODEL_LINES=$(echo "$MODEL_OUT" | wc -l | tr -d ' ')
    if [ "$MODEL_LINES" -le 1 ]; then
        # No model set — fetch from API and re-apply
        SAVED_CFG=$(curl -sf "${API_URL}/api/agent/config" \
            -H "Authorization: Bearer ${AGENT_TOKEN}" 2>/dev/null)
        SAVED_MODEL=$(echo "$SAVED_CFG" | jq -r '.config.model // empty' 2>/dev/null)
        SAVED_PROVIDER=$(echo "$SAVED_CFG" | jq -r '.config.provider // empty' 2>/dev/null)
        if [ -n "$SAVED_MODEL" ]; then
            if [ "$SAVED_PROVIDER" = "openrouter" ]; then
                docker exec openclaw-gateway openclaw models set "openrouter/${SAVED_MODEL}" 2>/dev/null
            else
                docker exec openclaw-gateway openclaw models set "${SAVED_MODEL}" 2>/dev/null
            fi
        fi
    fi
fi

# --- iptables NAT drift guard (runs every heartbeat) ---
# Ensure port 80 → 18789 NAT rule exists (Cloudflare Proxy connects on port 80).
# OpenClaw runs as UID 1000 and cannot bind port 80 directly.
if ! iptables -t nat -C PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789 2>/dev/null; then
    iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789
    netfilter-persistent save 2>/dev/null || true
fi

# --- Gateway auth drift guard (runs every heartbeat) ---
# OpenClaw may overwrite openclaw.json on startup and revert auth mode.
# We want "token" mode so internal tool calls authenticate via OPENCLAW_GATEWAY_TOKEN.
# IMPORTANT: This guard must NOT be gated on STATUS=running. It only edits
# openclaw.json on disk. If the container is crash-looping because auth.mode
# was reverted to "none" (which refuses to start with bind=lan), we need to
# fix the file while the container is stopped so the next restart succeeds.
GW_AUTH_MODE=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null | jq -r '.gateway.auth.mode // "unknown"' 2>/dev/null)
GW_INSECURE_AUTH=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null | jq -r '.gateway.controlUi.allowInsecureAuth // false' 2>/dev/null)
if [ "$GW_AUTH_MODE" != "token" ] || [ "$GW_INSECURE_AUTH" != "true" ]; then
    # Write the auth block and controlUi settings via python.
    # allowInsecureAuth: required for shared token auth to grant operator scopes (OC 2026.3.22+).
    # Update meta.lastTouchedAt so OpenClaw detects the change and self-reloads.
    python3 -c "
import json, datetime
with open('/opt/openclaw/data/openclaw/openclaw.json') as f:
    cfg = json.load(f)
cfg.setdefault('gateway', {})['auth'] = {'mode': 'token'}
cui = cfg['gateway'].setdefault('controlUi', {})
cui['dangerouslyDisableDeviceAuth'] = True
cui['allowInsecureAuth'] = True
if 'allowedOrigins' not in cui:
    cui['allowedOrigins'] = ['*']
cfg.setdefault('meta', {})['lastTouchedAt'] = datetime.datetime.utcnow().strftime('%Y-%m-%dT%H:%M:%S.000Z')
with open('/opt/openclaw/data/openclaw/openclaw.json', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null
    # If the container is crash-looping (stopped/restarting), restart it now
    # so it picks up the fixed config immediately instead of waiting for
    # Docker's exponential backoff.
    if [ "$STATUS" = "stopped" ] || [ "$CONTAINER_STATE" = "restarting" ]; then
        cd /opt/openclaw && docker compose up -d --force-recreate openclaw-gateway 2>/dev/null || true
    fi
fi

# Ensure OPENCLAW_GATEWAY_TOKEN is in .env (needed for token auth mode)
if ! grep -q '^OPENCLAW_GATEWAY_TOKEN=' /opt/openclaw/.env 2>/dev/null; then
    OPENCLAW_TOKEN=$(grep '^OPENCLAW_AUTH_TOKEN=' /opt/openclaw/.env | cut -d= -f2-)
    [ -n "$OPENCLAW_TOKEN" ] && echo "OPENCLAW_GATEWAY_TOKEN=$OPENCLAW_TOKEN" >> /opt/openclaw/.env
fi

# --- Ensure dangerouslyDisableDeviceAuth is set for Control UI ---
# Without this, the Control UI requires device pairing for every new browser,
# which blocks the dashboard from working when opened from the Tardi frontend.
OC_CONFIG="/opt/openclaw/data/openclaw/openclaw.json"
if [ -f "$OC_CONFIG" ]; then
    DISABLE_DEVICE_AUTH=$(python3 -c "
import json
with open('$OC_CONFIG') as f:
    cfg = json.load(f)
print(cfg.get('gateway',{}).get('controlUi',{}).get('dangerouslyDisableDeviceAuth', False))
" 2>/dev/null || echo "False")
    if [ "$DISABLE_DEVICE_AUTH" != "True" ]; then
        python3 -c "
import json
with open('$OC_CONFIG') as f:
    cfg = json.load(f)
cfg.setdefault('gateway', {}).setdefault('controlUi', {})['dangerouslyDisableDeviceAuth'] = True
with open('$OC_CONFIG', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null
        # Restart gateway to apply config change
        cd /opt/openclaw && docker compose restart openclaw-gateway 2>/dev/null || true
    fi
fi

# Check for config changes
REMOTE_VERSION=$(echo "$RESPONSE" | jq -r '.config_version // 0' 2>/dev/null)
LOCAL_VERSION=$(cat /opt/openclaw/.config_version 2>/dev/null || echo "0")

if [ "$REMOTE_VERSION" != "0" ] && [ "$REMOTE_VERSION" != "$LOCAL_VERSION" ]; then
    CONFIG=$(curl -sf "${API_URL}/api/agent/config" \
        -H "Authorization: Bearer ${AGENT_TOKEN}" 2>/dev/null)

    if [ $? -eq 0 ]; then
        NEW_OR_KEY=$(echo "$CONFIG" | jq -r '.config.openrouter_api_key // empty')
        NEW_AN_KEY=$(echo "$CONFIG" | jq -r '.config.anthropic_api_key // empty')
        NEW_OA_KEY=$(echo "$CONFIG" | jq -r '.config.openai_api_key // empty')
        NEW_TG_TOKEN=$(echo "$CONFIG" | jq -r '.config.telegram_bot_token // empty')
        NEW_PROVIDER=$(echo "$CONFIG" | jq -r '.config.provider // empty')
        NEW_MODEL=$(echo "$CONFIG" | jq -r '.config.model // empty')
        NEW_GOOGLE_CLIENT=$(echo "$CONFIG" | jq -r '.config.google_client_b64 // empty')
        NEW_GOOGLE_TOKEN=$(echo "$CONFIG" | jq -r '.config.google_token_b64 // empty')
        NEW_GOOGLE_EMAIL=$(echo "$CONFIG" | jq -r '.config.google_email // empty')

        # Rebuild .env preserving non-key/token vars
        grep -v -E '_API_KEY=|TELEGRAM_BOT_TOKEN=' /opt/openclaw/.env > /opt/openclaw/.env.tmp
        [ -n "$NEW_OR_KEY" ] && echo "OPENROUTER_API_KEY=$NEW_OR_KEY" >> /opt/openclaw/.env.tmp
        [ -n "$NEW_AN_KEY" ] && echo "ANTHROPIC_API_KEY=$NEW_AN_KEY" >> /opt/openclaw/.env.tmp
        [ -n "$NEW_OA_KEY" ] && echo "OPENAI_API_KEY=$NEW_OA_KEY" >> /opt/openclaw/.env.tmp
        [ -n "$NEW_TG_TOKEN" ] && echo "TELEGRAM_BOT_TOKEN=$NEW_TG_TOKEN" >> /opt/openclaw/.env.tmp
        mv /opt/openclaw/.env.tmp /opt/openclaw/.env
        chmod 600 /opt/openclaw/.env

        # Recreate container to pick up new env (restart does not reload env_file)
        # NOTE: Do NOT edit openclaw.json — OpenClaw owns that file and overwrites
        # it on startup. Config changes must go through openclaw CLI after healthy.
        cd /opt/openclaw && docker compose up -d --force-recreate openclaw-gateway

        # Wait for healthy, then apply post-startup config
        HEALTHY=false
        for i in $(seq 1 12); do
            sleep 5
            if docker exec openclaw-gateway curl -sf http://localhost:18789/health >/dev/null 2>&1; then
                HEALTHY=true
                break
            fi
        done

        if [ "$HEALTHY" = true ]; then
            # Fix Telegram config if bot token is set:
            # - streaming:"off" prevents double replies (OpenClaw defaults to "partial")
            # - dmPolicy:"open" + allowFrom:["*"] allows anyone to message the bot
            # - groupPolicy:"disabled" ignores group messages
            if [ -n "$NEW_TG_TOKEN" ]; then
                docker exec openclaw-gateway openclaw config set channels.telegram.enabled true 2>/dev/null
                docker exec openclaw-gateway openclaw config set channels.telegram.streaming off 2>/dev/null
                docker exec openclaw-gateway openclaw config set channels.telegram.allowFrom '["*"]' 2>/dev/null
                docker exec openclaw-gateway openclaw config set channels.telegram.dmPolicy open 2>/dev/null
                docker exec openclaw-gateway openclaw config set channels.telegram.groupPolicy disabled 2>/dev/null
            fi

            # Update default model. OpenRouter model IDs contain a slash
            # (e.g. "anthropic/claude-sonnet-4.6") which OpenClaw interprets
            # as a provider prefix. Prepend "openrouter/" so OpenClaw routes
            # through OpenRouter instead of trying the native provider directly.
            if [ -n "$NEW_MODEL" ]; then
                if [ "$NEW_PROVIDER" = "openrouter" ]; then
                    docker exec openclaw-gateway openclaw models set "openrouter/${NEW_MODEL}"
                else
                    docker exec openclaw-gateway openclaw models set "${NEW_MODEL}"
                fi
            fi

            # Write Google OAuth credential files for gog CLI
            GOG_DIR="/opt/openclaw/data/openclaw/.config/gogcli"
            if [ -n "$NEW_GOOGLE_TOKEN" ] && [ -n "$NEW_GOOGLE_EMAIL" ]; then
                mkdir -p "$GOG_DIR/tokens"
                [ -n "$NEW_GOOGLE_CLIENT" ] && echo "$NEW_GOOGLE_CLIENT" | base64 -d > "$GOG_DIR/credentials.json"
                echo "$NEW_GOOGLE_TOKEN" | base64 -d > "$GOG_DIR/tokens/${NEW_GOOGLE_EMAIL}.json"
                chmod -R 600 "$GOG_DIR"
                chmod 700 "$GOG_DIR" "$GOG_DIR/tokens"
            else
                rm -rf "$GOG_DIR"
            fi

            # Write config version only after all patches applied so we retry
            # on next heartbeat if anything above failed
            echo "$REMOTE_VERSION" > /opt/openclaw/.config_version
        fi
    fi
fi

# --- OpenClaw version update ---
TARGET_VERSION=$(echo "$RESPONSE" | jq -r '.target_openclaw_version // empty' 2>/dev/null)

if [ -n "$TARGET_VERSION" ] && [ "$TARGET_VERSION" != "$CURRENT_TAG" ] \
   && [ "$UPDATE_STATUS" != "pulling" ] && [ "$UPDATE_STATUS" != "updating" ]; then

    echo "pulling" > /opt/openclaw/.update_status
    rm -f /opt/openclaw/.update_error

    # Update docker-compose.yml to use the new tag
    sed -i "s|image: ghcr.io/openclaw/openclaw:.*|image: ghcr.io/openclaw/openclaw:${TARGET_VERSION}|" \
        /opt/openclaw/docker-compose.yml

    # Pull the new image (rollback on failure)
    if ! docker compose -f /opt/openclaw/docker-compose.yml pull openclaw-gateway 2>/tmp/openclaw-pull.log; then
        echo "failed" > /opt/openclaw/.update_status
        echo "pull failed: $(tail -1 /tmp/openclaw-pull.log)" > /opt/openclaw/.update_error
        sed -i "s|image: ghcr.io/openclaw/openclaw:.*|image: ghcr.io/openclaw/openclaw:${CURRENT_TAG}|" \
            /opt/openclaw/docker-compose.yml
        exit 0
    fi

    echo "updating" > /opt/openclaw/.update_status

    # Recreate container with new image (volume mount preserves all user data)
    if ! docker compose -f /opt/openclaw/docker-compose.yml up -d openclaw-gateway 2>/tmp/openclaw-update.log; then
        echo "failed" > /opt/openclaw/.update_status
        echo "up failed: $(tail -1 /tmp/openclaw-update.log)" > /opt/openclaw/.update_error
        sed -i "s|image: ghcr.io/openclaw/openclaw:.*|image: ghcr.io/openclaw/openclaw:${CURRENT_TAG}|" \
            /opt/openclaw/docker-compose.yml
        docker compose -f /opt/openclaw/docker-compose.yml up -d openclaw-gateway 2>/dev/null
        exit 0
    fi

    # Health check: wait up to 90 seconds (18 x 5s)
    HEALTHY=false
    for i in $(seq 1 18); do
        sleep 5
        if curl -sf http://localhost:18789/health >/dev/null 2>&1; then
            HEALTHY=true
            break
        fi
    done

    if [ "$HEALTHY" = true ]; then
        echo "completed" > /opt/openclaw/.update_status
        rm -f /opt/openclaw/.update_error

        # Re-apply Telegram config after version update — OpenClaw auto-detects
        # TELEGRAM_BOT_TOKEN on startup and resets to bad defaults (streaming:"partial",
        # restrictive dmPolicy) which causes double replies and pairing prompts.
        TG_TOKEN_SET=$(grep -c '^TELEGRAM_BOT_TOKEN=.\+' /opt/openclaw/.env 2>/dev/null || echo "0")
        if [ "$TG_TOKEN_SET" -gt 0 ]; then
            docker exec openclaw-gateway openclaw config set channels.telegram.enabled true 2>/dev/null
            docker exec openclaw-gateway openclaw config set channels.telegram.streaming off 2>/dev/null
            docker exec openclaw-gateway openclaw config set channels.telegram.allowFrom '["*"]' 2>/dev/null
            docker exec openclaw-gateway openclaw config set channels.telegram.dmPolicy open 2>/dev/null
            docker exec openclaw-gateway openclaw config set channels.telegram.groupPolicy disabled 2>/dev/null
        fi

        # Clean up old images to save disk space
        docker image prune -f >/dev/null 2>&1
    else
        # ROLLBACK: revert to previous version
        echo "failed" > /opt/openclaw/.update_status
        echo "health check failed after update" > /opt/openclaw/.update_error
        sed -i "s|image: ghcr.io/openclaw/openclaw:.*|image: ghcr.io/openclaw/openclaw:${CURRENT_TAG}|" \
            /opt/openclaw/docker-compose.yml
        docker compose -f /opt/openclaw/docker-compose.yml up -d openclaw-gateway 2>/dev/null
    fi
fi
`
