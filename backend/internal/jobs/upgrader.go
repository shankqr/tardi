package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/dns"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

const (
	upgradeTimeout   = 25 * time.Minute
	downgradeTimeout = 10 * time.Minute
)

// Upgrader handles plan upgrades (snapshot-based server migration) and downgrades (fresh provisioning).
type Upgrader struct {
	pool             *pgxpool.Pool
	registry         *provider.Registry
	logger           *slog.Logger
	apiURL           string
	openClawImageTag string
	dnsClient        *dns.Client
}

func NewUpgrader(pool *pgxpool.Pool, registry *provider.Registry, logger *slog.Logger, apiURL, openClawImageTag string, dnsClient *dns.Client) *Upgrader {
	return &Upgrader{
		pool:             pool,
		registry:         registry,
		logger:           logger,
		apiURL:           apiURL,
		openClawImageTag: openClawImageTag,
		dnsClient:        dnsClient,
	}
}

// UpgradeInstance migrates an instance to a higher-tier plan by snapshotting,
// creating a new server from the snapshot, and deleting the old server.
// Runs asynchronously in a goroutine.
func (u *Upgrader) UpgradeInstance(inst *models.VpsInstance, newTier models.PlanTier) {
	go u.executeUpgrade(inst, newTier)
}

// DowngradeInstance terminates the current instance and re-provisions on the lower-tier plan.
// All data is lost. Runs asynchronously in a goroutine.
func (u *Upgrader) DowngradeInstance(inst *models.VpsInstance, newTier models.PlanTier) {
	go u.executeDowngrade(inst, newTier)
}

func (u *Upgrader) executeUpgrade(inst *models.VpsInstance, newTier models.PlanTier) {
	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)
	defer cancel()

	u.logger.Info("upgrader: starting plan upgrade",
		"instance_id", inst.ID,
		"new_tier", newTier,
	)

	_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusUpgrading)

	// Get provider
	prov, err := u.registry.Get(inst.Provider)
	if err != nil {
		u.logger.Error("upgrader: provider not found", "provider", inst.Provider, "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	if inst.ProviderServerID == nil {
		u.logger.Error("upgrader: instance has no provider server ID", "instance_id", inst.ID)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	oldServerID := *inst.ProviderServerID

	// Step 1: Create a system snapshot of the current server
	snapName := fmt.Sprintf("upgrade-%d", time.Now().Unix())
	snap := &models.Snapshot{
		ID:            uuid.New(),
		VpsInstanceID: inst.ID,
		Name:          snapName,
		Status:        models.SnapshotStatusCreating,
		IsSystem:      true,
	}
	if err := db.CreateSnapshot(ctx, u.pool, snap); err != nil {
		u.logger.Error("upgrader: create snapshot record", "instance_id", inst.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	snapCtx, snapCancel := context.WithTimeout(ctx, snapshotTimeout)
	result, err := prov.CreateSnapshot(snapCtx, oldServerID, snapName)
	snapCancel()
	if err != nil {
		u.logger.Error("upgrader: create snapshot failed", "instance_id", inst.ID, "error", err)
		_ = db.UpdateSnapshotError(ctx, u.pool, snap.ID, err.Error())
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	if err := db.UpdateSnapshotReady(ctx, u.pool, snap.ID, result.ProviderImageID, result.SizeGB); err != nil {
		u.logger.Error("upgrader: update snapshot ready", "snapshot_id", snap.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	u.logger.Info("upgrader: snapshot created", "instance_id", inst.ID, "snapshot_id", snap.ID)

	// Step 2: Look up the new tier's provider mapping
	mapping, err := db.GetBestProviderMapping(ctx, u.pool, newTier, inst.Region)
	if err != nil {
		u.logger.Error("upgrader: get provider mapping", "tier", newTier, "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Step 3: Generate new tokens and cloud-init
	agentToken, err := GenerateAgentToken()
	if err != nil {
		u.logger.Error("upgrader: generate agent token", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}
	if err := db.UpdateInstanceAgentToken(ctx, u.pool, inst.ID, agentToken); err != nil {
		u.logger.Error("upgrader: store agent token", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	openClawAuthToken, err := GenerateAgentToken()
	if err != nil {
		u.logger.Error("upgrader: generate openclaw auth token", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}
	if err := db.UpdateInstanceOpenClawAuthToken(ctx, u.pool, inst.ID, openClawAuthToken); err != nil {
		u.logger.Error("upgrader: store openclaw auth token", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Generate root password for the new server (cloud-init sets it via chpasswd)
	rootPassword, err := GenerateAgentToken()
	if err != nil {
		u.logger.Error("upgrader: generate root password", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}
	_ = db.UpdateInstanceRootPassword(ctx, u.pool, inst.ID, rootPassword)

	ciData := CloudInitData{
		AgentToken:        agentToken,
		APIURL:            u.apiURL,
		InstanceID:        inst.ID.String(),
		OpenClawAuthToken: openClawAuthToken,
		OpenClawImageTag:  u.openClawImageTag,
		RootPassword:      rootPassword,
	}

	// Preserve domain for Let's Encrypt TLS
	if inst.Domain != nil && *inst.Domain != "" {
		ciData.Domain = *inst.Domain
	}

	agentCfg, err := db.GetAgentConfigByInstanceID(ctx, u.pool, inst.ID)
	if err != nil {
		u.logger.Error("upgrader: get agent config", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}
	if agentCfg != nil {
		ciData.ConfigVersion = agentCfg.Version
		if v, ok := agentCfg.Config["openrouter_api_key"].(string); ok {
			ciData.OpenRouterAPIKey = v
		}
		if v, ok := agentCfg.Config["anthropic_api_key"].(string); ok {
			ciData.AnthropicAPIKey = v
		}
		if v, ok := agentCfg.Config["openai_api_key"].(string); ok {
			ciData.OpenAIAPIKey = v
		}
		if v, ok := agentCfg.Config["telegram_bot_token"].(string); ok {
			ciData.TelegramBotToken = v
		}
		if v, ok := agentCfg.Config["provider"].(string); ok {
			ciData.Provider = v
		}
		if v, ok := agentCfg.Config["model"].(string); ok {
			ciData.Model = v
		}
	}

	userData, err := RenderCloudInit(ciData)
	if err != nil {
		u.logger.Error("upgrader: render cloud-init", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Get user for labels
	user, err := db.GetUserByID(ctx, u.pool, inst.UserID)
	if err != nil {
		u.logger.Error("upgrader: get user", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Step 4: Create new server from snapshot on the new tier
	server, err := prov.CreateServer(ctx, provider.CreateServerRequest{
		Name:       buildServerName("openclaw", user.Email, inst.ID.String()[:8]),
		ServerType: mapping.ProviderServerType,
		Region:     mapping.ProviderRegion,
		ImageID:    result.ProviderImageID,
		UserData:   userData,
		Labels: map[string]string{
			"instance_id": inst.ID.String(),
			"user_id":     inst.UserID.String(),
			"email":       SanitizeLabelValue(user.Email),
		},
	})
	if err != nil {
		u.logger.Error("upgrader: create server from snapshot", "instance_id", inst.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Update instance with new server info
	if err := db.UpdateInstanceProviderInfo(ctx, u.pool, inst.ID, server.ProviderServerID, &server.IPv4); err != nil {
		u.logger.Error("upgrader: update provider info", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}
	if server.RootPassword != "" {
		_ = db.UpdateInstanceRootPassword(ctx, u.pool, inst.ID, server.RootPassword)
	}

	// Step 5: Wait for new server to be running
	if err := u.waitForServer(ctx, prov, server.ProviderServerID, inst); err != nil {
		u.logger.Error("upgrader: wait for server", "instance_id", inst.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	// Step 5b: Reset root password via provider API.
	// Cloud-init doesn't re-run on snapshot-based servers (it detects prior execution),
	// so the password set via chpasswd in cloud-init is never applied. We must reset
	// the password through the provider API to get a working credential.
	newPassword, err := prov.ResetPassword(ctx, server.ProviderServerID)
	if err != nil {
		u.logger.Error("upgrader: reset password after upgrade", "instance_id", inst.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}
	_ = db.UpdateInstanceRootPassword(ctx, u.pool, inst.ID, newPassword)
	u.logger.Info("upgrader: root password reset on new server", "instance_id", inst.ID)

	// Step 6: Update DNS records to point to new server IP
	if u.dnsClient != nil && inst.DNSRecordID != nil && inst.Domain != nil {
		subdomain := inst.ID.String()[:8]
		if err := u.dnsClient.UpdateARecord(ctx, *inst.DNSRecordID, subdomain, server.IPv4); err != nil {
			u.logger.Error("upgrader: update DNS record", "instance_id", inst.ID, "error", err)
			// Non-fatal — continue with upgrade
		} else {
			u.logger.Info("upgrader: DNS record updated", "domain", *inst.Domain, "ip", server.IPv4)
		}
	}
	if u.dnsClient != nil && inst.PreviewDNSRecordID != nil && inst.PreviewDomain != nil {
		if err := u.dnsClient.UpdateARecordForDomain(ctx, *inst.PreviewDNSRecordID, *inst.PreviewDomain, server.IPv4); err != nil {
			u.logger.Error("upgrader: update preview DNS record", "instance_id", inst.ID, "error", err)
		} else {
			u.logger.Info("upgrader: preview DNS record updated", "domain", *inst.PreviewDomain, "ip", server.IPv4)
		}
	}

	// Step 7: Delete the old server
	if err := prov.DeleteServer(ctx, oldServerID); err != nil {
		u.logger.Error("upgrader: delete old server", "old_server_id", oldServerID, "error", err)
		// Non-fatal — old server will be cleaned up manually or by reconciler
	}

	// Step 8: Clean up the upgrade snapshot
	_ = db.UpdateSnapshotStatus(ctx, u.pool, snap.ID, models.SnapshotStatusDeleting)
	if err := prov.DeleteSnapshot(ctx, result.ProviderImageID); err != nil {
		u.logger.Error("upgrader: delete upgrade snapshot", "snapshot_id", snap.ID, "error", err)
	}
	_ = db.UpdateSnapshotStatus(ctx, u.pool, snap.ID, models.SnapshotStatusDeleted)

	// Step 9: Mark instance active (plan tier already updated by webhook)
	_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusActive)
	_ = db.UpdateInstanceOpenClawVersion(ctx, u.pool, inst.ID, u.openClawImageTag, nil, nil)

	u.logger.Info("upgrader: instance upgraded successfully",
		"instance_id", inst.ID,
		"new_tier", newTier,
		"server_id", server.ProviderServerID,
	)
}

func (u *Upgrader) executeDowngrade(inst *models.VpsInstance, newTier models.PlanTier) {
	ctx, cancel := context.WithTimeout(context.Background(), downgradeTimeout)
	defer cancel()

	u.logger.Info("upgrader: starting plan downgrade",
		"instance_id", inst.ID,
		"new_tier", newTier,
	)

	_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusDowngrading)

	// Step 1: Delete current server and all snapshots
	if inst.ProviderServerID != nil {
		prov, err := u.registry.Get(inst.Provider)
		if err != nil {
			u.logger.Error("upgrader: provider not found", "provider", inst.Provider, "error", err)
		} else {
			// Delete all snapshots
			snapshots, _ := db.GetAllSnapshotsByInstanceID(ctx, u.pool, inst.ID)
			for _, snap := range snapshots {
				if snap.ProviderImageID != nil {
					_ = db.UpdateSnapshotStatus(ctx, u.pool, snap.ID, models.SnapshotStatusDeleting)
					if err := prov.DeleteSnapshot(ctx, *snap.ProviderImageID); err != nil {
						u.logger.Error("upgrader: delete snapshot", "snapshot_id", snap.ID, "error", err)
					}
					_ = db.UpdateSnapshotStatus(ctx, u.pool, snap.ID, models.SnapshotStatusDeleted)
				}
			}

			// Delete server
			if err := prov.DeleteServer(ctx, *inst.ProviderServerID); err != nil {
				u.logger.Error("upgrader: delete server", "instance_id", inst.ID, "error", err)
			}
		}
	}

	// Step 2: Delete DNS records
	if u.dnsClient != nil && inst.DNSRecordID != nil {
		if err := u.dnsClient.DeleteRecord(ctx, *inst.DNSRecordID); err != nil {
			u.logger.Error("upgrader: delete DNS record", "instance_id", inst.ID, "error", err)
		}
		_ = db.UpdateInstanceDomain(ctx, u.pool, inst.ID, "", "")
	}
	if u.dnsClient != nil && inst.PreviewDNSRecordID != nil {
		if err := u.dnsClient.DeleteRecord(ctx, *inst.PreviewDNSRecordID); err != nil {
			u.logger.Error("upgrader: delete preview DNS record", "instance_id", inst.ID, "error", err)
		}
		_ = db.UpdateInstancePreviewDNS(ctx, u.pool, inst.ID, "", "")
	}

	// Step 3: Reset instance for fresh provisioning (plan tier already updated by webhook)
	_ = db.ClearInstanceProviderInfo(ctx, u.pool, inst.ID)
	_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusRequested)

	// Step 5: Create new provisioning job
	step := models.StepSelectProvider
	job := &models.ProvisioningJob{
		ID:             uuid.New(),
		VpsInstanceID:  inst.ID,
		IdempotencyKey: fmt.Sprintf("downgrade-%s-%d", inst.ID, time.Now().Unix()),
		Status:         models.JobPending,
		Step:           &step,
		MaxAttempts:    5,
	}
	if err := db.CreateProvisioningJob(ctx, u.pool, job); err != nil {
		u.logger.Error("upgrader: create provisioning job", "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	u.logger.Info("upgrader: downgrade initiated, fresh provisioning job created",
		"instance_id", inst.ID,
		"new_tier", newTier,
		"job_id", job.ID,
	)
}

func (u *Upgrader) waitForServer(ctx context.Context, prov provider.InfraProvider, serverID string, inst *models.VpsInstance) error {
	ticker := time.NewTicker(serverPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server ready: %w", ctx.Err())
		case <-ticker.C:
			server, err := prov.GetServer(ctx, serverID)
			if err != nil {
				u.logger.Warn("upgrader: poll server status failed", "error", err)
				continue
			}
			if server.Status == "running" {
				if server.IPv4 != "" {
					_ = db.UpdateInstanceProviderInfo(ctx, u.pool, inst.ID, server.ProviderServerID, &server.IPv4)
				}
				return nil
			}
			u.logger.Debug("upgrader: server not ready yet",
				"server_id", serverID,
				"status", server.Status,
			)
		}
	}
}
