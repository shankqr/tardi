package jobs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/dns"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
	"github.com/shanq/tardi/internal/sshexec"
)

// frameworkCodes maps agent framework names to short codes for server naming.
var frameworkCodes = map[string]string{
	"openclaw": "oc",
	"hermes":   "hm",
}

// Per-step timeouts per the architecture spec.
var stepTimeouts = map[models.ProvisioningStep]time.Duration{
	models.StepSelectProvider:  2 * time.Minute,
	models.StepCreateServer:    5 * time.Minute,
	models.StepWaitServerReady: 5 * time.Minute,
	models.StepBootstrap:       10 * time.Minute,
	models.StepInstallAgent:    10 * time.Minute,
	models.StepActivate:        1 * time.Minute,
}

// fallbackDefaultModelID is used when the DB models table is unavailable.
// Codex (ChatGPT-linked) is the default routing path for new instances —
// users link via the FE to enable outbound calls.
const fallbackDefaultModelID = "openai-codex/gpt-5.5"
const fallbackDefaultProvider = "openai-codex"

// CloudInitModel pairs a catalog model id with its routing provider so the
// cloud-init template can decide per-model whether to prepend "openrouter/".
type CloudInitModel struct {
	ID       string
	Provider string
}

// CloudInitData holds all template variables for cloud-init rendering.
type CloudInitData struct {
	AgentToken         string           // Tardi API auth token
	APIURL             string           // Tardi backend URL
	InstanceID         string           // VPS instance UUID
	OpenRouterAPIKey   string           // Default LLM provider (required)
	AnthropicAPIKey    string           // Optional direct Anthropic access
	OpenAIAPIKey       string           // Optional direct OpenAI access
	OpenClawAuthToken  string           // Auto-generated, for OpenClaw's own auth
	OpenClawImageTag   string           // e.g. "latest" or "v1.2.3"
	Provider           string           // AI provider: openrouter, anthropic, openai
	Model              string           // Model ID for the provider
	ConfigVersion      int              // Initial config version to prevent redundant first sync
	RootPassword       string           // Explicitly set root password (overrides Hetzner's auto-generated one)
	SSHPublicKey       string           // Ed25519 public key for key-based SSH auth (injected into authorized_keys)
	Domain             string           // Optional domain for Cloudflare Proxy (e.g. "abc12345.tardi.ai"); empty = IP-only access
	PreviewDomain      string           // Optional preview domain (e.g. "abc12345-b.tardi.ai") for user-built apps on port 3000
	BackendEgressCIDRs string           // Comma-separated CIDRs for backend egress IPs (restricts UFW SSH + OpenClaw access)
	AllModels          []CloudInitModel // All enabled catalog models (id + provider) for per-model routing
}

// cloudInitTemplate is the user-data script for bootstrapping a new VPS
// with OpenClaw via Docker host networking + Cloudflare Proxy for TLS.
var cloudInitTemplate = template.Must(template.New("cloudinit").Parse(`#!/bin/bash
set -euo pipefail
exec > >(tee -a /var/log/openclaw-init.log) 2>&1

STATUS_FILE=/opt/openclaw/.init-status
mkdir -p /opt/openclaw
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
apt-get install -y -qq ca-certificates curl gnupg jq ufw
log_status "BASE_PACKAGES_INSTALLED"

# --- Desktop application base packages ---
# Chrome is preinstalled so the private Ubuntu desktop has a browser available
# before a user opens it. Keep this non-fatal; desktop.install retries the same
# package setup later if an upstream repo has a transient failure.
if bash -euo pipefail <<'DESKTOPAPPSEOF'
export DEBIAN_FRONTEND=noninteractive
install -m 0755 -d /usr/share/keyrings

curl -fsSL https://dl.google.com/linux/linux_signing_key.pub \
    | gpg --dearmor > /usr/share/keyrings/google-linux-signing-keyring.gpg
printf '%s\n' 'deb [arch=amd64 signed-by=/usr/share/keyrings/google-linux-signing-keyring.gpg] https://dl.google.com/linux/chrome/deb/ stable main' \
    > /etc/apt/sources.list.d/google-chrome.list

apt-get update -qq
apt-get install -y -qq google-chrome-stable
DESKTOPAPPSEOF
then
    log_status "DESKTOP_APPS_INSTALLED"
else
    log_status "DESKTOP_APPS_INSTALL_FAILED"
fi

# --- Firewall ---
ufw default deny incoming
ufw default allow outgoing
ufw allow 80/tcp
# Port 18789: only allow from Cloudflare IPs (for NAT'd port-80 traffic) and
# backend egress IPs (for direct WebSocket RPC). Without this, anyone who
# scans the VPS IP can connect directly to OpenClaw bypassing Cloudflare.
CF_IPS=$(curl -sf https://www.cloudflare.com/ips-v4 2>/dev/null || echo "")
for cidr in $CF_IPS; do
    ufw allow from $cidr to any port 18789 2>/dev/null || true
done
{{- if .BackendEgressCIDRs}}
for cidr in $(echo "{{.BackendEgressCIDRs}}" | tr ',' ' '); do
    ufw allow from $cidr to any port 18789
    ufw allow from $cidr to any port 22
done
{{- else}}
# No BACKEND_EGRESS_CIDRS configured — SSH open to all as fallback.
# Set BACKEND_EGRESS_CIDRS env var on the backend to restrict SSH access.
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
# Disable password auth, allow key-based root login only
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

# --- Build sandbox image for OpenClaw tool execution ---
# The sandbox image is required for executing tools in chat sessions.
# Try the built-in setup command first, fall back to pulling pre-built image.
docker pull ghcr.io/openclaw/openclaw:{{.OpenClawImageTag}}
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
    ghcr.io/openclaw/openclaw:{{.OpenClawImageTag}} openclaw setup --build-sandbox 2>/dev/null || \
    docker pull ghcr.io/openclaw/openclaw-sandbox:bookworm-slim 2>/dev/null || \
    log_status "SANDBOX_BUILD_SKIPPED"
log_status "SANDBOX_READY"

# --- Create openclaw user (UID 1000) with Docker access ---
useradd -r -m -u 1000 -s /usr/sbin/nologin openclaw || true
usermod -aG docker openclaw

# --- Host admin helper (root bridge via Unix socket) ---
for i in $(seq 1 10); do
    if curl -sf -H "Authorization: Bearer {{.AgentToken}}" "{{.APIURL}}/api/agent/host-admin-script" -o /opt/openclaw/install-host-admin.sh; then
        chmod +x /opt/openclaw/install-host-admin.sh
        /opt/openclaw/install-host-admin.sh && break
    fi
    sleep 5
done
log_status "HOST_ADMIN_READY"

# --- Directory structure ---
mkdir -p /opt/openclaw/data/openclaw /opt/openclaw/data/gogcli /opt/openclaw/data/codex
chown -R 1000:1000 /opt/openclaw/data

# --- OpenClaw config ---
# bind=lan: listen on 0.0.0.0 (host network exposes directly)
# auth=token: OpenClaw reads OPENCLAW_GATEWAY_TOKEN from env.
#   Internal tool calls authenticate automatically (OpenClaw knows its own token).
# trustedProxies: Cloudflare terminates TLS and adds X-Forwarded-For headers.
#   Without this, OpenClaw sees proxy headers from an untrusted address and refuses
#   to grant operator scopes, causing "missing scope: operator.read" errors.
#   Using 0.0.0.0/0 is safe because auth is still enforced via token mode.
cat > /opt/openclaw/data/openclaw/openclaw.json <<CFGEOF
{
  "gateway": {
    "bind": "lan",
    "trustedProxies": ["0.0.0.0/0"],
    "controlUi": {
      "allowedOrigins": ["*"{{if .Domain}}, "https://{{.Domain}}"{{end}}],
      "dangerouslyDisableDeviceAuth": true,
      "allowInsecureAuth": true
    },
    "auth": {
      "mode": "token"
    }
  },
  "plugins": {
    "entries": {
      "openrouter": { "enabled": true },
      "openai": { "enabled": true },
      "anthropic": { "enabled": true }
    }
  },
  "models": {
    "providers": {
      "openai-codex": {
        "baseUrl": "https://chatgpt.com/backend-api",
        "apiKey": "codex-app-server",
        "auth": "token",
        "api": "openai-codex-responses",
        "models": [
          {
            "id": "gpt-5.5",
            "name": "GPT-5.5",
            "api": "openai-codex-responses",
            "reasoning": true,
            "input": ["text", "image"],
            "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 },
            "contextWindow": 272000,
            "maxTokens": 128000,
            "compat": {
              "supportsReasoningEffort": true,
              "supportsUsageInStreaming": true
            }
          }
        ]
      }
    }
  }
}
CFGEOF
chown 1000:1000 /opt/openclaw/data/openclaw/openclaw.json

# --- Environment file ---
DOCKER_GID=$(getent group docker | cut -d: -f3)
OPENCLAW_GID=$(getent group openclaw | cut -d: -f3 || true)
[ -n "$OPENCLAW_GID" ] || OPENCLAW_GID=1000
cat > /opt/openclaw/.env <<ENVEOF
DOCKER_GID=${DOCKER_GID}
OPENCLAW_GID=${OPENCLAW_GID}
AGENT_TOKEN={{.AgentToken}}
API_URL={{.APIURL}}
INSTANCE_ID={{.InstanceID}}
OPENCLAW_AUTH_TOKEN={{.OpenClawAuthToken}}
OPENCLAW_GATEWAY_TOKEN={{.OpenClawAuthToken}}
OPENROUTER_API_KEY={{.OpenRouterAPIKey}}
NODE_ENV=production
TARDI_HOST_ADMIN_SOCKET=/run/tardi-host-admin/admin.sock
TARDI_HOST_EXEC_TIMEOUT=1800
PATH=/opt/tardi/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
{{- if .BackendEgressCIDRs}}
BACKEND_EGRESS_CIDRS={{.BackendEgressCIDRs}}
{{- end}}
{{- if .PreviewDomain}}
PREVIEW_DOMAIN={{.PreviewDomain}}
{{- end}}
ENVEOF
{{- if .AnthropicAPIKey}}
echo "ANTHROPIC_API_KEY={{.AnthropicAPIKey}}" >> /opt/openclaw/.env
{{- end}}
{{- if .OpenAIAPIKey}}
echo "OPENAI_API_KEY={{.OpenAIAPIKey}}" >> /opt/openclaw/.env
{{- end}}
chmod 600 /opt/openclaw/.env
{{- if .ConfigVersion}}
echo "{{.ConfigVersion}}" > /opt/openclaw/.config_version
{{- end}}

# --- TLS: Cloudflare Proxy handles TLS at the edge ---
# No self-signed certs. Cloudflare terminates TLS and connects to the
# origin on port 80 (HTTP). Caddy reverse proxy handles hostname routing.
log_status "TLS_CLOUDFLARE_PROXY"

# --- Caddy reverse proxy: hostname-based routing on port 80 ---
# Preview domain (e.g., abc12345-b.tardi.ai) → localhost:3000 (user-built apps)
# All other traffic → localhost:18789 (OpenClaw gateway)
# Caddy runs on port 80 as root (no TLS — Cloudflare handles that).
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
    reverse_proxy localhost:18789
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
cat > /opt/openclaw/docker-compose.yml <<'COMPOSEEOF'
services:
  openclaw-gateway:
    image: ghcr.io/openclaw/openclaw:{{.OpenClawImageTag}}
    container_name: openclaw-gateway
    restart: unless-stopped
    network_mode: host
    user: "1000:1000"
    group_add:
      - "${DOCKER_GID}"
      - "${OPENCLAW_GID}"
    volumes:
      - ./data/openclaw:/home/node/.openclaw:rw
      - ./data/gogcli:/home/node/.config/gogcli:rw
      - ./data/codex:/home/node/.codex:rw
      - /var/run/docker.sock:/var/run/docker.sock
      - /run/tardi-host-admin:/run/tardi-host-admin:rw
      - /opt/openclaw/host-admin/bin:/opt/tardi/bin:ro
      - /opt/openclaw/host-admin/bin/tardi-host-admin:/usr/local/bin/tardi-host-admin:ro
      - /opt/openclaw/host-admin/bin/sudo:/usr/local/bin/sudo:ro
      - /opt/openclaw/host-admin/bin/sudo:/usr/bin/sudo:ro
    env_file:
      - .env
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:18789/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 60s

COMPOSEEOF
log_status "FILES_WRITTEN"

# --- Pre-pull images (retry on failure) ---
for i in 1 2 3; do
    docker compose -f /opt/openclaw/docker-compose.yml pull && break
    log_status "DOCKER_PULL_RETRY_$i"
    sleep 10
done
log_status "IMAGES_PULLED"

# --- Systemd service for Docker Compose stack ---
cat > /etc/systemd/system/openclaw-stack.service <<'SVCEOF'
[Unit]
Description=OpenClaw Agent Stack
After=docker.service tardi-host-admin.service
Requires=docker.service
Wants=tardi-host-admin.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/openclaw
ExecStart=/usr/bin/docker compose up -d --remove-orphans
ExecStop=/usr/bin/docker compose down
Restart=on-failure
RestartSec=30

[Install]
WantedBy=multi-user.target
SVCEOF

# --- Heartbeat script (downloaded from backend API to keep cloud-init under 32KB) ---
for i in $(seq 1 10); do
    if curl -sf -H "Authorization: Bearer {{.AgentToken}}" "{{.APIURL}}/api/agent/heartbeat-script" -o /opt/openclaw/heartbeat.sh; then
        chmod +x /opt/openclaw/heartbeat.sh
        break
    fi
    sleep 5
done

# --- Heartbeat systemd timer (every 5 minutes) ---
cat > /etc/systemd/system/openclaw-heartbeat.service <<'HBSVCEOF'
[Unit]
Description=OpenClaw Heartbeat

[Service]
Type=oneshot
ExecStart=/opt/openclaw/heartbeat.sh
HBSVCEOF

cat > /etc/systemd/system/openclaw-heartbeat.timer <<'HBTEOF'
[Unit]
Description=OpenClaw Heartbeat Timer

[Timer]
OnBootSec=90s
OnUnitActiveSec=300s
AccuracySec=10s

[Install]
WantedBy=timers.target
HBTEOF

# --- Start everything ---
systemctl daemon-reload
systemctl enable openclaw-stack
systemctl start openclaw-stack
systemctl enable openclaw-heartbeat.timer
systemctl start openclaw-heartbeat.timer

# --- Wait for gateway to be healthy, then apply post-startup config ---
HEALTHY=false
for i in $(seq 1 30); do
    if docker exec openclaw-gateway curl -sf http://localhost:18789/health >/dev/null 2>&1; then
        HEALTHY=true
        break
    fi
    sleep 2
done

if [ "$HEALTHY" = true ]; then
    # Note: codex CLI is installed on-demand by CodexLinkStartHandler
    # and reinstalled after upgrades by the heartbeat script — keeping
    # it out of cloud-init narrows the race window between the backend
    # marking the instance active (on /health 200) and the models loop
    # below finishing.

    # Register all Tardi catalog models so OC dashboard dropdown matches frontend.
    # Per-model: prepend "openrouter/" only for models routed via OpenRouter;
    # native routes (e.g. codex/*) use the bare id as-is.
    # Register non-active models first, then set active model last.
{{- range .AllModels}}
{{- if ne .ID $.Model}}
{{- if eq .Provider "openrouter"}}
    docker exec openclaw-gateway openclaw models set "openrouter/{{.ID}}" 2>/dev/null
{{- else}}
    docker exec openclaw-gateway openclaw models set "{{.ID}}" 2>/dev/null
{{- end}}
{{- end}}
{{- end}}
{{- if .Model}}
{{- if eq .Provider "openrouter"}}
    docker exec openclaw-gateway openclaw models set "openrouter/{{.Model}}" 2>/dev/null
    docker exec openclaw-gateway openclaw config set agents.defaults.model.primary "openrouter/{{.Model}}"
{{- else}}
    docker exec openclaw-gateway openclaw models set "{{.Model}}" 2>/dev/null
    docker exec openclaw-gateway openclaw config set agents.defaults.model.primary "{{.Model}}"
{{- end}}
{{- end}}

    # OC defaults session.reset.mode to "daily" with atHour=4, archiving every
    # chat session at 04:00 UTC and starting fresh on the next inbound message —
    # users lose chat memory daily and the agent re-runs BOOTSTRAP.md, clobbering
    # IDENTITY.md/USER.md as if first contact. Force "idle" mode with an
    # effectively-infinite window. Must be set via the CLI: raw edits to
    # session.* in openclaw.json get sanitized away on next container start.
    # Schema rejects idleMinutes <= 0, so 100y stands in for "never".
    docker exec openclaw-gateway openclaw config set session.reset.mode idle 2>/dev/null
    docker exec openclaw-gateway openclaw config set session.reset.idleMinutes 52560000 2>/dev/null

    # OC's bundled BOOTSTRAP.md tells the agent to delete itself once identity
    # is established, but agents rarely follow through. Left in place, any
    # session reset re-runs the bootstrap flow which overwrites IDENTITY.md and
    # USER.md as if the user is a stranger.
    rm -f /opt/openclaw/data/openclaw/workspace/BOOTSTRAP.md
fi

log_status "COMPLETED"
`))

type Provisioner struct {
	pool               *pgxpool.Pool
	registry           *provider.Registry
	logger             *slog.Logger
	apiURL             string
	openClawImageTag   string
	dnsClient          *dns.Client // nil if Cloudflare DNS not configured
	backendEgressCIDRs string      // comma-separated CIDRs for UFW restriction
	sshPublicKey       string      // Ed25519 public key for authorized_keys injection
	sshPrivateKey      []byte      // PEM bytes; used to confirm cloud-init COMPLETED over SSH
}

// Execute runs through the provisioning steps for a job.
func (p *Provisioner) Execute(ctx context.Context, job *models.ProvisioningJob) error {
	steps := []struct {
		step   models.ProvisioningStep
		status models.VpsStatus
		fn     func(ctx context.Context, job *models.ProvisioningJob) error
	}{
		{models.StepSelectProvider, models.VpsStatusProvisioning, p.stepSelectProvider},
		{models.StepCreateServer, models.VpsStatusProvisioning, p.stepCreateServer},
		{models.StepWaitServerReady, models.VpsStatusBootstrapping, p.stepWaitServerReady},
		{models.StepBootstrap, models.VpsStatusBootstrapping, p.stepBootstrap},
		{models.StepInstallAgent, models.VpsStatusInstallingAgent, p.stepInstallAgent},
		{models.StepActivate, models.VpsStatusActive, p.stepActivate},
	}

	// Find the starting step
	startIdx := 0
	if job.Step != nil {
		for i, s := range steps {
			if s.step == *job.Step {
				startIdx = i
				break
			}
		}
	}

	for i := startIdx; i < len(steps); i++ {
		s := steps[i]

		// Update job step
		if err := db.UpdateJobStatus(ctx, p.pool, job.ID, models.JobRunning, &s.step, nil); err != nil {
			return fmt.Errorf("update job step: %w", err)
		}

		// Update instance status
		if err := db.UpdateInstanceStatus(ctx, p.pool, job.VpsInstanceID, s.status); err != nil {
			return fmt.Errorf("update instance status: %w", err)
		}

		p.logger.Info("provisioner: executing step",
			"job_id", job.ID,
			"step", s.step,
			"instance_id", job.VpsInstanceID,
		)

		// Apply per-step timeout
		timeout := stepTimeouts[s.step]
		stepCtx, cancel := context.WithTimeout(ctx, timeout)
		err := s.fn(stepCtx, job)
		cancel()

		if err != nil {
			return p.handleStepError(ctx, job, s.step, err)
		}
	}

	// Mark job completed
	completedStep := models.StepActivate
	if err := db.UpdateJobStatus(ctx, p.pool, job.ID, models.JobCompleted, &completedStep, nil); err != nil {
		return fmt.Errorf("mark job completed: %w", err)
	}

	p.logger.Info("provisioner: job completed", "job_id", job.ID, "instance_id", job.VpsInstanceID)
	return nil
}

func (p *Provisioner) handleStepError(_ context.Context, job *models.ProvisioningJob, step models.ProvisioningStep, stepErr error) error {
	errMsg := stepErr.Error()

	// Use a background context for DB writes — these must succeed even when
	// the parent context is canceled (e.g., Cloud Run SIGTERM during deploy).
	// Without this, retry scheduling fails and the job gets orphaned.
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dbCancel()

	if job.Attempts >= job.MaxAttempts {
		// Mark as dead
		if err := db.UpdateJobStatus(dbCtx, p.pool, job.ID, models.JobDead, &step, &errMsg); err != nil {
			p.logger.Error("provisioner: failed to mark job dead", "error", err)
		}
		if err := db.UpdateInstanceStatus(dbCtx, p.pool, job.VpsInstanceID, models.VpsStatusError); err != nil {
			p.logger.Error("provisioner: failed to mark instance error", "error", err)
		}
		p.logger.Error("provisioner: job dead after max retries",
			"job_id", job.ID,
			"step", step,
			"attempts", job.Attempts,
			"error", errMsg,
		)
		return fmt.Errorf("job dead: %w", stepErr)
	}

	// Schedule retry with exponential backoff: 5s * 2^(attempt-1), max 5min
	backoff := time.Duration(5*math.Pow(2, float64(job.Attempts-1))) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	nextRetry := time.Now().Add(backoff)

	if err := db.UpdateJobRetry(dbCtx, p.pool, job.ID, nextRetry, errMsg); err != nil {
		p.logger.Error("provisioner: failed to schedule retry", "error", err)
	}

	p.logger.Warn("provisioner: step failed, scheduling retry",
		"job_id", job.ID,
		"step", step,
		"attempt", job.Attempts,
		"next_retry", nextRetry,
		"error", errMsg,
	)

	return stepErr
}

func (p *Provisioner) stepSelectProvider(ctx context.Context, job *models.ProvisioningJob) error {
	inst, err := GetInstanceInternal(ctx, p.pool, job.VpsInstanceID)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}

	_, err = p.registry.Get(inst.Provider)
	if err != nil {
		return fmt.Errorf("provider not available: %w", err)
	}

	return nil
}

func (p *Provisioner) stepCreateServer(ctx context.Context, job *models.ProvisioningJob) error {
	inst, err := GetInstanceInternal(ctx, p.pool, job.VpsInstanceID)
	if err != nil {
		return err
	}

	// Idempotency: if server was already created in a prior attempt, skip creation
	if inst.ProviderServerID != nil && *inst.ProviderServerID != "" {
		p.logger.Info("provisioner: server already created, skipping",
			"instance_id", inst.ID, "provider_server_id", *inst.ProviderServerID)
		return nil
	}

	prov, err := p.registry.Get(inst.Provider)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}

	// Look up the provider plan mapping for server type and image
	sub, err := db.GetSubscriptionByID(ctx, p.pool, inst.SubscriptionID)
	if err != nil {
		return fmt.Errorf("get subscription: %w", err)
	}

	mapping, err := db.GetBestProviderMapping(ctx, p.pool, sub.PlanTier, inst.Region)
	if err != nil {
		return fmt.Errorf("get provider mapping: %w", err)
	}

	// Generate agent token
	agentToken, err := GenerateAgentToken()
	if err != nil {
		return fmt.Errorf("generate agent token: %w", err)
	}
	if err := db.UpdateInstanceAgentToken(ctx, p.pool, inst.ID, agentToken); err != nil {
		return fmt.Errorf("store agent token: %w", err)
	}

	// Generate framework auth token (OpenClaw gateway token / Hermes API server key)
	frameworkAuthToken, err := GenerateAgentToken()
	if err != nil {
		return fmt.Errorf("generate framework auth token: %w", err)
	}
	if err := db.UpdateInstanceOpenClawAuthToken(ctx, p.pool, inst.ID, frameworkAuthToken); err != nil {
		return fmt.Errorf("store framework auth token: %w", err)
	}

	// Generate root password (set explicitly in cloud-init to avoid Hetzner/cloud-init expiry)
	rootPassword, err := GenerateRootPassword()
	if err != nil {
		return fmt.Errorf("generate root password: %w", err)
	}

	// Compute domain if DNS is configured (known before server creation since it's based on instance ID)
	// Flat domain scheme: <uuid8>.tardi.ai (single-level subdomain for Cloudflare Universal SSL)
	// Preview domain: <uuid8>-b.tardi.ai (hyphen suffix, not sub-subdomain)
	var domain, previewDomain string
	if p.dnsClient != nil {
		subdomain := inst.ID.String()[:8]
		domain = fmt.Sprintf("%s.%s", subdomain, p.dnsClient.BaseDomain())
		previewDomain = fmt.Sprintf("%s-b.%s", subdomain, p.dnsClient.BaseDomain())
	}

	// Resolve default model from DB (falls back to hardcoded constant).
	// Provider is paired with the model so per-model routing in cloud-init
	// stays correct regardless of which provider the catalog default uses.
	defaultModel := fallbackDefaultModelID
	defaultProvider := fallbackDefaultProvider
	if m, err := db.GetDefaultModel(ctx, p.pool); err == nil && m != nil {
		defaultModel = m.ID
		defaultProvider = m.Provider
	} else if err != nil {
		p.logger.Warn("provisioner: could not fetch default model from DB, using fallback", "error", err)
	}

	// Fetch API keys from agent config (with defaults for provider/model)
	agentCfg, err := db.GetAgentConfigByInstanceID(ctx, p.pool, inst.ID)
	if err != nil {
		return fmt.Errorf("get agent config: %w", err)
	}
	providerName := defaultProvider
	modelID := defaultModel
	var openRouterAPIKey, anthropicAPIKey, openAIAPIKey, telegramBotToken, telegramAllowedUsers string
	var configVersion int
	if agentCfg != nil {
		if v, ok := agentCfg.Config["openrouter_api_key"].(string); ok {
			openRouterAPIKey = v
		}
		if v, ok := agentCfg.Config["anthropic_api_key"].(string); ok {
			anthropicAPIKey = v
		}
		if v, ok := agentCfg.Config["openai_api_key"].(string); ok {
			openAIAPIKey = v
		}
		if v, ok := agentCfg.Config["telegram_bot_token"].(string); ok {
			telegramBotToken = v
		}
		if v, ok := agentCfg.Config["telegram_allowed_users"].(string); ok {
			telegramAllowedUsers = v
		}
		if v, ok := agentCfg.Config["provider"].(string); ok && v != "" {
			providerName = v
		}
		if v, ok := agentCfg.Config["model"].(string); ok && v != "" {
			modelID = v
		}
		configVersion = agentCfg.Version
	}

	// Resolve framework and render cloud-init
	framework := inst.Framework
	if framework == "" {
		framework = models.FrameworkOpenClaw
	}
	if framework == models.FrameworkHermes {
		providerName, modelID = models.NormalizeHermesProviderModel(providerName, modelID)
	}

	var userData string
	switch framework {
	case models.FrameworkHermes:
		hermesData := HermesCloudInitData{
			AgentToken:           agentToken,
			APIURL:               p.apiURL,
			InstanceID:           inst.ID.String(),
			APIServerKey:         frameworkAuthToken,
			HermesImageTag:       resolveHermesVersion(ctx, p.pool),
			Provider:             providerName,
			Model:                modelID,
			OpenRouterAPIKey:     openRouterAPIKey,
			AnthropicAPIKey:      anthropicAPIKey,
			OpenAIAPIKey:         openAIAPIKey,
			TelegramBotToken:     telegramBotToken,
			TelegramAllowedUsers: telegramAllowedUsers,
			ConfigVersion:        configVersion,
			RootPassword:         rootPassword,
			SSHPublicKey:         p.sshPublicKey,
			Domain:               domain,
			PreviewDomain:        previewDomain,
			BackendEgressCIDRs:   p.backendEgressCIDRs,
		}
		userData, err = RenderHermesCloudInit(hermesData)
		if err != nil {
			return fmt.Errorf("render hermes cloud-init: %w", err)
		}

	default: // OpenClaw
		ciData := CloudInitData{
			AgentToken:         agentToken,
			APIURL:             p.apiURL,
			InstanceID:         inst.ID.String(),
			OpenClawAuthToken:  frameworkAuthToken,
			OpenClawImageTag:   resolveImageTag(ctx, p.pool, p.openClawImageTag),
			Provider:           providerName,
			Model:              modelID,
			OpenRouterAPIKey:   openRouterAPIKey,
			AnthropicAPIKey:    anthropicAPIKey,
			OpenAIAPIKey:       openAIAPIKey,
			ConfigVersion:      configVersion,
			RootPassword:       rootPassword,
			SSHPublicKey:       p.sshPublicKey,
			Domain:             domain,
			PreviewDomain:      previewDomain,
			BackendEgressCIDRs: p.backendEgressCIDRs,
		}
		// Fetch all enabled models (id+provider) so the cloud-init template can
		// route each model correctly (openrouter/ prefix vs bare id).
		if allModels, err := db.ListEnabledModels(ctx, p.pool); err == nil {
			for _, m := range allModels {
				ciData.AllModels = append(ciData.AllModels, CloudInitModel{ID: m.ID, Provider: m.Provider})
			}
		}
		userData, err = RenderCloudInit(ciData)
		if err != nil {
			return fmt.Errorf("render cloud-init: %w", err)
		}
	}

	// Fetch user email for server labels
	user, err := db.GetUserByID(ctx, p.pool, inst.UserID)
	if err != nil {
		return fmt.Errorf("get user for labels: %w", err)
	}

	server, err := prov.CreateServer(ctx, provider.CreateServerRequest{
		Name:       buildServerName(string(framework), user.Email, inst.ID.String()[:8]),
		ServerType: mapping.ProviderServerType,
		Region:     mapping.ProviderRegion,
		Image:      mapping.ProviderImage,
		UserData:   userData,
		Labels: map[string]string{
			"instance_id": inst.ID.String(),
			"user_id":     inst.UserID.String(),
			"email":       SanitizeLabelValue(user.Email),
		},
	})
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	if err := db.UpdateInstanceProviderInfo(ctx, p.pool, inst.ID, server.ProviderServerID, &server.IPv4); err != nil {
		return fmt.Errorf("update provider info: %w", err)
	}

	// Store our generated password (matches what cloud-init sets via chpasswd)
	if err := db.UpdateInstanceRootPassword(ctx, p.pool, inst.ID, rootPassword); err != nil {
		return fmt.Errorf("store root password: %w", err)
	}

	// Create DNS A record now that we have the IP.
	// Records are created with Cloudflare Proxy enabled (proxied=true) for TLS.
	if _, err := p.createDNSRecord(ctx, inst.ID.String(), server.IPv4); err != nil {
		p.logger.Warn("provisioner: DNS record creation failed (instance will only be accessible via IP:18789)",
			"instance_id", inst.ID, "error", err)
		// Non-fatal but no HTTPS: Cloudflare Proxy requires DNS to work
	}

	return nil
}

// createDNSRecord creates a Cloudflare DNS A record for the instance.
// Returns the domain (e.g. "abc12345.agents.tardi.ai") or empty string if DNS is not configured.
func (p *Provisioner) createDNSRecord(ctx context.Context, instanceID string, ip string) (domain string, err error) {
	if p.dnsClient == nil {
		return "", nil
	}

	// Use first 8 chars of instance UUID as subdomain
	subdomain := instanceID
	if len(subdomain) > 8 {
		subdomain = subdomain[:8]
	}

	recordID, err := p.dnsClient.CreateARecord(ctx, subdomain, ip)
	if err != nil {
		return "", fmt.Errorf("create DNS A record: %w", err)
	}

	domain = fmt.Sprintf("%s.%s", subdomain, p.dnsClient.BaseDomain())

	// Parse UUID for DB update
	instUUID, err := uuid.Parse(instanceID)
	if err != nil {
		return "", fmt.Errorf("parse instance ID: %w", err)
	}

	if err := db.UpdateInstanceDomain(ctx, p.pool, instUUID, domain, recordID); err != nil {
		return "", fmt.Errorf("store domain: %w", err)
	}

	// Create preview DNS record for user-built apps
	// Flat scheme: <uuid8>-b.tardi.ai (hyphen suffix, single-level subdomain)
	previewDomain := fmt.Sprintf("%s-b.%s", subdomain, p.dnsClient.BaseDomain())
	previewRecordID, err := p.dnsClient.CreateARecordForDomain(ctx, previewDomain, ip)
	if err != nil {
		p.logger.Warn("provisioner: preview DNS record creation failed",
			"instance_id", instanceID, "preview_domain", previewDomain, "error", err)
		// Non-fatal: preview URL just won't work
	} else {
		if err := db.UpdateInstancePreviewDNS(ctx, p.pool, instUUID, previewDomain, previewRecordID); err != nil {
			p.logger.Warn("provisioner: store preview DNS failed", "instance_id", instanceID, "error", err)
		}
		p.logger.Info("provisioner: preview DNS record created",
			"instance_id", instanceID, "preview_domain", previewDomain, "ip", ip)
	}

	p.logger.Info("provisioner: DNS record created",
		"instance_id", instanceID,
		"domain", domain,
		"ip", ip,
	)

	return domain, nil
}

func (p *Provisioner) stepWaitServerReady(ctx context.Context, job *models.ProvisioningJob) error {
	inst, err := GetInstanceInternal(ctx, p.pool, job.VpsInstanceID)
	if err != nil {
		return err
	}

	if inst.ProviderServerID == nil {
		return fmt.Errorf("no provider server ID")
	}

	prov, err := p.registry.Get(inst.Provider)
	if err != nil {
		return fmt.Errorf("get provider: %w", err)
	}

	// Poll provider until server is running
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server ready: %w", ctx.Err())
		case <-ticker.C:
			server, getErr := prov.GetServer(ctx, *inst.ProviderServerID)
			if getErr != nil {
				p.logger.Warn("provisioner: poll server status failed", "error", getErr)
				continue
			}
			if server.Status == "running" {
				if server.IPv4 != "" {
					_ = db.UpdateInstanceProviderInfo(ctx, p.pool, inst.ID, server.ProviderServerID, &server.IPv4)
				}
				return nil
			}
			p.logger.Debug("provisioner: server not ready yet",
				"server_id", *inst.ProviderServerID,
				"status", server.Status,
			)
		}
	}
}

func (p *Provisioner) stepBootstrap(ctx context.Context, job *models.ProvisioningJob) error {
	// Cloud-init runs automatically. Wait for the server to finish bootstrap.
	// The actual verification is the first heartbeat in stepInstallAgent.
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout during bootstrap: %w", ctx.Err())
	case <-time.After(30 * time.Second):
		return nil
	}
}

func (p *Provisioner) stepInstallAgent(ctx context.Context, job *models.ProvisioningJob) error {
	inst, err := GetInstanceInternal(ctx, p.pool, job.VpsInstanceID)
	if err != nil {
		return err
	}
	if inst.IPv4 == nil {
		return fmt.Errorf("no IPv4 address available")
	}

	// Always use IP-based HTTP health check during provisioning.
	// Domain-based HTTPS is unreliable here: DNS may not have propagated,
	// TLS certs may not be provisioned yet, and Cloudflare proxy may not
	// cover nested wildcard subdomains.
	healthURL := fmt.Sprintf("http://%s/health", *inst.IPv4)

	tlsClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for agent health: %w", ctx.Err())
		case <-ticker.C:
			resp, err := tlsClient.Get(healthURL) //nolint:gosec // Health check URL is constructed from our own DB
			if err != nil {
				p.logger.Debug("provisioner: agent not ready yet", "error", err, "instance_id", job.VpsInstanceID)
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == 200 {
				p.logger.Info("provisioner: agent health check passed", "instance_id", job.VpsInstanceID)
				// Additionally wait for cloud-init's post-startup models
				// loop to finish. Without this, the backend marks the
				// instance active while cloud-init is still running
				// `openclaw models set ...` inside the container — any
				// user action that triggers `docker compose up -d` (e.g.
				// saving an OpenRouter key via sync-config) recreates
				// the container mid-loop and leaves only a subset of
				// models registered with primary pointing at a random
				// one.
				if err := p.waitForCloudInitCompleted(ctx, inst); err != nil {
					p.logger.Warn("provisioner: cloud-init completion wait failed, proceeding anyway",
						"instance_id", job.VpsInstanceID, "error", err)
				}
				_ = db.UpdateInstanceHeartbeat(ctx, p.pool, inst.ID, nil, nil)
				return nil
			}
			p.logger.Debug("provisioner: agent health non-200", "status", resp.StatusCode, "instance_id", job.VpsInstanceID)
		}
	}
}

// waitForCloudInitCompleted polls the framework-specific init-status file via
// SSH until it contains the "COMPLETED" marker written by cloud-init's final
// log_status call, or until a 3-minute budget elapses.
func (p *Provisioner) waitForCloudInitCompleted(ctx context.Context, inst *models.VpsInstance) error {
	if inst.IPv4 == nil || *inst.IPv4 == "" {
		return fmt.Errorf("no IPv4")
	}
	if inst.RootPassword == nil {
		return fmt.Errorf("no root password")
	}
	statusPath := "/opt/openclaw/.init-status"
	if inst.Framework == models.FrameworkHermes {
		statusPath = "/opt/hermes/.init-status"
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
		out, err := sshexec.RunCommand(*inst.IPv4, p.sshPrivateKey, *inst.RootPassword,
			fmt.Sprintf("grep -q COMPLETED %s && echo DONE || echo PENDING", statusPath),
			10*time.Second)
		if err != nil {
			p.logger.Debug("provisioner: init-status probe failed, retrying",
				"instance_id", inst.ID, "error", err)
			continue
		}
		if strings.Contains(out, "DONE") {
			p.logger.Info("provisioner: cloud-init COMPLETED", "instance_id", inst.ID)
			return nil
		}
	}
	return fmt.Errorf("cloud-init did not COMPLETE within 3 minutes")
}

func (p *Provisioner) stepActivate(ctx context.Context, job *models.ProvisioningJob) error {
	if err := db.UpdateInstanceStatus(ctx, p.pool, job.VpsInstanceID, models.VpsStatusActive); err != nil {
		return fmt.Errorf("activate instance: %w", err)
	}
	// Set agent_status=running immediately — the health check just passed in the
	// previous step, so we know the agent is live. Without this, the frontend
	// shows "Setting up..." until the first heartbeat fires (up to 5 min later).
	running := "running"
	_ = db.UpdateInstanceHeartbeat(ctx, p.pool, job.VpsInstanceID, &running, nil)
	inst, err := GetInstanceInternal(ctx, p.pool, job.VpsInstanceID)
	if err != nil {
		return fmt.Errorf("get instance for version: %w", err)
	}
	_ = db.UpdateInstanceOpenClawVersion(ctx, p.pool, job.VpsInstanceID, resolveFrameworkVersion(ctx, p.pool, inst.Framework, p.openClawImageTag), nil, nil)
	return nil
}

// getInstanceInternal fetches an instance without user scoping (for internal worker use).
func GetInstanceInternal(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) (*models.VpsInstance, error) {
	inst := &models.VpsInstance{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, subscription_id, framework, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, openclaw_auth_token, agent_status, last_heartbeat_at,
		       openclaw_version, target_openclaw_version, openclaw_update_status, openclaw_update_error,
		       domain, dns_record_id,
		       created_at, updated_at
		FROM vps_instances WHERE id = $1
	`, instanceID).Scan(
		&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Framework, &inst.Provider,
		&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
		&inst.Region, &inst.Status,
		&inst.RootPassword, &inst.AgentTokenSecretName, &inst.OpenClawAuthToken, &inst.AgentStatus, &inst.LastHeartbeatAt,
		&inst.OpenClawVersion, &inst.TargetOpenClawVersion, &inst.OpenClawUpdateStatus, &inst.OpenClawUpdateError,
		&inst.Domain, &inst.DNSRecordID,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get instance internal: %w", err)
	}
	return inst, nil
}

// sanitizeLabelValue makes a string safe for use as a Hetzner label value.
// Labels must be ≤63 chars, start/end with alphanumeric, and only contain alphanumerics, dashes, dots.
func SanitizeLabelValue(s string) string {
	var result []byte
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			result = append(result, c)
		case c == '-', c == '.':
			result = append(result, c)
		case c == '@':
			result = append(result, '.') // email @ → dot
		default:
			result = append(result, '-')
		}
	}
	if len(result) > 63 {
		result = result[:63]
	}
	// Trim non-alphanumeric from start and end
	for len(result) > 0 && !isAlphaNum(result[0]) {
		result = result[1:]
	}
	for len(result) > 0 && !isAlphaNum(result[len(result)-1]) {
		result = result[:len(result)-1]
	}
	return string(result)
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// frameworkCode returns the short code for a framework name, or the name itself as fallback.
func frameworkCode(framework string) string {
	if code, ok := frameworkCodes[framework]; ok {
		return code
	}
	return framework
}

// buildServerName creates an RFC 1123 compliant server name: {frameworkCode}-{sanitizedEmail}-{uniqueID}.
// Max 63 chars; the email portion is truncated if the total exceeds the limit.
func buildServerName(framework, email, uniqueID string) string {
	code := frameworkCode(framework)
	sanitizedEmail := SanitizeLabelValue(email)

	// Format: {code}-{email}-{uniqueID}
	name := fmt.Sprintf("%s-%s-%s", code, sanitizedEmail, uniqueID)

	// Truncate to 63 chars by trimming the email if needed
	maxLen := 63
	if len(name) > maxLen {
		// Reserve space for code + "-" + "-" + uniqueID
		overhead := len(code) + 1 + 1 + len(uniqueID)
		maxEmail := maxLen - overhead
		if maxEmail < 1 {
			// Edge case: just use code-uniqueID
			name = fmt.Sprintf("%s-%s", code, uniqueID)
		} else {
			trimmed := sanitizedEmail
			if len(trimmed) > maxEmail {
				trimmed = trimmed[:maxEmail]
			}
			// Trim trailing non-alphanumeric from the truncated email
			for len(trimmed) > 0 && !isAlphaNum(trimmed[len(trimmed)-1]) {
				trimmed = trimmed[:len(trimmed)-1]
			}
			name = fmt.Sprintf("%s-%s-%s", code, trimmed, uniqueID)
		}
	}

	return name
}

func buildUpgradeServerName(framework, email, instancePrefix, attemptID string) string {
	return buildServerName(framework, email, fmt.Sprintf("%s-u%s", instancePrefix, attemptID))
}

func GenerateAgentToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GenerateRootPassword generates a 24-character hex password for VPS root access.
func GenerateRootPassword() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// RenderCloudInit generates the cloud-init user-data script.
func RenderCloudInit(data CloudInitData) (string, error) {
	if data.OpenClawImageTag == "" {
		data.OpenClawImageTag = "latest"
	}
	var buf bytes.Buffer
	if err := cloudInitTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute cloud-init template: %w", err)
	}
	return buf.String(), nil
}
