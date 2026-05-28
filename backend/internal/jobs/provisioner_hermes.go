package jobs

import (
	"bytes"
	"text/template"
)

// HermesCloudInitData holds all template variables for Hermes cloud-init rendering.
type HermesCloudInitData struct {
	AgentToken           string // Tardi API auth token
	APIURL               string // Tardi backend URL
	InstanceID           string // VPS instance UUID
	OpenRouterAPIKey     string // Default LLM provider
	AnthropicAPIKey      string // Optional direct Anthropic access
	OpenAIAPIKey         string // Optional direct OpenAI access
	TelegramBotToken     string // Optional Telegram bot token
	TelegramAllowedUsers string // Optional comma-separated Telegram user allowlist
	APIServerKey         string // Hermes HTTP API auth token (stored in openclaw_auth_token column)
	HermesImageTag       string // Docker image tag (e.g. "latest" or "v2026.4.13")
	Provider             string // AI provider: openrouter, anthropic, openai
	Model                string // Model ID for the provider
	ConfigVersion        int    // Initial config version to prevent redundant first sync
	RootPassword         string // Root password for VPS
	SSHPublicKey         string // Ed25519 public key for SSH
	Domain               string // Cloudflare domain (e.g. "abc12345.tardi.ai")
	PreviewDomain        string // Preview domain for port 3000 apps
	BackendEgressCIDRs   string // Comma-separated CIDRs for backend egress IPs
}

// hermesCloudInitTemplate bootstraps Hermes in the same VPS architecture as
// OpenClaw: Docker Compose, host networking, a persistent data volume, Caddy on
// port 80, and a heartbeat timer that owns config drift and image updates.
var hermesCloudInitTemplate = template.Must(template.New("hermes-cloudinit").Parse(`#!/bin/bash
set -euo pipefail
exec > >(tee -a /var/log/hermes-init.log) 2>&1

STATUS_FILE=/opt/hermes/.init-status
mkdir -p /opt/hermes
log_status() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $1" | tee -a "$STATUS_FILE"; }

log_status "STARTED"

# --- Swap (prevents OOM on small instances during image pull) ---
if [ ! -f /swapfile ]; then
    fallocate -l 2G /swapfile
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile
    echo '/swapfile none swap sw 0 0' >> /etc/fstab
    log_status "SWAP_CREATED"
fi

# --- System setup ---
export DEBIAN_FRONTEND=noninteractive
for i in 1 2 3; do
    apt-get update -qq && break
    log_status "APT_UPDATE_RETRY_$i"
    sleep 5
done
apt-get install -y -qq ca-certificates curl jq ufw

# --- Firewall ---
ufw default deny incoming
ufw default allow outgoing
ufw allow 80/tcp
# Port 8642: Hermes API server. Allow Cloudflare and backend egress only.
CF_IPS=$(curl -sf https://www.cloudflare.com/ips-v4 2>/dev/null || echo "")
for cidr in $CF_IPS; do
    ufw allow from $cidr to any port 8642 2>/dev/null || true
done
{{- if .BackendEgressCIDRs}}
for cidr in $(echo "{{.BackendEgressCIDRs}}" | tr ',' ' '); do
    ufw allow from $cidr to any port 8642
    ufw allow from $cidr to any port 22
done
{{- else}}
# No BACKEND_EGRESS_CIDRS configured — SSH open to all as fallback.
ufw allow 22/tcp
{{- end}}
ufw --force enable
log_status "FIREWALL_CONFIGURED"

# --- Set root password (kept as fallback, but SSH key auth is preferred) ---
{{- if .RootPassword}}
echo "root:{{.RootPassword}}" | chpasswd
{{- end}}

# --- SSH key-based auth ---
{{- if .SSHPublicKey}}
mkdir -p /root/.ssh
chmod 700 /root/.ssh
echo "{{.SSHPublicKey}}" >> /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
{{- end}}
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
mkdir -p /etc/ssh/sshd_config.d
printf 'PasswordAuthentication no\nPubkeyAuthentication yes\nPermitRootLogin prohibit-password\n' > /etc/ssh/sshd_config.d/60-tardi.conf
systemctl restart sshd || systemctl restart ssh || true
log_status "SSH_KEY_CONFIGURED"

# --- Install Docker from official repository ---
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
apt-get update -qq
apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin

systemctl enable docker
systemctl start docker
log_status "DOCKER_INSTALLED"

# --- Create hermes user (UID 1000) with Docker access ---
useradd -r -m -u 1000 -s /usr/sbin/nologin hermes || true
usermod -aG docker hermes

# --- Directory structure ---
mkdir -p /opt/hermes/data/workspace /opt/hermes/docker-cli/bin /opt/hermes/docker-cli/cli-plugins
chown -R 1000:1000 /opt/hermes/data

# --- Docker CLI bridge for the Hermes container ---
# Hermes runs in a container, but application containers should be created on
# the VPS Docker daemon. Bind-mount a known-good host CLI and Compose plugin
# into the Hermes container so docker, docker compose, and docker-compose
# work from chat commands.
DOCKER_BIN=$(command -v docker || true)
if [ -n "$DOCKER_BIN" ]; then
    cp -f "$DOCKER_BIN" /opt/hermes/docker-cli/bin/docker
fi
COMPOSE_PLUGIN=""
for path in \
    /usr/libexec/docker/cli-plugins/docker-compose \
    /usr/lib/docker/cli-plugins/docker-compose \
    /usr/local/lib/docker/cli-plugins/docker-compose; do
    if [ -x "$path" ]; then
        COMPOSE_PLUGIN="$path"
        break
    fi
done
if [ -n "$COMPOSE_PLUGIN" ]; then
    cp -f "$COMPOSE_PLUGIN" /opt/hermes/docker-cli/cli-plugins/docker-compose
fi
cat > /opt/hermes/docker-cli/bin/docker-compose <<'DOCKERCOMPOSEEOF'
#!/bin/sh
exec docker compose "$@"
DOCKERCOMPOSEEOF
chmod 755 /opt/hermes/docker-cli/bin/docker /opt/hermes/docker-cli/bin/docker-compose 2>/dev/null || true
chmod 755 /opt/hermes/docker-cli/cli-plugins/docker-compose 2>/dev/null || true

# --- Environment file ---
DOCKER_GID=$(getent group docker | cut -d: -f3)
HERMES_GID=$(getent group hermes | cut -d: -f3 || true)
[ -n "$HERMES_GID" ] || HERMES_GID=1000
cat > /opt/hermes/.env <<ENVEOF
DOCKER_GID=${DOCKER_GID}
HERMES_UID=1000
HERMES_GID=${HERMES_GID}
AGENT_TOKEN={{.AgentToken}}
API_URL={{.APIURL}}
INSTANCE_ID={{.InstanceID}}
API_SERVER_ENABLED=true
API_SERVER_HOST=0.0.0.0
API_SERVER_PORT=8642
API_SERVER_KEY={{.APIServerKey}}
API_SERVER_CORS_ORIGINS=*
HERMES_DASHBOARD=1
HERMES_DASHBOARD_HOST=127.0.0.1
HERMES_DASHBOARD_PORT=9119
HERMES_DASHBOARD_TUI=1
TERMINAL_ENV=local
DOCKER_HOST=unix:///var/run/docker.sock
HERMES_DOCKER_BINARY=/usr/local/bin/docker
SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
SSL_CERT_DIR=/etc/ssl/certs
OPENROUTER_API_KEY={{.OpenRouterAPIKey}}
{{- if .BackendEgressCIDRs}}
BACKEND_EGRESS_CIDRS={{.BackendEgressCIDRs}}
{{- end}}
{{- if .PreviewDomain}}
PREVIEW_DOMAIN={{.PreviewDomain}}
{{- end}}
ENVEOF
{{- if .AnthropicAPIKey}}
echo "ANTHROPIC_API_KEY={{.AnthropicAPIKey}}" >> /opt/hermes/.env
{{- end}}
{{- if .OpenAIAPIKey}}
echo "OPENAI_API_KEY={{.OpenAIAPIKey}}" >> /opt/hermes/.env
{{- end}}
{{- if .TelegramBotToken}}
echo "TELEGRAM_BOT_TOKEN={{.TelegramBotToken}}" >> /opt/hermes/.env
echo "TELEGRAM_REPLY_TO_MODE=off" >> /opt/hermes/.env
{{- if .TelegramAllowedUsers}}
echo "TELEGRAM_ALLOWED_USERS={{.TelegramAllowedUsers}}" >> /opt/hermes/.env
{{- else}}
echo "GATEWAY_ALLOW_ALL_USERS=true" >> /opt/hermes/.env
{{- end}}
{{- end}}
chmod 600 /opt/hermes/.env
cp /opt/hermes/.env /opt/hermes/data/.env
chown 1000:1000 /opt/hermes/data/.env
chmod 600 /opt/hermes/data/.env
{{- if .ConfigVersion}}
echo "{{.ConfigVersion}}" > /opt/hermes/.config_version
{{- end}}

# --- Hermes config ---
cat > /opt/hermes/data/config.yaml <<CFGEOF
{{- if .Model}}
model:
  provider: "{{.Provider}}"
  model: "{{.Model}}"
{{- end}}
terminal:
  backend: local
  cwd: "/opt/hermes/data/workspace"
CFGEOF
chown 1000:1000 /opt/hermes/data/config.yaml
chmod 640 /opt/hermes/data/config.yaml

cat > /opt/hermes/data/SOUL.md <<SOULEOF
You are a helpful AI assistant running on Tardi.
You can execute code, browse the web, manage files, and help with various tasks.
Docker and Docker Compose are available on this dedicated VPS. For web apps, bind the app to host port 3000 so the preview domain can reach it.
Be concise and helpful in your responses.
SOULEOF
chown 1000:1000 /opt/hermes/data/SOUL.md

# --- TLS: Cloudflare Proxy handles TLS at the edge ---
log_status "TLS_CLOUDFLARE_PROXY"

# --- Caddy reverse proxy: hostname-based routing on port 80 ---
for i in 1 2 3; do
    curl -sL "https://caddyserver.com/api/download?os=linux&arch=amd64" -o /usr/local/bin/caddy && break
    sleep 3
done
chmod +x /usr/local/bin/caddy

mkdir -p /etc/caddy
cat > /etc/caddy/Caddyfile <<CADDYEOF
{{- if .PreviewDomain}}
http://{{.PreviewDomain}} {
    reverse_proxy localhost:3000
}
{{- end}}

http:// {
    handle /v1/* {
        reverse_proxy localhost:8642
    }
    handle /health {
        reverse_proxy localhost:8642
    }
    handle {
        reverse_proxy localhost:9118
    }
}
CADDYEOF

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
systemctl enable caddy
systemctl start caddy
log_status "CADDY_PROXY_CONFIGURED"

# --- Docker Compose (single container, host networking) ---
cat > /opt/hermes/docker-compose.yml <<'COMPOSEEOF'
services:
  hermes-agent:
    image: nousresearch/hermes-agent:{{.HermesImageTag}}
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
      - ./data:/opt/hermes/data:rw
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/hermes/docker-cli/bin/docker:/usr/local/bin/docker:ro
      - /opt/hermes/docker-cli/bin/docker-compose:/usr/local/bin/docker-compose:ro
      - /opt/hermes/docker-cli/cli-plugins:/usr/local/lib/docker/cli-plugins:ro
    env_file:
      - .env
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:8642/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 60s

COMPOSEEOF
log_status "FILES_WRITTEN"

# --- Pre-pull image (retry on failure) ---
for i in 1 2 3; do
    docker compose -f /opt/hermes/docker-compose.yml pull && break
    log_status "DOCKER_PULL_RETRY_$i"
    sleep 10
done
log_status "IMAGES_PULLED"

# --- Systemd service for Docker Compose stack ---
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

# --- Tardi dashboard auth shim ---
for i in $(seq 1 10); do
    if curl -fsSL -H "Authorization: Bearer {{.AgentToken}}" \
        "{{.APIURL}}/api/agent/dashboard-shim" \
        -o /usr/local/bin/tardi-dashboard-shim; then
        chmod +x /usr/local/bin/tardi-dashboard-shim
        break
    fi
    sleep 5
done

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

# --- Heartbeat script ---
for i in $(seq 1 10); do
    if curl -sf -H "Authorization: Bearer {{.AgentToken}}" "{{.APIURL}}/api/agent/heartbeat-script" -o /opt/hermes/heartbeat.sh; then
        chmod +x /opt/hermes/heartbeat.sh
        break
    fi
    sleep 5
done

# --- Heartbeat systemd timer (every 5 minutes) ---
cat > /etc/systemd/system/hermes-heartbeat.service <<'HBSVCEOF'
[Unit]
Description=Hermes Heartbeat

[Service]
Type=oneshot
ExecStart=/opt/hermes/heartbeat.sh
HBSVCEOF

cat > /etc/systemd/system/hermes-heartbeat.timer <<'HBTEOF'
[Unit]
Description=Hermes Heartbeat Timer

[Timer]
OnBootSec=90s
OnUnitActiveSec=300s
AccuracySec=10s

[Install]
WantedBy=timers.target
HBTEOF

# --- Start everything ---
systemctl daemon-reload
systemctl enable hermes-stack
systemctl start hermes-stack
systemctl enable tardi-dashboard-shim
systemctl start tardi-dashboard-shim
systemctl enable hermes-heartbeat.timer
systemctl start hermes-heartbeat.timer
log_status "SERVICES_STARTED"

# --- Wait for API to be healthy ---
HEALTHY=false
for i in $(seq 1 60); do
    if curl -sf http://localhost:8642/health >/dev/null 2>&1; then
        HEALTHY=true
        break
    fi
    sleep 3
done

if [ "$HEALTHY" = true ]; then
    log_status "HERMES_HEALTHY"
else
    log_status "HERMES_HEALTH_TIMEOUT"
fi

log_status "COMPLETED"
`))

// RenderHermesCloudInit renders the Hermes cloud-init template with the given data.
func RenderHermesCloudInit(data HermesCloudInitData) (string, error) {
	if data.HermesImageTag == "" {
		data.HermesImageTag = "latest"
	}
	var buf bytes.Buffer
	if err := hermesCloudInitTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
