package jobs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
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

// cloudInitTemplate is the user-data script for bootstrapping a new VPS.
var cloudInitTemplate = template.Must(template.New("cloudinit").Parse(`#!/bin/bash
set -euo pipefail

# --- System setup ---
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq docker.io curl jq

systemctl enable docker
systemctl start docker

# --- Agent configuration ---
AGENT_TOKEN="{{.AgentToken}}"
API_URL="{{.APIURL}}"
INSTANCE_ID="{{.InstanceID}}"

# --- Create agent config ---
mkdir -p /opt/openclaw
cat > /opt/openclaw/env <<ENVEOF
AGENT_TOKEN=${AGENT_TOKEN}
API_URL=${API_URL}
INSTANCE_ID=${INSTANCE_ID}
ENVEOF

# --- Create systemd service ---
cat > /etc/systemd/system/openclaw-agent.service <<SVCEOF
[Unit]
Description=OpenClaw AI Agent
After=docker.service
Requires=docker.service

[Service]
Type=simple
Restart=always
RestartSec=10
EnvironmentFile=/opt/openclaw/env
ExecStartPre=-/usr/bin/docker pull ghcr.io/openclaw/agent:latest
ExecStart=/usr/bin/docker run --rm --name openclaw-agent \
  --env-file /opt/openclaw/env \
  ghcr.io/openclaw/agent:latest
ExecStop=/usr/bin/docker stop openclaw-agent

[Install]
WantedBy=multi-user.target
SVCEOF

systemctl daemon-reload
systemctl enable openclaw-agent
systemctl start openclaw-agent
`))

type Provisioner struct {
	pool     *pgxpool.Pool
	registry *provider.Registry
	logger   *slog.Logger
	apiURL   string
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
	inst, err := getInstanceInternal(ctx, p.pool, job.VpsInstanceID)
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
	inst, err := getInstanceInternal(ctx, p.pool, job.VpsInstanceID)
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
	agentToken, err := generateAgentToken()
	if err != nil {
		return fmt.Errorf("generate agent token: %w", err)
	}
	if err := db.UpdateInstanceAgentToken(ctx, p.pool, inst.ID, agentToken); err != nil {
		return fmt.Errorf("store agent token: %w", err)
	}

	// Render cloud-init user data
	userData, err := renderCloudInit(agentToken, p.apiURL, inst.ID.String())
	if err != nil {
		return fmt.Errorf("render cloud-init: %w", err)
	}

	server, err := prov.CreateServer(ctx, provider.CreateServerRequest{
		Name:       sanitizeServerName(inst.Name),
		ServerType: mapping.ProviderServerType,
		Region:     mapping.ProviderRegion,
		Image:      mapping.ProviderImage,
		UserData:   userData,
		Labels: map[string]string{
			"instance_id": inst.ID.String(),
			"user_id":     inst.UserID.String(),
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
	inst, err := getInstanceInternal(ctx, p.pool, job.VpsInstanceID)
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
	// Wait for the first heartbeat from the agent
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for agent heartbeat: %w", ctx.Err())
		case <-ticker.C:
			inst, err := getInstanceInternal(ctx, p.pool, job.VpsInstanceID)
			if err != nil {
				return fmt.Errorf("get instance: %w", err)
			}
			if inst.LastHeartbeatAt != nil {
				p.logger.Info("provisioner: agent heartbeat received",
					"instance_id", inst.ID,
					"heartbeat_at", inst.LastHeartbeatAt,
				)
				return nil
			}
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
func getInstanceInternal(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) (*models.VpsInstance, error) {
	inst := &models.VpsInstance{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, subscription_id, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, last_heartbeat_at, created_at, updated_at
		FROM vps_instances WHERE id = $1
	`, instanceID).Scan(
		&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Provider,
		&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
		&inst.Region, &inst.Status,
		&inst.RootPassword, &inst.AgentTokenSecretName, &inst.LastHeartbeatAt,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get instance internal: %w", err)
	}
	return inst, nil
}

// generateAgentToken creates a cryptographically random 32-byte hex token.
// sanitizeServerName converts a user-provided name into a valid Hetzner server
// name (lowercase alphanumeric and hyphens, starting with a letter).
func sanitizeServerName(name string) string {
	s := strings.ToLower(name)
	s = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(s, "-")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		s = "agent-" + s
	}
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}

func generateAgentToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// renderCloudInit generates the cloud-init user-data script.
func renderCloudInit(agentToken, apiURL, instanceID string) (string, error) {
	var buf bytes.Buffer
	err := cloudInitTemplate.Execute(&buf, struct {
		AgentToken string
		APIURL     string
		InstanceID string
	}{
		AgentToken: agentToken,
		APIURL:     apiURL,
		InstanceID: instanceID,
	})
	if err != nil {
		return "", fmt.Errorf("execute cloud-init template: %w", err)
	}
	return buf.String(), nil
}
