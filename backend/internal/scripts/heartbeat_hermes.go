package scripts

// HermesHeartbeatScript is the bash script that runs on each Hermes VPS every
// 5 minutes via a systemd timer. It reports health status to the Tardi API,
// syncs config when the version changes, and guards against drift.
//
// Hermes runs as a native systemd service (hermes-agent.service), NOT Docker.
const HermesHeartbeatScript = `#!/bin/bash
source /opt/hermes/.env

# --- SSH key-based auth drift guard ---
if [ -f /etc/ssh/sshd_config.d/60-tardi.conf ] && grep -q "PasswordAuthentication yes" /etc/ssh/sshd_config.d/60-tardi.conf 2>/dev/null; then
    if [ -s /root/.ssh/authorized_keys ]; then
        printf 'PasswordAuthentication no\nPubkeyAuthentication yes\nPermitRootLogin prohibit-password\n' > /etc/ssh/sshd_config.d/60-tardi.conf
        sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
        sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
        systemctl restart sshd 2>/dev/null || systemctl restart ssh 2>/dev/null || true
    fi
fi

# Check Hermes health via API server
HEALTH=$(curl -sf http://localhost:8642/health 2>/dev/null)
if [ $? -eq 0 ]; then
    STATUS="running"
else
    # Check if systemd service is active
    SVC_STATE=$(systemctl is-active hermes-agent 2>/dev/null)
    if [ "$SVC_STATE" = "active" ]; then
        STATUS="unhealthy"
    elif [ "$SVC_STATE" = "inactive" ] || [ "$SVC_STATE" = "failed" ]; then
        STATUS="stopped"
    else
        STATUS="not_found"
    fi
fi

# Detect current Hermes version
CURRENT_TAG="unknown"
HERMES_BIN=$(su - hermes -c 'which hermes' 2>/dev/null || echo "")
if [ -n "$HERMES_BIN" ]; then
    CURRENT_TAG=$(su - hermes -c 'hermes --version 2>/dev/null' | head -1 | sed 's/[^0-9.]//g')
    [ -z "$CURRENT_TAG" ] && CURRENT_TAG="unknown"
fi

# Read update status if mid-update
UPDATE_STATUS=$(cat /opt/hermes/.update_status 2>/dev/null || echo "")
UPDATE_ERROR=$(cat /opt/hermes/.update_error 2>/dev/null || echo "")

# Check for errors in recent journal logs
AGENT_ERROR=""
if [ "$STATUS" = "running" ] || [ "$STATUS" = "unhealthy" ]; then
    RECENT_LOGS=$(journalctl -u hermes-agent --no-pager -n 100 --since "10 min ago" 2>&1)
    if echo "$RECENT_LOGS" | grep -qi "key limit exceeded"; then
        AGENT_ERROR="openrouter_credits_exhausted"
    elif echo "$RECENT_LOGS" | grep -qi "invalid.*api.*key\|authentication.*failed"; then
        AGENT_ERROR="invalid_api_key"
    fi
fi

# Send heartbeat and capture response
RESPONSE=$(curl -sf -X POST "${API_URL}/api/agent/heartbeat" \
    -H "Authorization: Bearer ${AGENT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"status\":\"${STATUS}\",\"openclaw_version\":\"${CURRENT_TAG}\",\"openclaw_update_status\":\"${UPDATE_STATUS}\",\"openclaw_update_error\":\"${UPDATE_ERROR}\",\"agent_error\":\"${AGENT_ERROR}\"}" 2>/dev/null)

# --- Sync PREVIEW_DOMAIN from heartbeat response ---
API_PREVIEW_DOMAIN=$(echo "$RESPONSE" | jq -r '.preview_domain // empty' 2>/dev/null)
if [ -n "$API_PREVIEW_DOMAIN" ]; then
    CURRENT_PREVIEW=$(grep '^PREVIEW_DOMAIN=' /opt/hermes/.env 2>/dev/null | cut -d= -f2)
    if [ "$API_PREVIEW_DOMAIN" != "$CURRENT_PREVIEW" ]; then
        if grep -q '^PREVIEW_DOMAIN=' /opt/hermes/.env 2>/dev/null; then
            sed -i "s|^PREVIEW_DOMAIN=.*|PREVIEW_DOMAIN=${API_PREVIEW_DOMAIN}|" /opt/hermes/.env
        else
            echo "PREVIEW_DOMAIN=${API_PREVIEW_DOMAIN}" >> /opt/hermes/.env
        fi
    fi
fi

# --- Caddy drift guard ---
source /opt/hermes/.env 2>/dev/null
EXPECTED_CADDYFILE=""
if [ -n "${PREVIEW_DOMAIN:-}" ]; then
    EXPECTED_CADDYFILE="http://${PREVIEW_DOMAIN} {
    reverse_proxy localhost:3000
}

http:// {
    reverse_proxy localhost:8642
}"
else
    EXPECTED_CADDYFILE="http:// {
    reverse_proxy localhost:8642
}"
fi

CURRENT_CADDYFILE=$(cat /etc/caddy/Caddyfile 2>/dev/null || echo "")
if [ "$CURRENT_CADDYFILE" != "$EXPECTED_CADDYFILE" ]; then
    echo "$EXPECTED_CADDYFILE" > /etc/caddy/Caddyfile
    /usr/local/bin/caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile 2>/dev/null || true
fi

# --- Config version sync ---
API_CONFIG_VERSION=$(echo "$RESPONSE" | jq -r '.config_version // empty' 2>/dev/null)
LOCAL_CONFIG_VERSION=$(cat /opt/hermes/.config_version 2>/dev/null || echo "0")
if [ -n "$API_CONFIG_VERSION" ] && [ "$API_CONFIG_VERSION" != "$LOCAL_CONFIG_VERSION" ]; then
    CONFIG_RESPONSE=$(curl -sf -H "Authorization: Bearer ${AGENT_TOKEN}" "${API_URL}/api/agent/config" 2>/dev/null)
    if [ $? -eq 0 ]; then
        # Update API keys in .env
        NEW_OR_KEY=$(echo "$CONFIG_RESPONSE" | jq -r '.config.openrouter_api_key // empty' 2>/dev/null)
        if [ -n "$NEW_OR_KEY" ]; then
            if grep -q '^OPENROUTER_API_KEY=' /opt/hermes/.env; then
                sed -i "s|^OPENROUTER_API_KEY=.*|OPENROUTER_API_KEY=${NEW_OR_KEY}|" /opt/hermes/.env
            else
                echo "OPENROUTER_API_KEY=${NEW_OR_KEY}" >> /opt/hermes/.env
            fi
        fi

        NEW_ANTHROPIC_KEY=$(echo "$CONFIG_RESPONSE" | jq -r '.config.anthropic_api_key // empty' 2>/dev/null)
        if [ -n "$NEW_ANTHROPIC_KEY" ]; then
            if grep -q '^ANTHROPIC_API_KEY=' /opt/hermes/.env; then
                sed -i "s|^ANTHROPIC_API_KEY=.*|ANTHROPIC_API_KEY=${NEW_ANTHROPIC_KEY}|" /opt/hermes/.env
            else
                echo "ANTHROPIC_API_KEY=${NEW_ANTHROPIC_KEY}" >> /opt/hermes/.env
            fi
        fi

        NEW_OPENAI_KEY=$(echo "$CONFIG_RESPONSE" | jq -r '.config.openai_api_key // empty' 2>/dev/null)
        if [ -n "$NEW_OPENAI_KEY" ]; then
            if grep -q '^OPENAI_API_KEY=' /opt/hermes/.env; then
                sed -i "s|^OPENAI_API_KEY=.*|OPENAI_API_KEY=${NEW_OPENAI_KEY}|" /opt/hermes/.env
            else
                echo "OPENAI_API_KEY=${NEW_OPENAI_KEY}" >> /opt/hermes/.env
            fi
        fi

        # Update model in config.yaml
        NEW_PROVIDER=$(echo "$CONFIG_RESPONSE" | jq -r '.config.provider // "openrouter"' 2>/dev/null)
        NEW_MODEL=$(echo "$CONFIG_RESPONSE" | jq -r '.config.model // empty' 2>/dev/null)
        if [ -n "$NEW_MODEL" ]; then
            cat > /opt/hermes/data/config.yaml <<CFGEOF
model:
  default: "${NEW_PROVIDER}/${NEW_MODEL}"
terminal:
  backend: docker
api_server:
  enabled: true
  host: "0.0.0.0"
  port: 8642
CFGEOF
            chown hermes:hermes /opt/hermes/data/config.yaml
        fi

        # Update SOUL.md if provided
        NEW_SOUL=$(echo "$CONFIG_RESPONSE" | jq -r '.config.soul_md // empty' 2>/dev/null)
        if [ -n "$NEW_SOUL" ]; then
            echo "$NEW_SOUL" > /opt/hermes/data/SOUL.md
            chown hermes:hermes /opt/hermes/data/SOUL.md
        fi

        # Restart Hermes service to pick up new config
        systemctl restart hermes-agent 2>/dev/null

        echo "$API_CONFIG_VERSION" > /opt/hermes/.config_version
    fi
fi

# --- Hermes version update ---
TARGET_VERSION=$(echo "$RESPONSE" | jq -r '.target_openclaw_version // empty' 2>/dev/null)

if [ -n "$TARGET_VERSION" ] && [ "$TARGET_VERSION" != "latest" ] \
   && [ "$TARGET_VERSION" != "$CURRENT_TAG" ] \
   && [ "$UPDATE_STATUS" != "pulling" ] && [ "$UPDATE_STATUS" != "updating" ]; then

    echo "pulling" > /opt/hermes/.update_status
    rm -f /opt/hermes/.update_error

    # Re-run the install script from the target version tag
    INSTALL_URL="https://raw.githubusercontent.com/NousResearch/hermes-agent/${TARGET_VERSION}/scripts/install.sh"
    if ! su - hermes -c "curl -fsSL '${INSTALL_URL}' | bash -s -- --skip-setup" 2>/tmp/hermes-update.log; then
        echo "failed" > /opt/hermes/.update_status
        echo "install failed: $(tail -1 /tmp/hermes-update.log)" > /opt/hermes/.update_error
        # No rollback needed — old binary is still in place if install fails
        exit 0
    fi

    echo "updating" > /opt/hermes/.update_status

    # Restart the service with the new version
    systemctl restart hermes-agent 2>/dev/null

    # Health check: wait up to 90 seconds (18 x 5s)
    HEALTHY=false
    for i in $(seq 1 18); do
        sleep 5
        if curl -sf http://localhost:8642/health >/dev/null 2>&1; then
            HEALTHY=true
            break
        fi
    done

    if [ "$HEALTHY" = true ]; then
        echo "completed" > /opt/hermes/.update_status
        rm -f /opt/hermes/.update_error
    else
        echo "failed" > /opt/hermes/.update_status
        echo "health check failed after update to ${TARGET_VERSION}" > /opt/hermes/.update_error
        # Restart service again in case it just needs more time
        systemctl restart hermes-agent 2>/dev/null
    fi
fi

# --- Download latest heartbeat script ---
SCRIPT_RESPONSE=$(curl -sf -H "Authorization: Bearer ${AGENT_TOKEN}" "${API_URL}/api/agent/heartbeat-script" 2>/dev/null)
if [ $? -eq 0 ] && [ -n "$SCRIPT_RESPONSE" ]; then
    CURRENT_SCRIPT=$(cat /opt/hermes/heartbeat.sh 2>/dev/null || echo "")
    if [ "$SCRIPT_RESPONSE" != "$CURRENT_SCRIPT" ]; then
        echo "$SCRIPT_RESPONSE" > /opt/hermes/heartbeat.sh
        chmod +x /opt/hermes/heartbeat.sh
    fi
fi
`
