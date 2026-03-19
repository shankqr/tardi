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

            # Update default model if provider+model are set
            if [ -n "$NEW_PROVIDER" ] && [ -n "$NEW_MODEL" ]; then
                docker exec openclaw-gateway openclaw models set "${NEW_PROVIDER}/${NEW_MODEL}" 2>/dev/null
            fi
        fi

        echo "$REMOTE_VERSION" > /opt/openclaw/.config_version
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
