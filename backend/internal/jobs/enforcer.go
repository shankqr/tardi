package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

const (
	// Grace period before suspending VPS after payment failure.
	suspendGracePeriod = 7 * 24 * time.Hour
	// Grace period before deleting suspension snapshot (making recovery impossible).
	snapshotRetentionPeriod = 30 * 24 * time.Hour
	// Timeout for snapshot creation during suspension.
	snapshotTimeout = 15 * time.Minute
)

// Enforcer periodically checks subscription status and enforces billing rules.
type Enforcer struct {
	pool     *pgxpool.Pool
	registry *provider.Registry
	logger   *slog.Logger
	interval time.Duration
}

func NewEnforcer(pool *pgxpool.Pool, registry *provider.Registry, logger *slog.Logger) *Enforcer {
	return &Enforcer{
		pool:     pool,
		registry: registry,
		logger:   logger,
		interval: 1 * time.Hour,
	}
}

// Start runs the enforcement loop. Blocks until ctx is canceled.
func (e *Enforcer) Start(ctx context.Context) {
	e.logger.Info("enforcer started", "interval", e.interval)

	// Run once immediately on startup
	e.enforce(ctx)

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("enforcer stopped")
			return
		case <-ticker.C:
			e.enforce(ctx)
		}
	}
}

func (e *Enforcer) enforce(ctx context.Context) {
	e.suspendPastDue(ctx)
	e.terminateSuspended(ctx)
	e.teardownCanceled(ctx)
}

// suspendPastDue snapshots and deletes VPS instances for subscriptions past_due > 7 days.
// The snapshot allows recovery if the user pays later. Deleting the server stops Hetzner charges.
func (e *Enforcer) suspendPastDue(ctx context.Context) {
	subs, err := db.GetPastDueSubscriptions(ctx, e.pool, suspendGracePeriod)
	if err != nil {
		e.logger.Error("enforcer: get past due subscriptions", "error", err)
		return
	}

	for _, sub := range subs {
		instances, err := db.GetInstancesBySubscriptionID(ctx, e.pool, sub.ID)
		if err != nil {
			e.logger.Error("enforcer: get instances for subscription", "sub_id", sub.ID, "error", err)
			continue
		}

		for _, inst := range instances {
			if inst.Status != models.VpsStatusActive {
				continue
			}

			e.logger.Info("enforcer: suspending instance (snapshot + delete)",
				"instance_id", inst.ID,
				"sub_id", sub.ID,
			)

			_ = db.UpdateInstanceStatus(ctx, e.pool, inst.ID, models.VpsStatusSuspending)

			if inst.ProviderServerID != nil {
				prov, err := e.registry.Get(inst.Provider)
				if err != nil {
					e.logger.Error("enforcer: provider not found", "provider", inst.Provider)
					_ = db.UpdateInstanceStatus(ctx, e.pool, inst.ID, models.VpsStatusSuspended)
					continue
				}

				// Take a system snapshot before deleting
				e.createSuspensionSnapshot(ctx, prov, &inst)

				// Delete the server to stop charges
				if err := prov.DeleteServer(ctx, *inst.ProviderServerID); err != nil {
					e.logger.Error("enforcer: delete server failed", "instance_id", inst.ID, "error", err)
				}

				// Clear provider info since server is gone
				if err := db.ClearInstanceProviderInfo(ctx, e.pool, inst.ID); err != nil {
					e.logger.Error("enforcer: clear provider info failed", "instance_id", inst.ID, "error", err)
				}
			}

			_ = db.UpdateInstanceStatus(ctx, e.pool, inst.ID, models.VpsStatusSuspended)
		}

		// Update subscription status to suspended
		_ = db.UpdateSubscriptionStatus(ctx, e.pool, sub.StripeSubscriptionID, models.SubStatusSuspended, sub.CurrentPeriodEnd, false)
	}
}

// createSuspensionSnapshot takes a system snapshot of the instance for later recovery.
func (e *Enforcer) createSuspensionSnapshot(ctx context.Context, prov provider.InfraProvider, inst *models.VpsInstance) {
	snapName := fmt.Sprintf("suspension-%d", time.Now().Unix())

	snap := &models.Snapshot{
		ID:            uuid.New(),
		VpsInstanceID: inst.ID,
		Name:          snapName,
		Status:        models.SnapshotStatusCreating,
		IsSystem:      true,
	}
	if err := db.CreateSnapshot(ctx, e.pool, snap); err != nil {
		e.logger.Error("enforcer: create suspension snapshot record", "instance_id", inst.ID, "error", err)
		return
	}

	snapCtx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	result, err := prov.CreateSnapshot(snapCtx, *inst.ProviderServerID, snapName)
	if err != nil {
		e.logger.Error("enforcer: create suspension snapshot failed",
			"instance_id", inst.ID, "error", err)
		_ = db.UpdateSnapshotError(ctx, e.pool, snap.ID, err.Error())
		return
	}

	if err := db.UpdateSnapshotReady(ctx, e.pool, snap.ID, result.ProviderImageID, result.SizeGB); err != nil {
		e.logger.Error("enforcer: update suspension snapshot ready", "snapshot_id", snap.ID, "error", err)
		return
	}

	e.logger.Info("enforcer: suspension snapshot created",
		"instance_id", inst.ID,
		"snapshot_id", snap.ID,
		"provider_image_id", result.ProviderImageID,
	)
}

// terminateSuspended deletes suspension snapshots for subscriptions suspended > 30 days.
// The server is already deleted during suspension; this just removes the snapshot to stop
// snapshot storage charges and make recovery impossible.
func (e *Enforcer) terminateSuspended(ctx context.Context) {
	subs, err := db.GetSuspendedSubscriptions(ctx, e.pool, snapshotRetentionPeriod)
	if err != nil {
		e.logger.Error("enforcer: get suspended subscriptions", "error", err)
		return
	}

	for _, sub := range subs {
		instances, err := db.GetInstancesBySubscriptionID(ctx, e.pool, sub.ID)
		if err != nil {
			e.logger.Error("enforcer: get instances for subscription", "sub_id", sub.ID, "error", err)
			continue
		}

		for _, inst := range instances {
			e.logger.Info("enforcer: terminating instance after suspension period",
				"instance_id", inst.ID,
				"sub_id", sub.ID,
			)

			// Delete all snapshots (including the system suspension snapshot)
			e.deleteAllSnapshots(ctx, &inst)

			_ = db.UpdateInstanceStatus(ctx, e.pool, inst.ID, models.VpsStatusTerminated)
		}
	}
}

// teardownCanceled deletes all resources (snapshots + VPS) for canceled subscriptions.
func (e *Enforcer) teardownCanceled(ctx context.Context) {
	subs, err := db.GetCanceledSubscriptions(ctx, e.pool)
	if err != nil {
		e.logger.Error("enforcer: get canceled subscriptions", "error", err)
		return
	}

	for _, sub := range subs {
		instances, err := db.GetInstancesBySubscriptionID(ctx, e.pool, sub.ID)
		if err != nil {
			e.logger.Error("enforcer: get instances for canceled sub", "sub_id", sub.ID, "error", err)
			continue
		}

		for _, inst := range instances {
			e.logger.Info("enforcer: tearing down instance after subscription canceled",
				"instance_id", inst.ID,
				"sub_id", sub.ID,
			)

			e.cleanupInstanceResources(ctx, &inst)
		}
	}
}

// deleteAllSnapshots removes all snapshots (user + system) from the provider for an instance.
func (e *Enforcer) deleteAllSnapshots(ctx context.Context, inst *models.VpsInstance) {
	prov, err := e.registry.Get(inst.Provider)
	if err != nil {
		e.logger.Error("enforcer: provider not found for snapshot cleanup", "provider", inst.Provider)
		return
	}

	snapshots, err := db.GetAllSnapshotsByInstanceID(ctx, e.pool, inst.ID)
	if err != nil {
		e.logger.Error("enforcer: get snapshots for instance", "instance_id", inst.ID, "error", err)
		return
	}

	for _, snap := range snapshots {
		if snap.ProviderImageID != nil {
			e.logger.Info("enforcer: deleting snapshot",
				"snapshot_id", snap.ID, "provider_image_id", *snap.ProviderImageID)
			_ = db.UpdateSnapshotStatus(ctx, e.pool, snap.ID, models.SnapshotStatusDeleting)
			if err := prov.DeleteSnapshot(ctx, *snap.ProviderImageID); err != nil {
				e.logger.Error("enforcer: delete snapshot failed",
					"snapshot_id", snap.ID, "error", err)
			}
		}
		_ = db.UpdateSnapshotStatus(ctx, e.pool, snap.ID, models.SnapshotStatusDeleted)
	}
}

// cleanupInstanceResources deletes all snapshots from the provider, then deletes the VPS.
func (e *Enforcer) cleanupInstanceResources(ctx context.Context, inst *models.VpsInstance) {
	e.deleteAllSnapshots(ctx, inst)

	// Delete VPS from provider (may already be gone if instance was suspended)
	_ = db.UpdateInstanceStatus(ctx, e.pool, inst.ID, models.VpsStatusTerminating)

	if inst.ProviderServerID != nil {
		prov, err := e.registry.Get(inst.Provider)
		if err != nil {
			e.logger.Error("enforcer: provider not found", "provider", inst.Provider)
		} else if err := prov.DeleteServer(ctx, *inst.ProviderServerID); err != nil {
			e.logger.Error("enforcer: delete server failed", "instance_id", inst.ID, "error", err)
		}
	}

	_ = db.UpdateInstanceStatus(ctx, e.pool, inst.ID, models.VpsStatusTerminated)
}
