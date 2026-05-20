package scripts

// HermesHeartbeatScript is the bash script that runs on each Hermes VPS every
// 5 minutes via a systemd timer. Hermes is managed as a Docker Compose stack,
// matching the OpenClaw VPS architecture.
const HermesHeartbeatScript = `#!/bin/bash
set -u
source /opt/hermes/.env 2>/dev/null || true
HERMES_DEFAULT_PROVIDER="openrouter"
HERMES_DEFAULT_MODEL="anthropic/claude-sonnet-4.6"

# --- Download latest heartbeat script ---
# Keep this at the top so old VPSes migrate themselves before doing any other work.
if [ -n "${AGENT_TOKEN:-}" ] && [ -n "${API_URL:-}" ]; then
    SCRIPT_RESPONSE=$(curl -sf -H "Authorization: Bearer ${AGENT_TOKEN}" "${API_URL}/api/agent/heartbeat-script" 2>/dev/null)
    if [ $? -eq 0 ] && [ -n "$SCRIPT_RESPONSE" ]; then
        CURRENT_SCRIPT=$(cat /opt/hermes/heartbeat.sh 2>/dev/null || echo "")
        if [ "$SCRIPT_RESPONSE" != "$CURRENT_SCRIPT" ]; then
            echo "$SCRIPT_RESPONSE" > /opt/hermes/heartbeat.sh
            chmod +x /opt/hermes/heartbeat.sh
            exec /bin/bash /opt/hermes/heartbeat.sh
        fi
    fi
fi

sync_data_env() {
    mkdir -p /opt/hermes/data
    if [ -f /opt/hermes/.env ]; then
        cp /opt/hermes/.env /opt/hermes/data/.env
        chown 1000:1000 /opt/hermes/data/.env 2>/dev/null || true
        chmod 600 /opt/hermes/data/.env 2>/dev/null || true
    fi
}

write_hermes_compose() {
    cat > /opt/hermes/docker-compose.yml <<'COMPOSEEOF'
services:
  hermes-agent:
    image: nousresearch/hermes-agent:latest
    container_name: hermes-agent
    restart: unless-stopped
    network_mode: host
    shm_size: "1g"
    command: gateway run
    group_add:
      - "${DOCKER_GID}"
      - "${HERMES_GID}"
    volumes:
      - ./data:/opt/data:rw
      - /var/run/docker.sock:/var/run/docker.sock
    env_file:
      - .env
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:8642/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 60s

COMPOSEEOF
}

ensure_hermes_stack() {
    mkdir -p /opt/hermes/data

    # Native Hermes installs stored state under /home/hermes/.hermes. Copy any
    # missing files into the Docker data volume before stopping native services.
    if [ -d /home/hermes/.hermes ]; then
        cp -an /home/hermes/.hermes/. /opt/hermes/data/ 2>/dev/null || true
    fi

    useradd -r -m -u 1000 -s /usr/sbin/nologin hermes 2>/dev/null || true
    usermod -aG docker hermes 2>/dev/null || true

    DOCKER_GID=$(getent group docker | cut -d: -f3 || true)
    HERMES_GID=$(getent group hermes | cut -d: -f3 || true)
    [ -n "$DOCKER_GID" ] || DOCKER_GID=999
    [ -n "$HERMES_GID" ] || HERMES_GID=1000

    touch /opt/hermes/.env
    for kv in \
        "DOCKER_GID=${DOCKER_GID}" \
        "HERMES_UID=1000" \
        "HERMES_GID=${HERMES_GID}" \
        "API_SERVER_ENABLED=true" \
        "API_SERVER_HOST=0.0.0.0" \
        "API_SERVER_PORT=8642" \
        "API_SERVER_CORS_ORIGINS=*" \
        "HERMES_DASHBOARD=1" \
        "HERMES_DASHBOARD_HOST=127.0.0.1" \
        "HERMES_DASHBOARD_PORT=9119" \
        "HERMES_DASHBOARD_TUI=1" \
        "TERMINAL_ENV=docker" \
        "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt" \
        "SSL_CERT_DIR=/etc/ssl/certs"; do
        key=${kv%%=*}
        if grep -q "^${key}=" /opt/hermes/.env 2>/dev/null; then
            sed -i "s|^${key}=.*|${kv}|" /opt/hermes/.env
        else
            echo "$kv" >> /opt/hermes/.env
        fi
    done
    chmod 600 /opt/hermes/.env
    source /opt/hermes/.env 2>/dev/null || true
    sync_data_env

    if [ ! -f /opt/hermes/data/config.yaml ]; then
        cat > /opt/hermes/data/config.yaml <<'CFGEOF'
terminal:
  backend: docker
CFGEOF
    fi
    if [ ! -f /opt/hermes/data/SOUL.md ]; then
        cat > /opt/hermes/data/SOUL.md <<'SOULEOF'
You are a helpful AI assistant running on Tardi.
You can execute code, browse the web, manage files, and help with various tasks.
Be concise and helpful in your responses.
SOULEOF
    fi
    chown -R 1000:1000 /opt/hermes/data 2>/dev/null || true

    STACK_CHANGED=false
    if [ ! -f /opt/hermes/docker-compose.yml ] || ! grep -q 'nousresearch/hermes-agent' /opt/hermes/docker-compose.yml 2>/dev/null; then
        write_hermes_compose
        STACK_CHANGED=true
    fi
    if ! grep -q 'network_mode: host' /opt/hermes/docker-compose.yml 2>/dev/null; then
        write_hermes_compose
        STACK_CHANGED=true
    fi

    if [ ! -f /etc/systemd/system/hermes-stack.service ]; then
        cat > /etc/systemd/system/hermes-stack.service <<'SVCEOF'
[Unit]
Description=Hermes Agent Stack
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/hermes
ExecStart=/usr/bin/docker compose up -d --remove-orphans
ExecStop=/usr/bin/docker compose down
Restart=on-failure
RestartSec=30

[Install]
WantedBy=multi-user.target
SVCEOF
        STACK_CHANGED=true
    fi

    # Disable older native-process units if this VPS was provisioned before the
    # Docker architecture. The dashboard shim stays host-side in front of :9119.
    systemctl stop hermes-agent hermes-dashboard 2>/dev/null || true
    systemctl disable hermes-agent hermes-dashboard 2>/dev/null || true

    systemctl daemon-reload 2>/dev/null || true
    systemctl enable hermes-stack 2>/dev/null || true
    if ! systemctl is-active --quiet hermes-stack 2>/dev/null; then
        systemctl start hermes-stack 2>/dev/null || true
    elif [ "$STACK_CHANGED" = true ]; then
        docker compose -f /opt/hermes/docker-compose.yml up -d --remove-orphans 2>/dev/null || true
    fi
}

ensure_hermes_stack
source /opt/hermes/.env 2>/dev/null || true

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
    CONTAINER_STATE=$(docker inspect -f '{{.State.Status}}' hermes-agent 2>/dev/null || true)
    if [ "$CONTAINER_STATE" = "running" ]; then
        STATUS="unhealthy"
    elif [ -n "$CONTAINER_STATE" ]; then
        STATUS="stopped"
    else
        STATUS="not_found"
    fi
fi

# Detect current running Hermes version. When tracking :latest, report the
# runtime version when available instead of the moving Docker tag.
CURRENT_IMAGE=$(docker inspect --format='{{.Config.Image}}' hermes-agent 2>/dev/null || true)
CURRENT_IMAGE_ID=$(docker inspect --format='{{.Image}}' hermes-agent 2>/dev/null || true)
CURRENT_TAG=$(echo "$CURRENT_IMAGE" | sed 's/.*://' | tr -d '[:space:]')
RUNTIME_VERSION=""
if [ "$STATUS" = "running" ] || [ "$STATUS" = "unhealthy" ]; then
    RUNTIME_VERSION=$(timeout 10s docker exec hermes-agent hermes --version 2>/dev/null | grep -Eo 'v?[0-9]{4}\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?|[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?' | head -1 || true)
fi
[ -n "$RUNTIME_VERSION" ] && CURRENT_TAG="$RUNTIME_VERSION"
[ -z "$CURRENT_TAG" ] && CURRENT_TAG="unknown"

# Read update status if mid-update
UPDATE_STATUS=$(cat /opt/hermes/.update_status 2>/dev/null || echo "")
UPDATE_ERROR=$(cat /opt/hermes/.update_error 2>/dev/null || echo "")

CODEX_AUTH_PRESENT=false
if jq -e '((.credential_pool["openai-codex"] // []) | map(select(((.access_token // .runtime_api_key // "") | length) > 0)) | length > 0) or (((.providers["openai-codex"].tokens.access_token // "") | length) > 0)' /opt/hermes/data/auth.json >/dev/null 2>&1; then
    CODEX_AUTH_PRESENT=true
fi
CODEX_CONFIG_ACTIVE=false
if grep -Eq 'provider:[[:space:]]*"?openai-codex"?|model:[[:space:]]*"?openai-codex/' /opt/hermes/data/config.yaml 2>/dev/null; then
    CODEX_CONFIG_ACTIVE=true
fi

# Check for errors in recent docker logs
AGENT_ERROR=""
if [ "$STATUS" = "running" ] || [ "$STATUS" = "unhealthy" ]; then
    RECENT_LOGS=$(docker logs hermes-agent --tail 100 --since 10m 2>&1)
    if [ "$CODEX_CONFIG_ACTIVE" = true ] && [ "$CODEX_AUTH_PRESENT" != true ]; then
        AGENT_ERROR="codex_reauth_required"
    elif echo "$RECENT_LOGS" | grep -qi "you've hit your usage limit\|codex.*usage limit"; then
        AGENT_ERROR="codex_usage_limit_exceeded"
    elif echo "$RECENT_LOGS" | grep -qi "missing bearer or basic authentication\|unexpected status 401 unauthorized.*codex\|codex.*401\|openai-codex.*unauthorized"; then
        AGENT_ERROR="codex_reauth_required"
    elif echo "$RECENT_LOGS" | grep -qi "key limit exceeded"; then
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

SHIM_ENV_CHANGED=false
API_DASHBOARD_TOKEN=$(echo "$RESPONSE" | jq -r '.dashboard_token // empty' 2>/dev/null)
CURRENT_API_SERVER_KEY=$(grep '^API_SERVER_KEY=' /opt/hermes/.env 2>/dev/null | cut -d= -f2-)
if [ -n "$API_DASHBOARD_TOKEN" ] && [ "$API_DASHBOARD_TOKEN" != "$CURRENT_API_SERVER_KEY" ]; then
    if grep -q '^API_SERVER_KEY=' /opt/hermes/.env 2>/dev/null; then
        sed -i "s|^API_SERVER_KEY=.*|API_SERVER_KEY=${API_DASHBOARD_TOKEN}|" /opt/hermes/.env
    else
        echo "API_SERVER_KEY=${API_DASHBOARD_TOKEN}" >> /opt/hermes/.env
    fi
    chmod 600 /opt/hermes/.env
    sync_data_env
    SHIM_ENV_CHANGED=true
fi
source /opt/hermes/.env 2>/dev/null || true

# --- Sync PREVIEW_DOMAIN from heartbeat response ---
API_PREVIEW_DOMAIN=$(echo "$RESPONSE" | jq -r '.preview_domain // empty' 2>/dev/null)
if [ -n "$API_PREVIEW_DOMAIN" ]; then
    CURRENT_PREVIEW=$(grep '^PREVIEW_DOMAIN=' /opt/hermes/.env 2>/dev/null | cut -d= -f2-)
    if [ "$API_PREVIEW_DOMAIN" != "$CURRENT_PREVIEW" ]; then
        if grep -q '^PREVIEW_DOMAIN=' /opt/hermes/.env 2>/dev/null; then
            sed -i "s|^PREVIEW_DOMAIN=.*|PREVIEW_DOMAIN=${API_PREVIEW_DOMAIN}|" /opt/hermes/.env
        else
            echo "PREVIEW_DOMAIN=${API_PREVIEW_DOMAIN}" >> /opt/hermes/.env
        fi
        sync_data_env
    fi
fi
source /opt/hermes/.env 2>/dev/null || true

# --- Caddy drift guard ---
CUSTOM_CADDYFILE=$(echo "$RESPONSE" | jq -r '.custom_caddyfile // empty' 2>/dev/null)
ROOT_BLOCK="http:// {
    handle /v1/* {
        reverse_proxy localhost:8642
    }
    handle /health {
        reverse_proxy localhost:8642
    }
    handle {
        reverse_proxy localhost:9118
    }
}"

if [ -n "$CUSTOM_CADDYFILE" ]; then
    EXPECTED_CADDYFILE="$CUSTOM_CADDYFILE"
elif [ -n "${PREVIEW_DOMAIN:-}" ]; then
    EXPECTED_CADDYFILE="http://${PREVIEW_DOMAIN} {
    reverse_proxy localhost:3000
}

${ROOT_BLOCK}"
else
    EXPECTED_CADDYFILE="${ROOT_BLOCK}"
fi

CURRENT_CADDYFILE=$(cat /etc/caddy/Caddyfile 2>/dev/null || echo "")
if [ "$CURRENT_CADDYFILE" != "$EXPECTED_CADDYFILE" ]; then
    printf '%s\n' "$EXPECTED_CADDYFILE" > /etc/caddy/Caddyfile
    /usr/local/bin/caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile 2>/dev/null || systemctl restart caddy 2>/dev/null || true
fi

# --- UFW security hardening for port 8642 and SSH ---
BACKEND_CIDRS=$(grep '^BACKEND_EGRESS_CIDRS=' /opt/hermes/.env 2>/dev/null | cut -d= -f2-)
if ufw status | grep "8642/tcp" | grep -q "Anywhere" 2>/dev/null; then
    UFW_NEEDS_HARDENING=true
elif ! ufw status | grep -q "8642" 2>/dev/null; then
    UFW_NEEDS_HARDENING=true
else
    UFW_NEEDS_HARDENING=false
fi

CF_MARKER="/opt/hermes/.cf_ufw_updated"
CF_STALE=false
if [ ! -f "$CF_MARKER" ]; then
    CF_STALE=true
elif [ -n "$(find "$CF_MARKER" -mmin +1440 2>/dev/null)" ]; then
    CF_STALE=true
fi

if [ "$UFW_NEEDS_HARDENING" = true ] || [ "$CF_STALE" = true ]; then
    ufw delete allow 8642/tcp 2>/dev/null || true
    CF_IPS=$(curl -sf https://www.cloudflare.com/ips-v4 2>/dev/null || echo "")
    if [ -n "$CF_IPS" ]; then
        for cidr in $CF_IPS; do
            ufw allow from $cidr to any port 8642 2>/dev/null || true
        done
        date > "$CF_MARKER"
    fi
    if [ -n "$BACKEND_CIDRS" ]; then
        ufw delete allow 22/tcp 2>/dev/null || true
        for cidr in $(echo "$BACKEND_CIDRS" | tr ',' ' '); do
            ufw allow from $cidr to any port 8642 2>/dev/null || true
            ufw allow from $cidr to any port 22 2>/dev/null || true
        done
    fi
fi

# --- Dashboard shim binary self-update ---
EXPECTED_SHIM_SHA=$(curl -sf -H "Authorization: Bearer ${AGENT_TOKEN}" "${API_URL}/api/agent/dashboard-shim-sha" 2>/dev/null)
if [ -n "$EXPECTED_SHIM_SHA" ]; then
    CURRENT_SHIM_SHA=$(sha256sum /usr/local/bin/tardi-dashboard-shim 2>/dev/null | awk '{print $1}')
    if [ "$EXPECTED_SHIM_SHA" != "$CURRENT_SHIM_SHA" ]; then
        if curl -fsSL -H "Authorization: Bearer ${AGENT_TOKEN}" \
            "${API_URL}/api/agent/dashboard-shim" \
            -o /usr/local/bin/tardi-dashboard-shim.new; then
            chmod +x /usr/local/bin/tardi-dashboard-shim.new
            mv /usr/local/bin/tardi-dashboard-shim.new /usr/local/bin/tardi-dashboard-shim
            systemctl restart tardi-dashboard-shim 2>/dev/null || true
        fi
    fi
fi

# --- Dashboard shim service ---
if [ ! -f /etc/systemd/system/tardi-dashboard-shim.service ]; then
    cat > /etc/systemd/system/tardi-dashboard-shim.service <<'SHIMSVCEOF'
[Unit]
Description=Tardi Dashboard Auth Shim
After=network.target hermes-stack.service
Wants=hermes-stack.service

[Service]
Type=simple
User=hermes
Group=hermes
EnvironmentFile=/opt/hermes/.env
Environment=LISTEN=127.0.0.1:9118
Environment=DASHBOARD_BACKEND=http://127.0.0.1:9119
ExecStart=/usr/local/bin/tardi-dashboard-shim
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
SHIMSVCEOF
    systemctl daemon-reload 2>/dev/null || true
    systemctl enable tardi-dashboard-shim 2>/dev/null || true
fi

if [ -f /etc/systemd/system/tardi-dashboard-shim.service ]; then
    SHIM_STATE=$(systemctl is-active tardi-dashboard-shim 2>/dev/null || echo "unknown")
    if [ "$SHIM_ENV_CHANGED" = true ] || [ "$SHIM_STATE" = "inactive" ] || [ "$SHIM_STATE" = "failed" ]; then
        systemctl restart tardi-dashboard-shim 2>/dev/null || true
    fi
fi

# The dashboard is a side-process inside the Hermes container. Restart the
# container if the gateway is healthy but the dashboard side-process has exited.
if [ "$STATUS" = "running" ] && ! curl -sf http://localhost:9119 >/dev/null 2>&1; then
    docker restart hermes-agent >/dev/null 2>&1 || true
fi

# --- Config version sync ---
API_CONFIG_VERSION=$(echo "$RESPONSE" | jq -r '.config_version // empty' 2>/dev/null)
LOCAL_CONFIG_VERSION=$(cat /opt/hermes/.config_version 2>/dev/null || echo "0")
if [ -n "$API_CONFIG_VERSION" ] && [ "$API_CONFIG_VERSION" != "$LOCAL_CONFIG_VERSION" ]; then
    CONFIG_RESPONSE=$(curl -sf -H "Authorization: Bearer ${AGENT_TOKEN}" "${API_URL}/api/agent/config" 2>/dev/null)
    if [ $? -eq 0 ]; then
        cp /opt/hermes/.env /opt/hermes/.env.bak
        grep -v -E '_API_KEY=' /opt/hermes/.env > /opt/hermes/.env.tmp

        NEW_OR_KEY=$(echo "$CONFIG_RESPONSE" | jq -r '.config.openrouter_api_key // empty' 2>/dev/null)
        NEW_ANTHROPIC_KEY=$(echo "$CONFIG_RESPONSE" | jq -r '.config.anthropic_api_key // empty' 2>/dev/null)
        NEW_OPENAI_KEY=$(echo "$CONFIG_RESPONSE" | jq -r '.config.openai_api_key // empty' 2>/dev/null)
        [ -n "$NEW_OR_KEY" ] && echo "OPENROUTER_API_KEY=${NEW_OR_KEY}" >> /opt/hermes/.env.tmp
        [ -n "$NEW_ANTHROPIC_KEY" ] && echo "ANTHROPIC_API_KEY=${NEW_ANTHROPIC_KEY}" >> /opt/hermes/.env.tmp
        [ -n "$NEW_OPENAI_KEY" ] && echo "OPENAI_API_KEY=${NEW_OPENAI_KEY}" >> /opt/hermes/.env.tmp

        mv /opt/hermes/.env.tmp /opt/hermes/.env
        chmod 600 /opt/hermes/.env
        sync_data_env

        NEW_PROVIDER=$(echo "$CONFIG_RESPONSE" | jq -r '.config.provider // "openrouter"' 2>/dev/null)
        NEW_MODEL=$(echo "$CONFIG_RESPONSE" | jq -r '.config.model // empty' 2>/dev/null)
        if [ "$NEW_PROVIDER" = "codex" ]; then
            NEW_PROVIDER="openai-codex"
        fi
        if [ -z "$NEW_PROVIDER" ]; then
            NEW_PROVIDER="$HERMES_DEFAULT_PROVIDER"
        fi
        if [[ "$NEW_MODEL" == codex/* ]]; then
            NEW_MODEL="openai-codex/${NEW_MODEL#codex/}"
        fi
        if [ -z "$NEW_MODEL" ]; then
            NEW_MODEL="$HERMES_DEFAULT_MODEL"
        fi
        if [ -n "$NEW_MODEL" ]; then
            cat > /opt/hermes/data/config.yaml <<CFGEOF
model:
  provider: "${NEW_PROVIDER}"
  model: "${NEW_MODEL}"
terminal:
  backend: docker
CFGEOF
            chown 1000:1000 /opt/hermes/data/config.yaml 2>/dev/null || true
            chmod 640 /opt/hermes/data/config.yaml 2>/dev/null || true
        fi

        NEW_SOUL=$(echo "$CONFIG_RESPONSE" | jq -r '.config.soul_md // empty' 2>/dev/null)
        if [ -n "$NEW_SOUL" ]; then
            printf '%s\n' "$NEW_SOUL" > /opt/hermes/data/SOUL.md
            chown 1000:1000 /opt/hermes/data/SOUL.md 2>/dev/null || true
        fi

        rm -f /opt/hermes/.env.bak
        docker compose -f /opt/hermes/docker-compose.yml up -d --force-recreate hermes-agent 2>/dev/null || true

        for i in $(seq 1 24); do
            sleep 5
            if curl -sf http://localhost:8642/health >/dev/null 2>&1; then
                echo "$API_CONFIG_VERSION" > /opt/hermes/.config_version
                break
            fi
        done
    fi
fi

# --- Hermes version update ---
TARGET_VERSION=$(echo "$RESPONSE" | jq -r '.target_openclaw_version // empty' 2>/dev/null)
TARGET_IMAGE_REF=""
UPDATE_NEEDED=false

if [ -n "$TARGET_VERSION" ] && [ "$UPDATE_STATUS" != "pulling" ] && [ "$UPDATE_STATUS" != "updating" ]; then
    if [ "$TARGET_VERSION" = "latest" ]; then
        TARGET_IMAGE_REF="nousresearch/hermes-agent:latest"
        PREVIOUS_IMAGE_REF=$(grep -E '^[[:space:]]*image:[[:space:]]*nousresearch/hermes-agent:' /opt/hermes/docker-compose.yml 2>/dev/null | head -1 | awk '{print $2}')
        [ -n "$PREVIOUS_IMAGE_REF" ] || PREVIOUS_IMAGE_REF="$CURRENT_IMAGE"

        sed -i "s|image: nousresearch/hermes-agent:.*|image: ${TARGET_IMAGE_REF}|" \
            /opt/hermes/docker-compose.yml

        echo "checking" > /opt/hermes/.update_status
        rm -f /opt/hermes/.update_error

        if ! docker compose -f /opt/hermes/docker-compose.yml pull hermes-agent 2>/tmp/hermes-pull.log; then
            echo "failed" > /opt/hermes/.update_status
            echo "pull failed: $(tail -1 /tmp/hermes-pull.log)" > /opt/hermes/.update_error
            if [ "$PREVIOUS_IMAGE_REF" != "$TARGET_IMAGE_REF" ]; then
                sed -i "s|image: nousresearch/hermes-agent:.*|image: ${PREVIOUS_IMAGE_REF}|" \
                    /opt/hermes/docker-compose.yml
            fi
            exit 0
        fi

        TARGET_IMAGE_ID=$(docker image inspect "$TARGET_IMAGE_REF" --format '{{.Id}}' 2>/dev/null || true)
        if [ -n "$TARGET_IMAGE_ID" ] && [ "$TARGET_IMAGE_ID" != "$CURRENT_IMAGE_ID" ]; then
            UPDATE_NEEDED=true
        else
            echo "completed" > /opt/hermes/.update_status
            rm -f /opt/hermes/.update_error
        fi
    elif [ "$TARGET_VERSION" != "$CURRENT_TAG" ]; then
        TARGET_IMAGE_REF="nousresearch/hermes-agent:${TARGET_VERSION}"
        PREVIOUS_IMAGE_REF=$(grep -E '^[[:space:]]*image:[[:space:]]*nousresearch/hermes-agent:' /opt/hermes/docker-compose.yml 2>/dev/null | head -1 | awk '{print $2}')
        [ -n "$PREVIOUS_IMAGE_REF" ] || PREVIOUS_IMAGE_REF="$CURRENT_IMAGE"
        UPDATE_NEEDED=true
    fi
fi

if [ "$UPDATE_NEEDED" = true ]; then
    echo "pulling" > /opt/hermes/.update_status
    rm -f /opt/hermes/.update_error

    sed -i "s|image: nousresearch/hermes-agent:.*|image: ${TARGET_IMAGE_REF}|" \
        /opt/hermes/docker-compose.yml

    if ! docker compose -f /opt/hermes/docker-compose.yml pull hermes-agent 2>/tmp/hermes-pull.log; then
        echo "failed" > /opt/hermes/.update_status
        echo "pull failed: $(tail -1 /tmp/hermes-pull.log)" > /opt/hermes/.update_error
        sed -i "s|image: nousresearch/hermes-agent:.*|image: ${PREVIOUS_IMAGE_REF}|" \
            /opt/hermes/docker-compose.yml
        exit 0
    fi

    echo "updating" > /opt/hermes/.update_status

    if ! docker compose -f /opt/hermes/docker-compose.yml up -d hermes-agent 2>/tmp/hermes-update.log; then
        echo "failed" > /opt/hermes/.update_status
        echo "up failed: $(tail -1 /tmp/hermes-update.log)" > /opt/hermes/.update_error
        sed -i "s|image: nousresearch/hermes-agent:.*|image: ${PREVIOUS_IMAGE_REF}|" \
            /opt/hermes/docker-compose.yml
        docker compose -f /opt/hermes/docker-compose.yml up -d hermes-agent 2>/dev/null
        exit 0
    fi

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
        docker image prune -f >/dev/null 2>&1
    else
        echo "failed" > /opt/hermes/.update_status
        echo "health check failed after update" > /opt/hermes/.update_error
        sed -i "s|image: nousresearch/hermes-agent:.*|image: ${PREVIOUS_IMAGE_REF}|" \
            /opt/hermes/docker-compose.yml
        docker compose -f /opt/hermes/docker-compose.yml up -d hermes-agent 2>/dev/null
    fi
fi
`
