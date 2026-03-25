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

# --- SSH key-based auth drift guard ---
# Ensure password auth stays disabled and key auth is enforced.
# The SSH public key is injected by cloud-init (new VPSes) or ScriptPusher (existing VPSes).
if [ -f /etc/ssh/sshd_config.d/60-tardi.conf ] && grep -q "PasswordAuthentication yes" /etc/ssh/sshd_config.d/60-tardi.conf 2>/dev/null; then
    # Old config detected — only flip if authorized_keys has a key (safety check)
    if [ -s /root/.ssh/authorized_keys ]; then
        printf 'PasswordAuthentication no\nPubkeyAuthentication yes\nPermitRootLogin prohibit-password\n' > /etc/ssh/sshd_config.d/60-tardi.conf
        sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
        sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
        systemctl restart sshd 2>/dev/null || systemctl restart ssh 2>/dev/null || true
    fi
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
    # UFW 18789 rules will be set by the hardening section below (per-CIDR, not blanket allow)
    # Ensure trustedProxies is set (Cloudflare adds proxy headers; without this
    # OpenClaw treats connections as untrusted and won't grant operator scopes)
    python3 -c "
import json
with open('/opt/openclaw/data/openclaw/openclaw.json') as f:
    cfg = json.load(f)
cfg.setdefault('gateway', {})['trustedProxies'] = ['0.0.0.0/0']
with open('/opt/openclaw/data/openclaw/openclaw.json', 'w') as f:
    json.dump(cfg, f, indent=2)
" 2>/dev/null || true
    # Force-remove orphaned Caddy container (may not be in new compose file)
    docker rm -f openclaw-caddy 2>/dev/null || true
    # Stop old stack and start new single-container stack
    cd /opt/openclaw && docker compose down --remove-orphans 2>/dev/null; docker compose up -d 2>/dev/null || true
    # Clean up old Caddy files and image
    rm -f /opt/openclaw/Caddyfile
    rm -rf /opt/openclaw/certs /opt/openclaw/caddy
    docker image rm caddy:2-alpine 2>/dev/null || true
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

# Check for provider errors in recent docker logs
AGENT_ERROR=""
if [ "$STATUS" = "running" ] || [ "$STATUS" = "unhealthy" ]; then
    RECENT_LOGS=$(docker logs openclaw-gateway --tail 100 --since 10m 2>&1)
    if echo "$RECENT_LOGS" | grep -qi "key limit exceeded"; then
        AGENT_ERROR="openrouter_credits_exhausted"
    elif echo "$RECENT_LOGS" | grep -qi "invalid.*api.*key\|authentication.*failed"; then
        AGENT_ERROR="invalid_api_key"
    fi
fi

# Send heartbeat with version info and capture response
RESPONSE=$(curl -sf -X POST "${API_URL}/api/agent/heartbeat" \
    -H "Authorization: Bearer ${AGENT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"status\":\"${STATUS}\",\"openclaw_version\":\"${CURRENT_TAG}\",\"openclaw_update_status\":\"${UPDATE_STATUS}\",\"openclaw_update_error\":\"${UPDATE_ERROR}\",\"agent_error\":\"${AGENT_ERROR}\"}" 2>/dev/null)

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
# from crashes/OOM). Re-apply models from saved config if missing.
# Also registers all Tardi catalog models so the OC dashboard dropdown matches.
if [ "$STATUS" = "running" ]; then
    MODEL_OUT=$(docker exec openclaw-gateway openclaw models list 2>&1 || echo "")
    MODEL_LINES=$(echo "$MODEL_OUT" | wc -l | tr -d ' ')
    if [ "$MODEL_LINES" -le 1 ]; then
        # No model set — fetch from API and re-apply all models
        SAVED_CFG=$(curl -sf "${API_URL}/api/agent/config" \
            -H "Authorization: Bearer ${AGENT_TOKEN}" 2>/dev/null)
        SAVED_MODEL=$(echo "$SAVED_CFG" | jq -r '.config.model // empty' 2>/dev/null)
        SAVED_PROVIDER=$(echo "$SAVED_CFG" | jq -r '.config.provider // empty' 2>/dev/null)
        ALL_MIDS=$(echo "$SAVED_CFG" | jq -r '.model_ids // [] | .[]' 2>/dev/null)
        # Register non-active models first
        if [ -n "$ALL_MIDS" ]; then
            for MID in $ALL_MIDS; do
                [ "$MID" = "$SAVED_MODEL" ] && continue
                if [ "$SAVED_PROVIDER" = "openrouter" ]; then
                    docker exec openclaw-gateway openclaw models set "openrouter/${MID}" 2>/dev/null
                else
                    docker exec openclaw-gateway openclaw models set "${MID}" 2>/dev/null
                fi
            done
        fi
        # Set active model last and set it as the primary model.
        # "openclaw models set" only registers it — "config set" makes it primary.
        if [ -n "$SAVED_MODEL" ]; then
            if [ "$SAVED_PROVIDER" = "openrouter" ]; then
                docker exec openclaw-gateway openclaw models set "openrouter/${SAVED_MODEL}" 2>/dev/null
                docker exec openclaw-gateway openclaw config set agents.defaults.model.primary "openrouter/${SAVED_MODEL}" 2>/dev/null
            else
                docker exec openclaw-gateway openclaw models set "${SAVED_MODEL}" 2>/dev/null
                docker exec openclaw-gateway openclaw config set agents.defaults.model.primary "${SAVED_MODEL}" 2>/dev/null
            fi
        fi
    fi
fi

# --- Remove orphaned Caddy container if still running ---
if docker ps -q -f name=openclaw-caddy 2>/dev/null | grep -q .; then
    docker rm -f openclaw-caddy 2>/dev/null || true
    docker image rm caddy:2-alpine 2>/dev/null || true
fi

# --- iptables NAT drift guard (runs every heartbeat) ---
# Ensure port 80 → 18789 NAT rule exists (Cloudflare Proxy connects on port 80).
# OpenClaw runs as UID 1000 and cannot bind port 80 directly.
if ! iptables -t nat -C PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789 2>/dev/null; then
    iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789
    netfilter-persistent save 2>/dev/null || true
fi

# --- UFW security hardening for port 18789 and SSH ---
# Port 18789 must be allowed (iptables PREROUTING rewrites dest port before UFW
# INPUT chain), but we restrict it to Cloudflare IPs + backend egress CIDRs
# instead of allowing from all sources. This prevents direct access to OpenClaw
# from arbitrary IPs that would bypass Cloudflare proxy/WAF/DDoS protection.
#
# Migration: if blanket "18789/tcp ALLOW Anywhere" exists, replace it with
# per-CIDR rules. Cloudflare IPs are refreshed daily via a marker file.
BACKEND_CIDRS=$(grep '^BACKEND_EGRESS_CIDRS=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2-)

# Check if blanket 18789 allow still exists (pre-hardening or drift)
if ufw status | grep "18789/tcp" | grep -q "Anywhere" 2>/dev/null; then
    UFW_NEEDS_HARDENING=true
elif ! ufw status | grep -q "18789" 2>/dev/null; then
    # No 18789 rules at all — need to add them
    UFW_NEEDS_HARDENING=true
else
    UFW_NEEDS_HARDENING=false
fi

# Refresh Cloudflare IP allowlist daily (or on first run / after hardening)
CF_MARKER="/opt/openclaw/.cf_ufw_updated"
CF_STALE=false
if [ ! -f "$CF_MARKER" ]; then
    CF_STALE=true
elif [ -n "$(find "$CF_MARKER" -mmin +1440 2>/dev/null)" ]; then
    CF_STALE=true
fi

if [ "$UFW_NEEDS_HARDENING" = true ] || [ "$CF_STALE" = true ]; then
    # Remove blanket allow rules (safe to run even if they don't exist)
    ufw delete allow 18789/tcp 2>/dev/null || true

    # Add Cloudflare IP ranges for port 18789
    CF_IPS=$(curl -sf https://www.cloudflare.com/ips-v4 2>/dev/null || echo "")
    if [ -n "$CF_IPS" ]; then
        for cidr in $CF_IPS; do
            ufw allow from $cidr to any port 18789 2>/dev/null || true
        done
        date > "$CF_MARKER"
    fi

    # Add backend egress CIDRs for port 18789 + SSH
    if [ -n "$BACKEND_CIDRS" ]; then
        ufw delete allow 22/tcp 2>/dev/null || true
        for cidr in $(echo "$BACKEND_CIDRS" | tr ',' ' '); do
            ufw allow from $cidr to any port 18789 2>/dev/null || true
            ufw allow from $cidr to any port 22 2>/dev/null || true
        done
    fi
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
GW_TRUSTED_PROXIES=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null | jq -r '.gateway.trustedProxies // empty' 2>/dev/null)
if [ "$GW_AUTH_MODE" != "token" ] || [ "$GW_INSECURE_AUTH" != "true" ] || [ -z "$GW_TRUSTED_PROXIES" ]; then
    # Write the auth block, controlUi settings, and trustedProxies via python.
    # trustedProxies: Cloudflare adds X-Forwarded-For; without this OpenClaw sees
    #   "untrusted proxy" and won't grant operator scopes (operator.read error).
    # allowInsecureAuth: required for shared token auth to grant operator scopes (OC 2026.3.22+).
    # Update meta.lastTouchedAt so OpenClaw detects the change and self-reloads.
    python3 -c "
import json, datetime
with open('/opt/openclaw/data/openclaw/openclaw.json') as f:
    cfg = json.load(f)
cfg.setdefault('gateway', {})['auth'] = {'mode': 'token'}
cfg['gateway']['trustedProxies'] = ['0.0.0.0/0']
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

# Sync OPENCLAW_GATEWAY_TOKEN in .env with the actual token OpenClaw is using.
# Read from the running container's env (authoritative source), falling back to
# openclaw.json if the container isn't running.
ACTUAL_GW_TOKEN=$(docker exec openclaw-gateway printenv OPENCLAW_GATEWAY_TOKEN 2>/dev/null || true)
[ -z "$ACTUAL_GW_TOKEN" ] && ACTUAL_GW_TOKEN=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null | jq -r '.gateway.auth.token // empty' 2>/dev/null)
ENV_GW_TOKEN=$(grep '^OPENCLAW_GATEWAY_TOKEN=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2-)
if [ -n "$ACTUAL_GW_TOKEN" ] && [ "$ACTUAL_GW_TOKEN" != "$ENV_GW_TOKEN" ]; then
    sed -i '/^OPENCLAW_GATEWAY_TOKEN=/d' /opt/openclaw/.env
    echo "OPENCLAW_GATEWAY_TOKEN=$ACTUAL_GW_TOKEN" >> /opt/openclaw/.env
    # Keep OPENCLAW_AUTH_TOKEN in sync (same value, used by nothing on VPS but avoids confusion)
    sed -i '/^OPENCLAW_AUTH_TOKEN=/d' /opt/openclaw/.env
    echo "OPENCLAW_AUTH_TOKEN=$ACTUAL_GW_TOKEN" >> /opt/openclaw/.env
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
        # credentials are files on the host volume — no restart needed.
        ENV_CHANGED=false
        if ! diff -q /opt/openclaw/.env /opt/openclaw/.env.bak >/dev/null 2>&1; then
            ENV_CHANGED=true
        fi
        rm -f /opt/openclaw/.env.bak

        if [ "$ENV_CHANGED" = true ]; then
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
        else
            HEALTHY=true
        fi

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

            # Register all Tardi catalog models so OC dashboard dropdown matches.
            # OpenRouter model IDs contain a slash (e.g. "anthropic/claude-sonnet-4.6")
            # which OpenClaw interprets as a provider prefix. Prepend "openrouter/" so
            # OpenClaw routes through OpenRouter instead of the native provider.
            # Register non-active models first, then set active model last.
            if [ -n "$ALL_MODEL_IDS" ]; then
                for MID in $ALL_MODEL_IDS; do
                    [ "$MID" = "$NEW_MODEL" ] && continue
                    if [ "$NEW_PROVIDER" = "openrouter" ]; then
                        docker exec openclaw-gateway openclaw models set "openrouter/${MID}" 2>/dev/null
                    else
                        docker exec openclaw-gateway openclaw models set "${MID}" 2>/dev/null
                    fi
                done
            fi
            if [ -n "$NEW_MODEL" ]; then
                if [ "$NEW_PROVIDER" = "openrouter" ]; then
                    docker exec openclaw-gateway openclaw models set "openrouter/${NEW_MODEL}" 2>/dev/null
                    docker exec openclaw-gateway openclaw config set agents.defaults.model.primary "openrouter/${NEW_MODEL}" 2>/dev/null
                else
                    docker exec openclaw-gateway openclaw models set "${NEW_MODEL}" 2>/dev/null
                    docker exec openclaw-gateway openclaw config set agents.defaults.model.primary "${NEW_MODEL}" 2>/dev/null
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
