package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

const (
	resumeTimeout      = 20 * time.Minute
	serverPollInterval = 5 * time.Second
)

// Resumer handles re-provisioning suspended instances from their system snapshots.
type Resumer struct {
	pool               *pgxpool.Pool
	registry           *provider.Registry
	logger             *slog.Logger
	apiURL             string
	openClawImageTag   string
	backendEgressCIDRs string
	sshPublicKey       string
}

func NewResumer(pool *pgxpool.Pool, registry *provider.Registry, logger *slog.Logger, apiURL, openClawImageTag, backendEgressCIDRs, sshPublicKey string) *Resumer {
	return &Resumer{
		pool:               pool,
		registry:           registry,
		logger:             logger,
		apiURL:             apiURL,
		openClawImageTag:   openClawImageTag,
		backendEgressCIDRs: backendEgressCIDRs,
		sshPublicKey:       sshPublicKey,
	}
}

// ResumeInstance re-provisions a suspended instance from its system snapshot.
// Runs asynchronously in a goroutine.
func (r *Resumer) ResumeInstance(inst *models.VpsInstance) {
	go r.executeResume(inst)
}

func (r *Resumer) executeResume(inst *models.VpsInstance) {
	ctx, cancel := context.WithTimeout(context.Background(), resumeTimeout)
	defer cancel()

	r.logger.Info("resumer: starting instance resume",
		"instance_id", inst.ID,
		"provider", inst.Provider,
	)

	_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusResuming)

	// Look up the system snapshot
	snap, err := db.GetSystemSnapshotByInstanceID(ctx, r.pool, inst.ID)
	if err != nil {
		r.logger.Error("resumer: get system snapshot", "instance_id", inst.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}
	if snap == nil || snap.ProviderImageID == nil {
		r.logger.Error("resumer: no system snapshot available for resume", "instance_id", inst.ID)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Get provider
	prov, err := r.registry.Get(inst.Provider)
	if err != nil {
		r.logger.Error("resumer: provider not found", "provider", inst.Provider, "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Get subscription and provider mapping
	sub, err := db.GetSubscriptionByID(ctx, r.pool, inst.SubscriptionID)
	if err != nil {
		r.logger.Error("resumer: get subscription", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	mapping, err := db.GetBestProviderMapping(ctx, r.pool, sub.PlanTier, inst.Region)
	if err != nil {
		r.logger.Error("resumer: get provider mapping", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Generate new agent token
	agentToken, err := GenerateAgentToken()
	if err != nil {
		r.logger.Error("resumer: generate agent token", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}
	if err := db.UpdateInstanceAgentToken(ctx, r.pool, inst.ID, agentToken); err != nil {
		r.logger.Error("resumer: store agent token", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Generate framework auth token
	frameworkAuthToken, err := GenerateAgentToken()
	if err != nil {
		r.logger.Error("resumer: generate framework auth token", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}
	if err := db.UpdateInstanceOpenClawAuthToken(ctx, r.pool, inst.ID, frameworkAuthToken); err != nil {
		r.logger.Error("resumer: store framework auth token", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Extract config values
	agentCfg, err := db.GetAgentConfigByInstanceID(ctx, r.pool, inst.ID)
	if err != nil {
		r.logger.Error("resumer: get agent config", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}
	providerName := fallbackDefaultProvider
	modelID := fallbackDefaultModelID
	var openRouterAPIKey, anthropicAPIKey, openAIAPIKey string
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
		if v, ok := agentCfg.Config["provider"].(string); ok && v != "" {
			providerName = v
		}
		if v, ok := agentCfg.Config["model"].(string); ok && v != "" {
			modelID = v
		}
	}

	framework := inst.Framework
	if framework == "" {
		framework = models.FrameworkOpenClaw
	}

	var userData string
	switch framework {
	case models.FrameworkHermes:
		hermesData := HermesCloudInitData{
			AgentToken:         agentToken,
			APIURL:             r.apiURL,
			InstanceID:         inst.ID.String(),
			APIServerKey:       frameworkAuthToken,
			HermesImageTag:     resolveHermesVersion(ctx, r.pool),
			Provider:           providerName,
			Model:              modelID,
			OpenRouterAPIKey:   openRouterAPIKey,
			AnthropicAPIKey:    anthropicAPIKey,
			OpenAIAPIKey:       openAIAPIKey,
			SSHPublicKey:       r.sshPublicKey,
			BackendEgressCIDRs: r.backendEgressCIDRs,
		}
		userData, err = RenderHermesCloudInit(hermesData)
	default:
		ciData := CloudInitData{
			AgentToken:         agentToken,
			APIURL:             r.apiURL,
			InstanceID:         inst.ID.String(),
			OpenClawAuthToken:  frameworkAuthToken,
			OpenClawImageTag:   resolveImageTag(ctx, r.pool, r.openClawImageTag),
			SSHPublicKey:       r.sshPublicKey,
			BackendEgressCIDRs: r.backendEgressCIDRs,
			OpenRouterAPIKey:   openRouterAPIKey,
			AnthropicAPIKey:    anthropicAPIKey,
			OpenAIAPIKey:       openAIAPIKey,
		}
		if allModels, lErr := db.ListEnabledModels(ctx, r.pool); lErr == nil {
			for _, m := range allModels {
				ciData.AllModels = append(ciData.AllModels, CloudInitModel{ID: m.ID, Provider: m.Provider})
			}
		}
		userData, err = RenderCloudInit(ciData)
	}
	if err != nil {
		r.logger.Error("resumer: render cloud-init", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Get user for labels
	user, err := db.GetUserByID(ctx, r.pool, inst.UserID)
	if err != nil {
		r.logger.Error("resumer: get user", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Create new server from snapshot
	server, err := prov.CreateServer(ctx, provider.CreateServerRequest{
		Name:       buildServerName(string(framework), user.Email, inst.ID.String()[:8]),
		ServerType: mapping.ProviderServerType,
		Region:     mapping.ProviderRegion,
		ImageID:    *snap.ProviderImageID,
		UserData:   userData,
		Labels: map[string]string{
			"instance_id": inst.ID.String(),
			"user_id":     inst.UserID.String(),
			"email":       SanitizeLabelValue(user.Email),
		},
	})
	if err != nil {
		r.logger.Error("resumer: create server from snapshot", "instance_id", inst.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Update instance with new server info
	if err := db.UpdateInstanceProviderInfo(ctx, r.pool, inst.ID, server.ProviderServerID, &server.IPv4); err != nil {
		r.logger.Error("resumer: update provider info", "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}
	if server.RootPassword != "" {
		if err := db.UpdateInstanceRootPassword(ctx, r.pool, inst.ID, server.RootPassword); err != nil {
			r.logger.Error("resumer: update root password", "error", err)
		}
	}

	// Poll until server is running
	if err := r.waitForServer(ctx, prov, server.ProviderServerID, inst); err != nil {
		r.logger.Error("resumer: wait for server", "instance_id", inst.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Clean up the system snapshot (no longer needed)
	_ = db.UpdateSnapshotStatus(ctx, r.pool, snap.ID, models.SnapshotStatusDeleting)
	if err := prov.DeleteSnapshot(ctx, *snap.ProviderImageID); err != nil {
		r.logger.Error("resumer: delete system snapshot", "snapshot_id", snap.ID, "error", err)
	}
	_ = db.UpdateSnapshotStatus(ctx, r.pool, snap.ID, models.SnapshotStatusDeleted)

	// Mark instance active
	_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusActive)
	_ = db.UpdateInstanceOpenClawVersion(ctx, r.pool, inst.ID, resolveFrameworkVersion(ctx, r.pool, framework, r.openClawImageTag), nil, nil)

	r.logger.Info("resumer: instance resumed successfully",
		"instance_id", inst.ID,
		"server_id", server.ProviderServerID,
	)
}

func (r *Resumer) waitForServer(ctx context.Context, prov provider.InfraProvider, serverID string, inst *models.VpsInstance) error {
	ticker := time.NewTicker(serverPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server ready: %w", ctx.Err())
		case <-ticker.C:
			server, err := prov.GetServer(ctx, serverID)
			if err != nil {
				r.logger.Warn("resumer: poll server status failed", "error", err)
				continue
			}
			if server.Status == "running" {
				if server.IPv4 != "" {
					_ = db.UpdateInstanceProviderInfo(ctx, r.pool, inst.ID, server.ProviderServerID, &server.IPv4)
				}
				return nil
			}
			r.logger.Debug("resumer: server not ready yet",
				"server_id", serverID,
				"status", server.Status,
			)
		}
	}
}
