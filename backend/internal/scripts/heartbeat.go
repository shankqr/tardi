package scripts

// HeartbeatScript is the bash script that runs on each VPS every 5 minutes
// via a systemd timer. It sends health status to the Tardi API, syncs config
// when the version changes, handles OpenClaw version updates, and guards
// and handles OpenClaw version updates.
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

# --- Migrate: add gogcli volume mount if missing ---
if ! grep -q 'data/gogcli' /opt/openclaw/docker-compose.yml 2>/dev/null; then
    mkdir -p /opt/openclaw/data/gogcli
    chown 1000:1000 /opt/openclaw/data/gogcli
    sed -i '/\.\/data\/openclaw:\/home\/node\/\.openclaw:rw/a\      - ./data/gogcli:/home/node/.config/gogcli:rw' /opt/openclaw/docker-compose.yml
    cd /opt/openclaw && docker compose up -d 2>/dev/null || true
fi

# --- Migrate: add codex volume mount if missing (persists codex login across upgrades) ---
if ! grep -q 'data/codex' /opt/openclaw/docker-compose.yml 2>/dev/null; then
    mkdir -p /opt/openclaw/data/codex
    chown 1000:1000 /opt/openclaw/data/codex
    # Copy any existing ephemeral auth out before mount hides it
    docker cp openclaw-gateway:/home/node/.codex/. /opt/openclaw/data/codex/ 2>/dev/null || true
    chown -R 1000:1000 /opt/openclaw/data/codex
    sed -i '/\.\/data\/gogcli:\/home\/node\/\.config\/gogcli:rw/a\      - ./data/codex:/home/node/.codex:rw' /opt/openclaw/docker-compose.yml
    cd /opt/openclaw && docker compose up -d 2>/dev/null || true
fi

# --- Host admin helper drift guard ---
# Installs a root-owned helper and mounts its Unix socket plus client binaries
# into OpenClaw. The helper includes a generic host.exec root bridge exposed as
# /opt/tardi/bin/sudo inside the container.
if [ ! -S /run/tardi-host-admin/admin.sock ] || [ ! -x /opt/openclaw/host-admin/bin/tardi-host-admin ] || [ ! -x /opt/openclaw/host-admin/bin/sudo ] || ! grep -q 'host.exec' /opt/openclaw/host-admin/bin/tardi-host-admin 2>/dev/null; then
    if curl -sf -H "Authorization: Bearer ${AGENT_TOKEN}" "${API_URL}/api/agent/host-admin-script" -o /opt/openclaw/install-host-admin.sh 2>/dev/null; then
        chmod +x /opt/openclaw/install-host-admin.sh
        /opt/openclaw/install-host-admin.sh >/tmp/tardi-host-admin-install.log 2>&1 || true
    fi
fi

HOST_ADMIN_COMPOSE_CHANGED=false
if ! grep -q '/run/tardi-host-admin:/run/tardi-host-admin:rw' /opt/openclaw/docker-compose.yml 2>/dev/null; then
    sed -i '/\/var\/run\/docker\.sock:\/var\/run\/docker\.sock/a\      - /run/tardi-host-admin:/run/tardi-host-admin:rw' /opt/openclaw/docker-compose.yml
    HOST_ADMIN_COMPOSE_CHANGED=true
fi
if ! grep -q '/opt/openclaw/host-admin/bin:/opt/tardi/bin:ro' /opt/openclaw/docker-compose.yml 2>/dev/null; then
    sed -i '/\/run\/tardi-host-admin:\/run\/tardi-host-admin:rw/a\      - /opt/openclaw/host-admin/bin:/opt/tardi/bin:ro' /opt/openclaw/docker-compose.yml
    HOST_ADMIN_COMPOSE_CHANGED=true
fi
if ! grep -q '/opt/openclaw/host-admin/bin/tardi-host-admin:/usr/local/bin/tardi-host-admin:ro' /opt/openclaw/docker-compose.yml 2>/dev/null; then
    sed -i '/\/opt\/openclaw\/host-admin\/bin:\/opt\/tardi\/bin:ro/a\      - /opt/openclaw/host-admin/bin/tardi-host-admin:/usr/local/bin/tardi-host-admin:ro' /opt/openclaw/docker-compose.yml
    HOST_ADMIN_COMPOSE_CHANGED=true
fi
if ! grep -q '/opt/openclaw/host-admin/bin/sudo:/usr/local/bin/sudo:ro' /opt/openclaw/docker-compose.yml 2>/dev/null; then
    sed -i '/\/opt\/openclaw\/host-admin\/bin\/tardi-host-admin:\/usr\/local\/bin\/tardi-host-admin:ro/a\      - /opt/openclaw/host-admin/bin/sudo:/usr/local/bin/sudo:ro' /opt/openclaw/docker-compose.yml
    HOST_ADMIN_COMPOSE_CHANGED=true
fi
if ! grep -q '^TARDI_HOST_ADMIN_SOCKET=' /opt/openclaw/.env 2>/dev/null; then
    echo "TARDI_HOST_ADMIN_SOCKET=/run/tardi-host-admin/admin.sock" >> /opt/openclaw/.env
    HOST_ADMIN_COMPOSE_CHANGED=true
fi
if ! grep -q '^TARDI_HOST_EXEC_TIMEOUT=' /opt/openclaw/.env 2>/dev/null; then
    echo "TARDI_HOST_EXEC_TIMEOUT=1800" >> /opt/openclaw/.env
    HOST_ADMIN_COMPOSE_CHANGED=true
fi
if ! grep -q '^PATH=.*\/opt\/tardi\/bin' /opt/openclaw/.env 2>/dev/null; then
    if grep -q '^PATH=' /opt/openclaw/.env 2>/dev/null; then
        sed -i 's|^PATH=.*|PATH=/opt/tardi/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin|' /opt/openclaw/.env
    else
        echo "PATH=/opt/tardi/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" >> /opt/openclaw/.env
    fi
    HOST_ADMIN_COMPOSE_CHANGED=true
fi
if [ "$HOST_ADMIN_COMPOSE_CHANGED" = true ]; then
    cd /opt/openclaw && docker compose up -d 2>/dev/null || true
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

# --- Sync PREVIEW_DOMAIN from heartbeat response (for existing VPSes) ---
# New VPSes get PREVIEW_DOMAIN in .env from cloud-init. Existing VPSes need
# it from the API so Caddy can route the preview domain to port 3000.
API_PREVIEW_DOMAIN=$(echo "$RESPONSE" | jq -r '.preview_domain // empty' 2>/dev/null)
if [ -n "$API_PREVIEW_DOMAIN" ] && ! grep -q "^PREVIEW_DOMAIN=" /opt/openclaw/.env 2>/dev/null; then
    echo "PREVIEW_DOMAIN=$API_PREVIEW_DOMAIN" >> /opt/openclaw/.env
    # Re-source to pick up the new value for the Caddy drift guard below
    source /opt/openclaw/.env
fi

# --- Sync CUSTOM_CADDYFILE from heartbeat response ---
# If the backend provides a custom Caddyfile, the Caddy drift guard will use it
# instead of the auto-generated one. Used for users running custom web apps.
API_CUSTOM_CADDYFILE=$(echo "$RESPONSE" | jq -r '.custom_caddyfile // empty' 2>/dev/null)
if [ -n "$API_CUSTOM_CADDYFILE" ]; then
    CUSTOM_CADDYFILE="$API_CUSTOM_CADDYFILE"
fi

# --- Model drift guard (runs every heartbeat) ---
# OpenClaw loses the model setting on container restart (Docker auto-restarts
# from crashes/OOM). Re-apply models from saved config if missing.
# Also checks that the PRIMARY model matches the DB — a failed config.patch
# RPC during sync can leave the old primary even though models are registered.
if [ "$STATUS" = "running" ]; then
    MODEL_OUT=$(docker exec openclaw-gateway openclaw models list 2>&1 || echo "")
    MODEL_LINES=$(echo "$MODEL_OUT" | wc -l | tr -d ' ')

    # Fetch expected config from API
    SAVED_CFG=$(curl -sf "${API_URL}/api/agent/config" \
        -H "Authorization: Bearer ${AGENT_TOKEN}" 2>/dev/null)
    SAVED_MODEL=$(echo "$SAVED_CFG" | jq -r '.config.model // empty' 2>/dev/null)
    SAVED_PROVIDER=$(echo "$SAVED_CFG" | jq -r '.config.provider // empty' 2>/dev/null)

    # Build the expected full model ID
    EXPECTED_PRIMARY=""
    if [ -n "$SAVED_MODEL" ]; then
        if [ "$SAVED_PROVIDER" = "openrouter" ]; then
            EXPECTED_PRIMARY="openrouter/${SAVED_MODEL}"
        else
            EXPECTED_PRIMARY="${SAVED_MODEL}"
        fi
    fi

    # Check current primary from openclaw.json
    CURRENT_PRIMARY=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null | jq -r '.agents.defaults.model.primary // empty' 2>/dev/null || true)

    # Re-register all models if none exist
    if [ "$MODEL_LINES" -le 1 ]; then
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
        # Register active model
        if [ -n "$SAVED_MODEL" ]; then
            if [ "$SAVED_PROVIDER" = "openrouter" ]; then
                docker exec openclaw-gateway openclaw models set "openrouter/${SAVED_MODEL}" 2>/dev/null
            else
                docker exec openclaw-gateway openclaw models set "${SAVED_MODEL}" 2>/dev/null
            fi
        fi
    fi

    # Fix primary model if it doesn't match the expected model from DB
    if [ -n "$EXPECTED_PRIMARY" ] && [ "$CURRENT_PRIMARY" != "$EXPECTED_PRIMARY" ]; then
        docker exec openclaw-gateway openclaw config set agents.defaults.model.primary "$EXPECTED_PRIMARY" 2>/dev/null || true
    fi
fi

# --- Caddy reverse proxy drift guard (runs every heartbeat) ---
# Caddy routes by hostname: preview domain → port 3000, everything else → 18789.
# Remove old iptables NAT rule if present (replaced by Caddy).
if iptables -t nat -C PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789 2>/dev/null; then
    iptables -t nat -D PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789
    netfilter-persistent save 2>/dev/null || true
fi

# Install Caddy binary if not present
if [ ! -x /usr/local/bin/caddy ]; then
    for i in 1 2 3; do
        curl -sL "https://caddyserver.com/api/download?os=linux&arch=amd64" -o /usr/local/bin/caddy && break
        sleep 3
    done
    chmod +x /usr/local/bin/caddy 2>/dev/null || true
fi

# Ensure Caddyfile has correct routing and Caddy is running.
# PREVIEW_DOMAIN comes from .env (new VPSes) or heartbeat API response (migration).
PREVIEW_DOMAIN_ENV=$(grep '^PREVIEW_DOMAIN=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2-)
if [ -x /usr/local/bin/caddy ]; then
    CADDY_NEEDS_UPDATE=false
    mkdir -p /etc/caddy

    # Build expected Caddyfile content.
    # If a custom Caddyfile is set in the DB, use it verbatim (for users with
    # custom web apps that need HTTPS, different ports, etc.).
    if [ -n "${CUSTOM_CADDYFILE:-}" ]; then
        EXPECTED_CADDY="$CUSTOM_CADDYFILE"
    elif [ -n "$PREVIEW_DOMAIN_ENV" ]; then
        EXPECTED_CADDY="http://${PREVIEW_DOMAIN_ENV} {
    reverse_proxy localhost:3000
}

http:// {
    reverse_proxy localhost:18789
}"
    else
        EXPECTED_CADDY="http:// {
    reverse_proxy localhost:18789
}"
    fi

    # Check if Caddyfile matches expected content
    CURRENT_CADDY=$(cat /etc/caddy/Caddyfile 2>/dev/null || echo "")
    if [ "$CURRENT_CADDY" != "$EXPECTED_CADDY" ]; then
        printf '%s\n' "$EXPECTED_CADDY" > /etc/caddy/Caddyfile
        CADDY_NEEDS_UPDATE=true
    fi

    # Ensure systemd service exists
    if [ ! -f /etc/systemd/system/caddy.service ]; then
        cat > /etc/systemd/system/caddy.service <<'CADDYSVCEOF'
[Unit]
Description=Caddy reverse proxy
After=network.target

[Service]
ExecStart=/usr/local/bin/caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
CADDYSVCEOF
        systemctl daemon-reload
        systemctl enable caddy 2>/dev/null || true
        CADDY_NEEDS_UPDATE=true
    fi

    # Start or reload Caddy
    if ! systemctl is-active --quiet caddy 2>/dev/null; then
        systemctl start caddy 2>/dev/null || true
    elif [ "$CADDY_NEEDS_UPDATE" = true ]; then
        systemctl reload caddy 2>/dev/null || systemctl restart caddy 2>/dev/null || true
    fi
fi

# --- UFW security hardening for port 18789 and SSH ---
# Port 18789 must be allowed for backend direct WebSocket RPC connections.
# We restrict it to Cloudflare IPs + backend egress CIDRs to prevent direct
# access to OpenClaw from arbitrary IPs that bypass Cloudflare proxy/WAF.
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
        # Drain Telegram long-poll before recreate: OpenClaw's telegram plugin
        # does not close its long-poll socket on SIGTERM, so Telegram's server
        # holds the slot for ~30-60s. Without this quiesce, the new container's
        # poller hits 409 Conflict for 5+ minutes and silently drops messages.
        cd /opt/openclaw && docker compose stop openclaw-gateway 2>/dev/null || true
        sleep 45
        docker compose up -d --force-recreate openclaw-gateway 2>/dev/null || true
    fi
fi

# --- Session reset drift guard (runs every heartbeat) ---
# OC defaults session.reset.mode to "daily" with atHour=4, which archives every
# chat session at 04:00 UTC and starts a fresh one on the next inbound message.
# That means users lose chat memory daily and the agent re-runs BOOTSTRAP.md,
# overwriting IDENTITY.md/USER.md as if first contact. Force "idle" mode with
# an effectively-infinite window. Must use the CLI: raw edits to session.* in
# openclaw.json get sanitized away on next container start. Schema rejects
# idleMinutes <= 0, so 100y stands in for "never". CLI write is gated on the
# container being healthy (config set fails if the gateway is down) and on
# drift actually existing (the CLI triggers an openclaw.json rewrite each call).
if [ "$STATUS" = "running" ]; then
    SESSION_RESET_MODE=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null | jq -r '.session.reset.mode // "unset"' 2>/dev/null)
    SESSION_IDLE_MIN=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null | jq -r '.session.reset.idleMinutes // 0' 2>/dev/null)
    if [ "$SESSION_RESET_MODE" != "idle" ]; then
        docker exec openclaw-gateway openclaw config set session.reset.mode idle >/dev/null 2>&1
    fi
    if [ "$SESSION_IDLE_MIN" -lt 525600 ] 2>/dev/null; then
        docker exec openclaw-gateway openclaw config set session.reset.idleMinutes 52560000 >/dev/null 2>&1
    fi
fi

# --- Device pairing drift guard (runs every heartbeat) ---
# OC's localhost CLI (the agent's tool runtime) needs operator.admin / .write
# / .approvals / .pairing / .talk.secrets scopes to call most tools. On a
# fresh VPS the device is paired with operator.read only, so the first scope
# upgrade attempt fails with code=1008 ("pairing required"), the WebSocket
# handshake closes, the agent's tool call hangs, the typing indicator hits
# its 2m TTL, and the user sees no reply. OC has no built-in auto-approve
# for the in-container CLI, so we do it here: list pending requests and
# approve each. Idempotent — no pending → no-op.
if [ "$STATUS" = "running" ]; then
    PENDING_REQS=$(docker exec openclaw-gateway openclaw devices list 2>/dev/null \
        | awk '/^Pending/,/^Paired/' \
        | grep -oE '\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b' \
        | sort -u)
    for req in $PENDING_REQS; do
        docker exec openclaw-gateway openclaw devices approve "$req" >/dev/null 2>&1 || true
    done
fi

# --- Telegram channel drift guard (runs every heartbeat) ---
# OC auto-detects Telegram accounts, but defaults new account entries to
# pairing/allowlist and streaming replies. Normalize both top-level Telegram
# config and per-account overrides. OC 2026.4.26 uses streaming.mode instead
# of the old scalar streaming value.
if [ "$STATUS" = "running" ]; then
    python3 - <<'PY' >/tmp/openclaw-channel-drift.log 2>&1 || true
import datetime
import json

path = "/opt/openclaw/data/openclaw/openclaw.json"
try:
    with open(path) as f:
        cfg = json.load(f)
except Exception:
    raise SystemExit(0)

changed = False

def put(obj, key, value):
    global changed
    if obj.get(key) != value:
        obj[key] = value
        changed = True

channels = cfg.get("channels")
tg = channels.get("telegram") if isinstance(channels, dict) else None
if isinstance(tg, dict) and tg.get("enabled") is not False:
    put(tg, "enabled", True)
    put(tg, "allowFrom", ["*"])
    put(tg, "dmPolicy", "open")
    put(tg, "groupPolicy", "disabled")
    put(tg, "streaming", {"mode": "off"})

    accounts = tg.get("accounts")
    if isinstance(accounts, dict):
        for account in accounts.values():
            if isinstance(account, dict) and account.get("enabled") is not False:
                put(account, "enabled", True)
                put(account, "allowFrom", ["*"])
                put(account, "dmPolicy", "open")
                put(account, "groupPolicy", "disabled")
                put(account, "streaming", {"mode": "off"})

if changed:
    cfg.setdefault("meta", {})["lastTouchedAt"] = datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%S.000Z")
    with open(path, "w") as f:
        json.dump(cfg, f, indent=2)
PY
fi

# --- Codex model provider drift guard (runs every heartbeat) ---
# Tardi's default model is codex/gpt-5.5. In OC 2026.4.26 that model resolves
# through the OpenAI provider using the Codex app-server token marker. If the
# provider entry is missing, channels connect but agent turns never produce a
# reply because the resolved openai/gpt-5.5 provider is unauthenticated.
if [ "$STATUS" = "running" ] && [ -s /opt/openclaw/data/codex/auth.json ] && ! grep -q '^OPENAI_API_KEY=.\+' /opt/openclaw/.env 2>/dev/null; then
    python3 - <<'PY' >/tmp/openclaw-codex-model-drift.log 2>&1 || true
import datetime
import json

path = "/opt/openclaw/data/openclaw/openclaw.json"
try:
    with open(path) as f:
        cfg = json.load(f)
except Exception:
    raise SystemExit(0)

codex_provider = {
    "baseUrl": "https://chatgpt.com/backend-api/v1",
    "apiKey": "codex-app-server",
    "auth": "token",
    "api": "openai-codex-responses",
    "models": [
        {
            "id": "gpt-5.5",
            "name": "GPT-5.5",
            "api": "openai-codex-responses",
            "reasoning": True,
            "input": ["text", "image"],
            "cost": {"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
            "contextWindow": 272000,
            "maxTokens": 128000,
            "compat": {
                "supportsReasoningEffort": True,
                "supportsUsageInStreaming": True,
            },
        }
    ],
}

changed = False
providers = cfg.setdefault("models", {}).setdefault("providers", {})
if providers.get("openai") != codex_provider:
    providers["openai"] = codex_provider
    changed = True

model_cfg = cfg.setdefault("agents", {}).setdefault("defaults", {}).setdefault("model", {})
if model_cfg.get("primary") in ("", None, "openai/gpt-5.5", "codex/gpt-5.5"):
    if model_cfg.get("primary") != "codex/gpt-5.5":
        model_cfg["primary"] = "codex/gpt-5.5"
        changed = True

models = cfg.setdefault("agents", {}).setdefault("defaults", {}).setdefault("models", {})
if models.get("codex/gpt-5.5") != {}:
    models["codex/gpt-5.5"] = {}
    changed = True

if changed:
    cfg.setdefault("meta", {})["lastTouchedAt"] = datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%S.000Z")
    with open(path, "w") as f:
        json.dump(cfg, f, indent=2)
PY
fi

# --- BOOTSTRAP.md cleanup (runs every heartbeat) ---
# OC's bundled BOOTSTRAP.md tells the agent to delete itself once identity is
# established, but agents rarely follow through. Left in place, any session
# reset (manual /new, /reset, etc.) re-runs the bootstrap flow which overwrites
# IDENTITY.md and USER.md as if the user is a stranger. Workspace is on a host
# volume, so removing the file from the host is sufficient.
if [ -f /opt/openclaw/data/openclaw/workspace/BOOTSTRAP.md ]; then
    rm -f /opt/openclaw/data/openclaw/workspace/BOOTSTRAP.md
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
        NEW_PROVIDER=$(echo "$CONFIG" | jq -r '.config.provider // empty')
        NEW_MODEL=$(echo "$CONFIG" | jq -r '.config.model // empty')
        NEW_GOOGLE_CLIENT=$(echo "$CONFIG" | jq -r '.config.google_client_b64 // empty')
        NEW_GOOGLE_TOKEN=$(echo "$CONFIG" | jq -r '.config.google_token_b64 // empty')
        NEW_GOOGLE_EMAIL=$(echo "$CONFIG" | jq -r '.config.google_email // empty')
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
            # Drain Telegram long-poll first: see auth-drift guard above for why.
            cd /opt/openclaw && docker compose stop openclaw-gateway 2>/dev/null || true
            sleep 45
            docker compose up -d --force-recreate openclaw-gateway

            # Wait for healthy, then apply post-startup config.
            # OpenClaw takes ~70s to start; wait up to 120s (24 x 5s).
            HEALTHY=false
            for i in $(seq 1 24); do
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
            GOG_DIR="/opt/openclaw/data/gogcli"
            if [ -n "$NEW_GOOGLE_TOKEN" ] && [ -n "$NEW_GOOGLE_EMAIL" ]; then
                mkdir -p "$GOG_DIR/tokens"
                [ -n "$NEW_GOOGLE_CLIENT" ] && printf '%s' "$NEW_GOOGLE_CLIENT" | base64 -d > "$GOG_DIR/credentials.json"
                printf '%s' "$NEW_GOOGLE_TOKEN" | base64 -d > "$GOG_DIR/tokens/${NEW_GOOGLE_EMAIL}.json"
                chown -R 1000:1000 "$GOG_DIR"
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

    # Drain Telegram long-poll before recreate: see auth-drift guard above for why.
    docker compose -f /opt/openclaw/docker-compose.yml stop openclaw-gateway 2>/dev/null || true
    sleep 45

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

        # Reinstall Codex CLI (lost on container recreate). Best-effort;
        # failure does not fail the upgrade.
        docker exec -u 0 openclaw-gateway npm install -g @openai/codex >/dev/null 2>&1 || true

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
