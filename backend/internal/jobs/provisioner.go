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
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

// Per-step timeouts per the architecture spec.
var stepTimeouts = map[models.ProvisioningStep]time.Duration{
	models.StepSelectProvider:  2 * time.Minute,
	models.StepCreateServer:    5 * time.Minute,
	models.StepWaitServerReady: 5 * time.Minute,
	models.StepBootstrap:       10 * time.Minute,
	models.StepInstallAgent:    10 * time.Minute,
	models.StepActivate:        1 * time.Minute,
}

// CloudInitData holds all template variables for cloud-init rendering.
type CloudInitData struct {
	AgentToken        string // Tardi API auth token
	APIURL            string // Tardi backend URL
	InstanceID        string // VPS instance UUID
	OpenRouterAPIKey  string // Default LLM provider (required)
	AnthropicAPIKey   string // Optional direct Anthropic access
	OpenAIAPIKey      string // Optional direct OpenAI access
	OpenClawAuthToken string // Auto-generated, for OpenClaw's own auth
	OpenClawImageTag  string // e.g. "latest" or "v1.2.3"
}

// cloudInitTemplate is the user-data script for bootstrapping a new VPS
// with OpenClaw via Docker Compose + Caddy reverse proxy.
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
apt-get install -y -qq ca-certificates curl jq ufw

# --- Firewall ---
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable
log_status "FIREWALL_CONFIGURED"

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

# --- Create openclaw user (UID 1000) for container security ---
useradd -r -m -u 1000 -s /usr/sbin/nologin openclaw || true

# --- Directory structure ---
mkdir -p /opt/openclaw/data/openclaw
chown -R 1000:1000 /opt/openclaw/data

# --- OpenClaw config ---
# bind=lan: listen on 0.0.0.0 so Caddy can reach the gateway via Docker network
# auth token: pre-set so OpenClaw doesn't auto-generate a different one
cat > /opt/openclaw/data/openclaw/openclaw.json <<CFGEOF
{
  "gateway": {
    "bind": "lan",
    "auth": {
      "mode": "token",
      "token": "{{.OpenClawAuthToken}}"
    }
  }
}
CFGEOF
chown 1000:1000 /opt/openclaw/data/openclaw/openclaw.json

# --- Environment file ---
cat > /opt/openclaw/.env <<'ENVEOF'
AGENT_TOKEN={{.AgentToken}}
API_URL={{.APIURL}}
INSTANCE_ID={{.InstanceID}}
OPENCLAW_AUTH_TOKEN={{.OpenClawAuthToken}}
OPENROUTER_API_KEY={{.OpenRouterAPIKey}}
NODE_ENV=production
ENVEOF
{{- if .AnthropicAPIKey}}
echo "ANTHROPIC_API_KEY={{.AnthropicAPIKey}}" >> /opt/openclaw/.env
{{- end}}
{{- if .OpenAIAPIKey}}
echo "OPENAI_API_KEY={{.OpenAIAPIKey}}" >> /opt/openclaw/.env
{{- end}}
chmod 600 /opt/openclaw/.env

# --- Docker Compose ---
cat > /opt/openclaw/docker-compose.yml <<'COMPOSEEOF'
services:
  openclaw-gateway:
    image: ghcr.io/openclaw/openclaw:{{.OpenClawImageTag}}
    container_name: openclaw-gateway
    restart: unless-stopped
    user: "1000:1000"
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    networks:
      - openclaw-net
    volumes:
      - ./data/openclaw:/home/node/.openclaw:rw
    ports:
      - "127.0.0.1:18789:18789"
    env_file:
      - .env
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:18789/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 60s

  caddy:
    image: caddy:2-alpine
    container_name: openclaw-caddy
    restart: unless-stopped
    networks:
      - openclaw-net
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    env_file:
      - .env
    depends_on:
      openclaw-gateway:
        condition: service_healthy

networks:
  openclaw-net:
    driver: bridge

volumes:
  caddy_data:
  caddy_config:
COMPOSEEOF

# --- Caddyfile ---
cat > /opt/openclaw/Caddyfile <<'CADDYEOF'
:443 {
	tls internal

	@health path /health
	handle @health {
		reverse_proxy openclaw-gateway:18789
	}

	@auth_header {
		header Authorization "Bearer {env.OPENCLAW_AUTH_TOKEN}"
	}
	@auth_query {
		query token={env.OPENCLAW_AUTH_TOKEN}
	}
	handle @auth_header {
		reverse_proxy openclaw-gateway:18789 {
			header_up Connection {header.Connection}
			header_up Upgrade {header.Upgrade}
		}
	}
	handle @auth_query {
		reverse_proxy openclaw-gateway:18789 {
			header_up Connection {header.Connection}
			header_up Upgrade {header.Upgrade}
		}
	}

	respond 401
}

:80 {
	@health path /health
	handle @health {
		reverse_proxy openclaw-gateway:18789
	}
	handle {
		redir https://{host}{uri} permanent
	}
}
CADDYEOF
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
After=docker.service
Requires=docker.service

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

# --- Heartbeat script ---
cat > /opt/openclaw/heartbeat.sh <<'HBEOF'
#!/bin/bash
source /opt/openclaw/.env

# Check OpenClaw gateway health
HEALTH=$(curl -sf http://localhost:18789/health 2>/dev/null)
if [ $? -eq 0 ]; then
    STATUS="running"
else
    # Check if container exists but is unhealthy
    CONTAINER_STATE=$(docker inspect -f '{{"{{"}}.State.Status{{"}}"}}' openclaw-gateway 2>/dev/null)
    if [ "$CONTAINER_STATE" = "running" ]; then
        STATUS="unhealthy"
    elif [ -n "$CONTAINER_STATE" ]; then
        STATUS="stopped"
    else
        STATUS="not_found"
    fi
fi

curl -sf -X POST "${API_URL}/api/agent/heartbeat" \
    -H "Authorization: Bearer ${AGENT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"status\":\"${STATUS}\"}" > /dev/null 2>&1
HBEOF
chmod +x /opt/openclaw/heartbeat.sh

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

log_status "COMPLETED"
`))

type Provisioner struct {
	pool             *pgxpool.Pool
	registry         *provider.Registry
	logger           *slog.Logger
	apiURL           string
	openClawImageTag string
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

func (p *Provisioner) handleStepError(ctx context.Context, job *models.ProvisioningJob, step models.ProvisioningStep, stepErr error) error {
	errMsg := stepErr.Error()

	if job.Attempts >= job.MaxAttempts {
		// Mark as dead
		if err := db.UpdateJobStatus(ctx, p.pool, job.ID, models.JobDead, &step, &errMsg); err != nil {
			p.logger.Error("provisioner: failed to mark job dead", "error", err)
		}
		if err := db.UpdateInstanceStatus(ctx, p.pool, job.VpsInstanceID, models.VpsStatusError); err != nil {
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

	if err := db.UpdateJobRetry(ctx, p.pool, job.ID, nextRetry, errMsg); err != nil {
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

	// Generate OpenClaw auth token
	openClawAuthToken, err := GenerateAgentToken()
	if err != nil {
		return fmt.Errorf("generate openclaw auth token: %w", err)
	}
	if err := db.UpdateInstanceOpenClawAuthToken(ctx, p.pool, inst.ID, openClawAuthToken); err != nil {
		return fmt.Errorf("store openclaw auth token: %w", err)
	}

	// Fetch API keys from agent config
	ciData := CloudInitData{
		AgentToken:        agentToken,
		APIURL:            p.apiURL,
		InstanceID:        inst.ID.String(),
		OpenClawAuthToken: openClawAuthToken,
		OpenClawImageTag:  p.openClawImageTag,
	}
	agentCfg, err := db.GetAgentConfigByInstanceID(ctx, p.pool, inst.ID)
	if err != nil {
		return fmt.Errorf("get agent config: %w", err)
	}
	if agentCfg != nil {
		if v, ok := agentCfg.Config["openrouter_api_key"].(string); ok {
			ciData.OpenRouterAPIKey = v
		}
		if v, ok := agentCfg.Config["anthropic_api_key"].(string); ok {
			ciData.AnthropicAPIKey = v
		}
		if v, ok := agentCfg.Config["openai_api_key"].(string); ok {
			ciData.OpenAIAPIKey = v
		}
	}

	// Render cloud-init user data
	userData, err := RenderCloudInit(ciData)
	if err != nil {
		return fmt.Errorf("render cloud-init: %w", err)
	}

	// Fetch user email for server labels
	user, err := db.GetUserByID(ctx, p.pool, inst.UserID)
	if err != nil {
		return fmt.Errorf("get user for labels: %w", err)
	}

	server, err := prov.CreateServer(ctx, provider.CreateServerRequest{
		Name:       "openclaw",
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

	if server.RootPassword != "" {
		if err := db.UpdateInstanceRootPassword(ctx, p.pool, inst.ID, server.RootPassword); err != nil {
			return fmt.Errorf("store root password: %w", err)
		}
	}

	return nil
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

	healthURL := fmt.Sprintf("http://%s/health", *inst.IPv4)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for agent health: %w", ctx.Err())
		case <-ticker.C:
			resp, err := http.Get(healthURL) //nolint:gosec // Health check URL is constructed from our own DB
			if err != nil {
				p.logger.Debug("provisioner: agent not ready yet", "error", err, "instance_id", job.VpsInstanceID)
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == 200 {
				p.logger.Info("provisioner: agent health check passed", "instance_id", job.VpsInstanceID)
				_ = db.UpdateInstanceHeartbeat(ctx, p.pool, inst.ID, nil)
				return nil
			}
			p.logger.Debug("provisioner: agent health non-200", "status", resp.StatusCode, "instance_id", job.VpsInstanceID)
		}
	}
}

func (p *Provisioner) stepActivate(ctx context.Context, job *models.ProvisioningJob) error {
	if err := db.UpdateInstanceStatus(ctx, p.pool, job.VpsInstanceID, models.VpsStatusActive); err != nil {
		return fmt.Errorf("activate instance: %w", err)
	}
	return nil
}

// getInstanceInternal fetches an instance without user scoping (for internal worker use).
func GetInstanceInternal(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) (*models.VpsInstance, error) {
	inst := &models.VpsInstance{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, subscription_id, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, openclaw_auth_token, agent_status, last_heartbeat_at, created_at, updated_at
		FROM vps_instances WHERE id = $1
	`, instanceID).Scan(
		&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Provider,
		&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
		&inst.Region, &inst.Status,
		&inst.RootPassword, &inst.AgentTokenSecretName, &inst.OpenClawAuthToken, &inst.AgentStatus, &inst.LastHeartbeatAt,
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

func GenerateAgentToken() (string, error) {
	b := make([]byte, 32)
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
