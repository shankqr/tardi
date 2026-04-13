package jobs

import (
	"bytes"
	"text/template"
)

// HermesCloudInitData holds all template variables for Hermes cloud-init rendering.
type HermesCloudInitData struct {
	AgentToken         string // Tardi API auth token
	APIURL             string // Tardi backend URL
	InstanceID         string // VPS instance UUID
	OpenRouterAPIKey   string // Default LLM provider
	AnthropicAPIKey    string // Optional direct Anthropic access
	OpenAIAPIKey       string // Optional direct OpenAI access
	APIServerKey       string // Hermes HTTP API auth token (stored in openclaw_auth_token column)
	HermesImageTag     string // Version tag for install script (e.g. "v2026.4.8" or "main")
	Provider           string // AI provider: openrouter, anthropic, openai
	Model              string // Model ID for the provider
	ConfigVersion      int    // Initial config version to prevent redundant first sync
	RootPassword       string // Root password for VPS
	SSHPublicKey       string // Ed25519 public key for SSH
	Domain             string // Cloudflare domain (e.g. "abc12345.tardi.ai")
	PreviewDomain      string // Preview domain for port 3000 apps
	BackendEgressCIDRs string // Comma-separated CIDRs for backend egress IPs
}

// hermesCloudInitTemplate installs Hermes natively via the official install
// script (Python 3.11+ / uv / Node.js 22), runs it as a systemd service with
// "hermes gateway" for the HTTP API, and uses Docker only for Hermes's terminal
// backend (tool execution sandboxing).
var hermesCloudInitTemplate = template.Must(template.New("hermes-cloudinit").Parse(`#!/bin/bash
set -euo pipefail
exec > >(tee -a /var/log/hermes-init.log) 2>&1

STATUS_FILE=/opt/hermes/.init-status
mkdir -p /opt/hermes
log_status() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $1" | tee -a "$STATUS_FILE"; }

log_status "STARTED"

# --- Swap (prevents OOM on small instances) ---
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
apt-get install -y -qq ca-certificates curl jq ufw git

# --- Firewall ---
ufw default deny incoming
ufw default allow outgoing
ufw allow 80/tcp
# Port 8642: Hermes API server — only allow from Cloudflare IPs and backend egress
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
ufw allow 22/tcp
{{- end}}
ufw --force enable
log_status "FIREWALL_CONFIGURED"

# --- Set root password ---
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

# --- Install Docker (needed for Hermes terminal backend / tool execution) ---
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
apt-get update -qq
apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin

systemctl enable docker
systemctl start docker
log_status "DOCKER_INSTALLED"

# --- Create hermes user with Docker access + passwordless sudo ---
useradd -r -m -s /bin/bash hermes || true
usermod -aG docker hermes
# Passwordless sudo so the install script can install system packages non-interactively
echo 'hermes ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/hermes

# --- Pre-install system deps that the Hermes install script would ask for interactively ---
apt-get install -y -qq ripgrep ffmpeg 2>/dev/null || true

# --- Install Hermes via official install script ---
# The install script sets up Python 3.11+, Node.js 22, uv, and the hermes CLI.
# --skip-setup avoids the interactive setup wizard.
log_status "HERMES_INSTALLING"
su - hermes -c 'curl -fsSL https://raw.githubusercontent.com/NousResearch/hermes-agent/{{.HermesImageTag}}/scripts/install.sh | bash -s -- --skip-setup' || {
    log_status "HERMES_INSTALL_RETRY"
    sleep 5
    su - hermes -c 'curl -fsSL https://raw.githubusercontent.com/NousResearch/hermes-agent/{{.HermesImageTag}}/scripts/install.sh | bash -s -- --skip-setup'
}
log_status "HERMES_INSTALLED"

# --- Directory structure ---
mkdir -p /opt/hermes/data/memories /opt/hermes/data/skills /opt/hermes/data/sessions /opt/hermes/data/logs /opt/hermes/data/hooks /opt/hermes/data/cron
chown -R hermes:hermes /opt/hermes/data

# --- Hermes environment file ---
cat > /opt/hermes/.env <<ENVEOF
AGENT_TOKEN={{.AgentToken}}
API_URL={{.APIURL}}
INSTANCE_ID={{.InstanceID}}
API_SERVER_ENABLED=true
API_SERVER_HOST=0.0.0.0
API_SERVER_PORT=8642
API_SERVER_KEY={{.APIServerKey}}
API_SERVER_CORS_ORIGINS=*
SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
SSL_CERT_DIR=/etc/ssl/certs
OPENROUTER_API_KEY={{.OpenRouterAPIKey}}
{{- if .AnthropicAPIKey}}
ANTHROPIC_API_KEY={{.AnthropicAPIKey}}
{{- end}}
{{- if .OpenAIAPIKey}}
OPENAI_API_KEY={{.OpenAIAPIKey}}
{{- end}}
{{- if .BackendEgressCIDRs}}
BACKEND_EGRESS_CIDRS={{.BackendEgressCIDRs}}
{{- end}}
{{- if .PreviewDomain}}
PREVIEW_DOMAIN={{.PreviewDomain}}
{{- end}}
ENVEOF
chmod 600 /opt/hermes/.env
chown hermes:hermes /opt/hermes/.env
{{- if .ConfigVersion}}
echo "{{.ConfigVersion}}" > /opt/hermes/.config_version
chown hermes:hermes /opt/hermes/.config_version
{{- end}}

# Config and SOUL.md are written into ~/.hermes/ after install (see below)

# --- Write API keys into Hermes's own .env at ~/.hermes/.env ---
# The install script creates ~/.hermes/ with its own .env template.
# We append our API keys there so Hermes's dotenv loader finds them.
HERMES_ENV="/home/hermes/.hermes/.env"
if [ -f "$HERMES_ENV" ]; then
    # Remove any existing placeholder keys and add ours
    sed -i '/^#\?\s*OPENROUTER_API_KEY=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*ANTHROPIC_API_KEY=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*OPENAI_API_KEY=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*API_SERVER_ENABLED=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*API_SERVER_HOST=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*API_SERVER_PORT=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*API_SERVER_KEY=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*API_SERVER_CORS_ORIGINS=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*SSL_CERT_FILE=/d' "$HERMES_ENV"
    sed -i '/^#\?\s*SSL_CERT_DIR=/d' "$HERMES_ENV"
fi
cat >> "$HERMES_ENV" <<HERMESENVEOF
API_SERVER_ENABLED=true
API_SERVER_HOST=0.0.0.0
API_SERVER_PORT=8642
API_SERVER_KEY={{.APIServerKey}}
API_SERVER_CORS_ORIGINS=*
SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
SSL_CERT_DIR=/etc/ssl/certs
{{- if .OpenRouterAPIKey}}
OPENROUTER_API_KEY={{.OpenRouterAPIKey}}
{{- end}}
{{- if .AnthropicAPIKey}}
ANTHROPIC_API_KEY={{.AnthropicAPIKey}}
{{- end}}
{{- if .OpenAIAPIKey}}
OPENAI_API_KEY={{.OpenAIAPIKey}}
{{- end}}
HERMESENVEOF
chown hermes:hermes "$HERMES_ENV"

# --- Set model via Hermes CLI (writes to ~/.hermes/config.yaml) ---
{{- if .Model}}
su - hermes -c 'hermes config set model.default "{{.Model}}"' || true
{{- end}}

# --- Write SOUL.md into Hermes's config dir ---
cat > /home/hermes/.hermes/SOUL.md <<SOULEOF
You are a helpful AI assistant running on Tardi.
You can execute code, browse the web, manage files, and help with various tasks.
Be concise and helpful in your responses.
SOULEOF
chown hermes:hermes /home/hermes/.hermes/SOUL.md

log_status "CONFIG_WRITTEN"

# --- TLS: Cloudflare Proxy handles TLS at the edge ---
log_status "TLS_CLOUDFLARE_PROXY"

# --- Caddy reverse proxy ---
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
    reverse_proxy localhost:8642
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

# --- Hermes systemd service (native process, not Docker) ---
# Find the hermes binary path (installed by the install script under hermes user)
HERMES_BIN=$(su - hermes -c 'which hermes' 2>/dev/null || echo "/home/hermes/.local/bin/hermes")

cat > /etc/systemd/system/hermes-agent.service <<SVCEOF
[Unit]
Description=Hermes Agent Gateway
After=network.target docker.service

[Service]
Type=simple
User=hermes
Group=hermes
EnvironmentFile=/opt/hermes/.env
Environment=HOME=/home/hermes
Environment=PATH=/home/hermes/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=${HERMES_BIN} gateway
Restart=always
RestartSec=10
WorkingDirectory=/opt/hermes/data

[Install]
WantedBy=multi-user.target
SVCEOF

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
systemctl enable hermes-agent
systemctl start hermes-agent
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
	var buf bytes.Buffer
	if err := hermesCloudInitTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
