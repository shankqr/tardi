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
	upgradeTimeout        = 25 * time.Minute
	upgradeCleanupTimeout = 2 * time.Minute
	downgradeTimeout      = 10 * time.Minute
)

// Upgrader handles plan upgrades (snapshot-based server migration) and downgrades (fresh provisioning).
type Upgrader struct {
	pool               *pgxpool.Pool
	registry           *provider.Registry
	logger             *slog.Logger
	apiURL             string
	openClawImageTag   string
	dnsClient          *dns.Client
	backendEgressCIDRs string
	sshPublicKey       string
}

func NewUpgrader(pool *pgxpool.Pool, registry *provider.Registry, logger *slog.Logger, apiURL, openClawImageTag, backendEgressCIDRs, sshPublicKey string, dnsClient *dns.Client) *Upgrader {
	return &Upgrader{
		pool:               pool,
		registry:           registry,
		logger:             logger,
		apiURL:             apiURL,
		openClawImageTag:   openClawImageTag,
		dnsClient:          dnsClient,
		backendEgressCIDRs: backendEgressCIDRs,
		sshPublicKey:       sshPublicKey,
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

func serverAlreadyOnMapping(server *provider.Server, mapping *models.ProviderPlanMapping) bool {
	return server != nil &&
		mapping != nil &&
		server.ServerType != "" &&
		server.ServerType == mapping.ProviderServerType
}

func stringPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (u *Upgrader) executeUpgrade(inst *models.VpsInstance, newTier models.PlanTier) {
	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)
	defer cancel()

	u.logger.Info("upgrader: starting plan upgrade",
		"instance_id", inst.ID,
		"new_tier", newTier,
	)

	if err := db.UpdateInstanceStatusConditional(ctx, u.pool, inst.ID, models.VpsStatusActive, models.VpsStatusUpgrading); err != nil {
		u.logger.Info("upgrader: skipping upgrade because instance is not active",
			"instance_id", inst.ID,
			"status", inst.Status,
			"error", err,
		)
		return
	}
	restoreActive := func() {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), upgradeCleanupTimeout)
		defer restoreCancel()
		_ = db.UpdateInstanceStatus(restoreCtx, u.pool, inst.ID, models.VpsStatusActive)
	}

	// Get provider
	prov, err := u.registry.Get(inst.Provider)
	if err != nil {
		u.logger.Error("upgrader: provider not found", "provider", inst.Provider, "error", err)
		restoreActive()
		return
	}

	if inst.ProviderServerID == nil {
		u.logger.Error("upgrader: instance has no provider server ID", "instance_id", inst.ID)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}

	oldServerID := *inst.ProviderServerID
	oldIPv4 := inst.IPv4

	// Step 1: Look up the target tier's provider mapping and skip duplicate
	// upgrades where the provider server is already on the requested plan.
	mapping, err := db.GetBestProviderMapping(ctx, u.pool, newTier, inst.Region)
	if err != nil {
		u.logger.Error("upgrader: get provider mapping", "tier", newTier, "error", err)
		restoreActive()
		return
	}
	currentServer, err := prov.GetServer(ctx, oldServerID)
	if err != nil {
		u.logger.Error("upgrader: get current server", "instance_id", inst.ID, "server_id", oldServerID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusError)
		return
	}
	if serverAlreadyOnMapping(currentServer, mapping) {
		if currentServer.IPv4 != "" {
			_ = db.UpdateInstanceProviderInfo(ctx, u.pool, inst.ID, currentServer.ProviderServerID, &currentServer.IPv4)
		}
		restoreActive()
		u.logger.Info("upgrader: server already on requested tier, skipping replacement",
			"instance_id", inst.ID,
			"new_tier", newTier,
			"server_id", currentServer.ProviderServerID,
			"server_type", currentServer.ServerType,
		)
		return
	}

	agentToken := ""
	if inst.AgentTokenSecretName != nil {
		agentToken = *inst.AgentTokenSecretName
	}
	frameworkAuthToken := ""
	if inst.OpenClawAuthToken != nil {
		frameworkAuthToken = *inst.OpenClawAuthToken
	}
	if agentToken == "" || frameworkAuthToken == "" {
		u.logger.Error("upgrader: refusing snapshot upgrade with missing existing tokens",
			"instance_id", inst.ID,
			"has_agent_token", agentToken != "",
			"has_framework_auth_token", frameworkAuthToken != "",
		)
		restoreActive()
		return
	}

	// Step 2: Create a system snapshot of the current server.
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
		restoreActive()
		return
	}

	snapCtx, snapCancel := context.WithTimeout(ctx, snapshotTimeout)
	result, err := prov.CreateSnapshot(snapCtx, oldServerID, snapName)
	snapCancel()
	if err != nil {
		u.logger.Error("upgrader: create snapshot failed", "instance_id", inst.ID, "error", err)
		_ = db.UpdateSnapshotError(ctx, u.pool, snap.ID, err.Error())
		restoreActive()
		return
	}
	cleanupSnapshot := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), upgradeCleanupTimeout)
		defer cleanupCancel()
		_ = db.UpdateSnapshotStatus(cleanupCtx, u.pool, snap.ID, models.SnapshotStatusDeleting)
		if err := prov.DeleteSnapshot(cleanupCtx, result.ProviderImageID); err != nil {
			u.logger.Error("upgrader: delete upgrade snapshot", "snapshot_id", snap.ID, "error", err)
			return
		}
		_ = db.UpdateSnapshotStatus(cleanupCtx, u.pool, snap.ID, models.SnapshotStatusDeleted)
	}

	if err := db.UpdateSnapshotReady(ctx, u.pool, snap.ID, result.ProviderImageID, result.SizeGB); err != nil {
		u.logger.Error("upgrader: update snapshot ready", "snapshot_id", snap.ID, "error", err)
		cleanupSnapshot()
		restoreActive()
		return
	}

	u.logger.Info("upgrader: snapshot created", "instance_id", inst.ID, "snapshot_id", snap.ID)

	// Step 3: Render cloud-init with the existing tokens. Snapshot-based
	// servers usually keep the source server's cloud-init state and local env
	// files, so rotating tokens here would break the replacement heartbeat.
	rootPassword, err := GenerateAgentToken()
	if err != nil {
		u.logger.Error("upgrader: generate root password", "error", err)
		cleanupSnapshot()
		restoreActive()
		return
	}

	// Extract config values
	agentCfg, err := db.GetAgentConfigByInstanceID(ctx, u.pool, inst.ID)
	if err != nil {
		u.logger.Error("upgrader: get agent config", "error", err)
		cleanupSnapshot()
		restoreActive()
		return
	}
	providerName := fallbackDefaultProvider
	modelID := fallbackDefaultModelID
	var openRouterAPIKey, anthropicAPIKey, openAIAPIKey, telegramBotToken, telegramAllowedUsers string
	var configVersion int
	if agentCfg != nil {
		configVersion = agentCfg.Version
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
	}

	var domain string
	if inst.Domain != nil && *inst.Domain != "" {
		domain = *inst.Domain
	}

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
			APIURL:               u.apiURL,
			InstanceID:           inst.ID.String(),
			APIServerKey:         frameworkAuthToken,
			HermesImageTag:       resolveHermesVersion(ctx, u.pool),
			Provider:             providerName,
			Model:                modelID,
			OpenRouterAPIKey:     openRouterAPIKey,
			AnthropicAPIKey:      anthropicAPIKey,
			OpenAIAPIKey:         openAIAPIKey,
			TelegramBotToken:     telegramBotToken,
			TelegramAllowedUsers: telegramAllowedUsers,
			ConfigVersion:        configVersion,
			RootPassword:         rootPassword,
			SSHPublicKey:         u.sshPublicKey,
			Domain:               domain,
			BackendEgressCIDRs:   u.backendEgressCIDRs,
		}
		userData, err = RenderHermesCloudInit(hermesData)
	default:
		ciData := CloudInitData{
			AgentToken:         agentToken,
			APIURL:             u.apiURL,
			InstanceID:         inst.ID.String(),
			OpenClawAuthToken:  frameworkAuthToken,
			OpenClawImageTag:   resolveImageTag(ctx, u.pool, u.openClawImageTag),
			RootPassword:       rootPassword,
			SSHPublicKey:       u.sshPublicKey,
			Domain:             domain,
			BackendEgressCIDRs: u.backendEgressCIDRs,
			Provider:           providerName,
			Model:              modelID,
			OpenRouterAPIKey:   openRouterAPIKey,
			AnthropicAPIKey:    anthropicAPIKey,
			OpenAIAPIKey:       openAIAPIKey,
			ConfigVersion:      configVersion,
		}
		if allModels, lErr := db.ListEnabledModels(ctx, u.pool); lErr == nil {
			for _, m := range allModels {
				ciData.AllModels = append(ciData.AllModels, CloudInitModel{ID: m.ID, Provider: m.Provider})
			}
		}
		userData, err = RenderCloudInit(ciData)
	}
	if err != nil {
		u.logger.Error("upgrader: render cloud-init", "error", err)
		cleanupSnapshot()
		restoreActive()
		return
	}

	// Get user for labels
	user, err := db.GetUserByID(ctx, u.pool, inst.UserID)
	if err != nil {
		u.logger.Error("upgrader: get user", "error", err)
		cleanupSnapshot()
		restoreActive()
		return
	}

	// Step 4: Create new server from snapshot on the new tier
	replacementName := buildUpgradeServerName(string(framework), user.Email, inst.ID.String()[:8], uuid.NewString()[:8])
	server, err := prov.CreateServer(ctx, provider.CreateServerRequest{
		Name:       replacementName,
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
		cleanupSnapshot()
		restoreActive()
		return
	}
	rollbackToOldServer := func(reason string, rollbackErr error) {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), upgradeCleanupTimeout)
		defer rollbackCancel()
		u.logger.Error("upgrader: rolling back to existing server",
			"instance_id", inst.ID,
			"reason", reason,
			"error", rollbackErr,
			"old_server_id", oldServerID,
			"replacement_server_id", server.ProviderServerID,
		)
		if err := db.UpdateInstanceProviderInfo(rollbackCtx, u.pool, inst.ID, oldServerID, oldIPv4); err != nil {
			u.logger.Error("upgrader: rollback provider info failed", "instance_id", inst.ID, "error", err)
			_ = db.UpdateInstanceStatus(rollbackCtx, u.pool, inst.ID, models.VpsStatusError)
			return
		}
		restoreActive()
		if err := prov.DeleteServer(rollbackCtx, server.ProviderServerID); err != nil {
			u.logger.Error("upgrader: delete replacement after rollback", "server_id", server.ProviderServerID, "error", err)
		}
		cleanupSnapshot()
	}

	// Update instance with new server info
	if err := db.UpdateInstanceProviderInfo(ctx, u.pool, inst.ID, server.ProviderServerID, stringPtrIfNotEmpty(server.IPv4)); err != nil {
		u.logger.Error("upgrader: update provider info", "error", err)
		deleteCtx, deleteCancel := context.WithTimeout(context.Background(), upgradeCleanupTimeout)
		if delErr := prov.DeleteServer(deleteCtx, server.ProviderServerID); delErr != nil {
			u.logger.Error("upgrader: delete replacement after DB update failure", "server_id", server.ProviderServerID, "error", delErr)
		}
		deleteCancel()
		cleanupSnapshot()
		restoreActive()
		return
	}

	// Step 5: Wait for new server to be running
	if err := u.waitForServer(ctx, prov, server.ProviderServerID, inst); err != nil {
		rollbackToOldServer("wait for replacement server", err)
		return
	}
	if readyServer, err := prov.GetServer(ctx, server.ProviderServerID); err == nil {
		if readyServer.IPv4 != "" {
			server.IPv4 = readyServer.IPv4
			_ = db.UpdateInstanceProviderInfo(ctx, u.pool, inst.ID, server.ProviderServerID, &server.IPv4)
		}
	} else {
		u.logger.Warn("upgrader: get replacement server after wait failed", "server_id", server.ProviderServerID, "error", err)
	}
	if server.IPv4 == "" {
		rollbackToOldServer("replacement server missing IPv4", fmt.Errorf("replacement server %s has no IPv4", server.ProviderServerID))
		return
	}

	// Step 5b: Reset root password via provider API.
	// Cloud-init doesn't re-run on snapshot-based servers (it detects prior execution),
	// so the password set via chpasswd in cloud-init is never applied. We must reset
	// the password through the provider API to get a working credential.
	newPassword, err := prov.ResetPassword(ctx, server.ProviderServerID)
	if err != nil {
		rollbackToOldServer("reset replacement password", err)
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
	cleanupSnapshot()

	// Step 9: Mark instance active (plan tier already updated by webhook)
	_ = db.UpdateInstanceStatus(ctx, u.pool, inst.ID, models.VpsStatusActive)
	_ = db.UpdateInstanceOpenClawVersion(ctx, u.pool, inst.ID, resolveFrameworkVersion(ctx, u.pool, framework, u.openClawImageTag), nil, nil)

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
