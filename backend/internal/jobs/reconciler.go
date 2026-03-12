package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

type Reconciler struct {
	pool     *pgxpool.Pool
	registry *provider.Registry
	logger   *slog.Logger
	interval time.Duration
}

func NewReconciler(pool *pgxpool.Pool, registry *provider.Registry, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		pool:     pool,
		registry: registry,
		logger:   logger,
		interval: 10 * time.Minute,
	}
}

// Start runs the reconciliation loop. Blocks until ctx is canceled.
func (r *Reconciler) Start(ctx context.Context) {
	r.logger.Info("reconciler started", "interval", r.interval)

	// Run once immediately on startup to fix any stuck instances
	r.reconcile(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("reconciler stopped")
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

func (r *Reconciler) reconcile(ctx context.Context) {
	r.reconcileActive(ctx)
	r.reconcileStaleRestarting(ctx)
	r.reconcileStaleProvisioning(ctx)
	r.reconcileStaleResuming(ctx)
	r.reconcileStaleSnapshotting(ctx)
	r.reconcileStaleRestoring(ctx)
}

// reconcileActive checks active instances against provider state.
func (r *Reconciler) reconcileActive(ctx context.Context) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, provider, provider_server_id
		FROM vps_instances
		WHERE status = 'active' AND provider_server_id IS NOT NULL
	`)
	if err != nil {
		r.logger.Error("reconciler: query active instances", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var idStr, providerName, serverID string
		if err := rows.Scan(&idStr, &providerName, &serverID); err != nil {
			r.logger.Error("reconciler: scan row", "error", err)
			continue
		}

		instanceID, err := uuid.Parse(idStr)
		if err != nil {
			r.logger.Error("reconciler: parse instance id", "id", idStr, "error", err)
			continue
		}

		prov, err := r.registry.Get(providerName)
		if err != nil {
			r.logger.Warn("reconciler: provider not registered", "provider", providerName)
			continue
		}

		server, err := prov.GetServer(ctx, serverID)
		if err != nil {
			if errors.Is(err, provider.ErrServerNotFound) {
				// Server confirmed deleted at provider — terminate the instance
				r.logger.Warn("reconciler: server deleted externally, terminating instance",
					"instance_id", idStr,
					"server_id", serverID,
				)
				_ = db.ClearInstanceProviderInfo(ctx, r.pool, instanceID)
				_ = db.UpdateInstanceStatus(ctx, r.pool, instanceID, models.VpsStatusTerminated)
			} else {
				// Transient error — log but don't mark as error immediately
				r.logger.Warn("reconciler: transient error getting server from provider",
					"instance_id", idStr,
					"server_id", serverID,
					"error", err,
				)
			}
			continue
		}

		switch server.Status {
		case "running":
			// All good — update IP if changed
			if server.IPv4 != "" {
				_ = db.UpdateInstanceProviderInfo(ctx, r.pool, instanceID, server.ProviderServerID, &server.IPv4)
			}
		case "off":
			// Server was stopped externally — try to restart it
			r.logger.Warn("reconciler: server is off, attempting restart",
				"instance_id", idStr,
				"server_id", serverID,
			)
			if err := prov.StartServer(ctx, serverID); err != nil {
				r.logger.Error("reconciler: restart failed", "instance_id", idStr, "error", err)
				_ = db.UpdateInstanceStatus(ctx, r.pool, instanceID, models.VpsStatusError)
			}
		default:
			r.logger.Warn("reconciler: unexpected server status",
				"instance_id", idStr,
				"server_id", serverID,
				"status", server.Status,
			)
		}
	}

	if err := rows.Err(); err != nil {
		r.logger.Error("reconciler: rows error", "error", err)
	}
}

// reconcileStaleProvisioning fixes instances stuck in provisioning states
// (bootstrapping, installing_agent) by checking the actual provider server status.
func (r *Reconciler) reconcileStaleProvisioning(ctx context.Context) {
	staleStatuses := []models.VpsStatus{
		models.VpsStatusBootstrapping,
		models.VpsStatusInstallingAgent,
	}

	for _, status := range staleStatuses {
		instances, err := db.GetActiveInstancesByStatus(ctx, r.pool, status)
		if err != nil {
			r.logger.Error("reconciler: get provisioning instances", "status", status, "error", err)
			continue
		}

		for _, inst := range instances {
			// Only reconcile if stuck for > 5 minutes
			if time.Since(inst.UpdatedAt) < 5*time.Minute {
				continue
			}

			if inst.ProviderServerID == nil {
				continue
			}

			prov, err := r.registry.Get(inst.Provider)
			if err != nil {
				continue
			}

			server, err := prov.GetServer(ctx, *inst.ProviderServerID)
			if err != nil {
				r.logger.Warn("reconciler: failed to get provisioning server",
					"instance_id", inst.ID, "error", err)
				continue
			}

			if server.Status == "running" {
				r.logger.Info("reconciler: stuck provisioning instance is actually running, activating",
					"instance_id", inst.ID, "was_status", status,
				)
				_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusActive)

				// Update IP if available
				if server.IPv4 != "" {
					_ = db.UpdateInstanceProviderInfo(ctx, r.pool, inst.ID, server.ProviderServerID, &server.IPv4)
				}

				// Kill any stuck provisioning jobs
				_, _ = r.pool.Exec(ctx, `
					UPDATE provisioning_jobs SET status = 'completed', updated_at = NOW()
					WHERE vps_instance_id = $1 AND status IN ('pending', 'running')
				`, inst.ID)
			}
		}
	}
}

// reconcileStaleRestarting fixes instances stuck in "restarting" for too long.
func (r *Reconciler) reconcileStaleRestarting(ctx context.Context) {
	instances, err := db.GetActiveInstancesByStatus(ctx, r.pool, models.VpsStatusRestarting)
	if err != nil {
		r.logger.Error("reconciler: get restarting instances", "error", err)
		return
	}

	for _, inst := range instances {
		// If stuck in restarting for > 10 minutes, check actual status
		if time.Since(inst.UpdatedAt) < 10*time.Minute {
			continue
		}

		if inst.ProviderServerID == nil {
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
			continue
		}

		prov, err := r.registry.Get(inst.Provider)
		if err != nil {
			continue
		}

		server, err := prov.GetServer(ctx, *inst.ProviderServerID)
		if err != nil {
			if errors.Is(err, provider.ErrServerNotFound) {
				r.logger.Warn("reconciler: restarting server deleted externally, terminating",
					"instance_id", inst.ID,
				)
				_ = db.ClearInstanceProviderInfo(ctx, r.pool, inst.ID)
				_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusTerminated)
			} else {
				_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
			}
			continue
		}

		if server.Status == "running" {
			r.logger.Info("reconciler: stale restarting instance is actually running, fixing",
				"instance_id", inst.ID,
			)
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusActive)
		} else {
			r.logger.Warn("reconciler: stale restarting instance still not running",
				"instance_id", inst.ID,
				"actual_status", server.Status,
			)
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		}
	}
}

// reconcileStaleSnapshotting fixes instances stuck in "snapshotting" for too long.
// This happens when the snapshot goroutine crashes or the context times out without cleanup.
func (r *Reconciler) reconcileStaleSnapshotting(ctx context.Context) {
	instances, err := db.GetActiveInstancesByStatus(ctx, r.pool, models.VpsStatusSnapshotting)
	if err != nil {
		r.logger.Error("reconciler: get snapshotting instances", "error", err)
		return
	}

	for _, inst := range instances {
		if time.Since(inst.UpdatedAt) < 10*time.Minute {
			continue
		}

		r.logger.Warn("reconciler: instance stuck in snapshotting, resetting to active",
			"instance_id", inst.ID,
			"stuck_since", inst.UpdatedAt,
		)

		// Mark any snapshots still in "creating" as error
		snapshots, err := db.GetSnapshotsByInstanceID(ctx, r.pool, inst.ID)
		if err == nil {
			for _, snap := range snapshots {
				if snap.Status == models.SnapshotStatusCreating {
					_ = db.UpdateSnapshotError(ctx, r.pool, snap.ID, "snapshot creation timed out")
				}
			}
		}

		_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusActive)
	}
}

// reconcileStaleRestoring fixes instances stuck in "restoring" for too long.
func (r *Reconciler) reconcileStaleRestoring(ctx context.Context) {
	instances, err := db.GetActiveInstancesByStatus(ctx, r.pool, models.VpsStatusRestoring)
	if err != nil {
		r.logger.Error("reconciler: get restoring instances", "error", err)
		return
	}

	for _, inst := range instances {
		if time.Since(inst.UpdatedAt) < 15*time.Minute {
			continue
		}

		if inst.ProviderServerID == nil {
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
			continue
		}

		prov, err := r.registry.Get(inst.Provider)
		if err != nil {
			continue
		}

		server, err := prov.GetServer(ctx, *inst.ProviderServerID)
		if err != nil {
			r.logger.Warn("reconciler: failed to get restoring server",
				"instance_id", inst.ID, "error", err)
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
			continue
		}

		if server.Status == "running" {
			r.logger.Info("reconciler: stale restoring instance is actually running, activating",
				"instance_id", inst.ID,
			)
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusActive)
		} else {
			r.logger.Warn("reconciler: stale restoring instance still not running",
				"instance_id", inst.ID,
				"actual_status", server.Status,
			)
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		}
	}
}

// reconcileStaleResuming fixes instances stuck in "resuming" for too long.
func (r *Reconciler) reconcileStaleResuming(ctx context.Context) {
	instances, err := db.GetActiveInstancesByStatus(ctx, r.pool, models.VpsStatusResuming)
	if err != nil {
		r.logger.Error("reconciler: get resuming instances", "error", err)
		return
	}

	for _, inst := range instances {
		// If stuck in resuming for > 20 minutes, check actual status
		if time.Since(inst.UpdatedAt) < 20*time.Minute {
			continue
		}

		if inst.ProviderServerID == nil {
			r.logger.Warn("reconciler: stale resuming instance has no server ID, marking error",
				"instance_id", inst.ID,
			)
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
			continue
		}

		prov, err := r.registry.Get(inst.Provider)
		if err != nil {
			continue
		}

		server, err := prov.GetServer(ctx, *inst.ProviderServerID)
		if err != nil {
			if errors.Is(err, provider.ErrServerNotFound) {
				r.logger.Warn("reconciler: resuming server deleted externally, terminating",
					"instance_id", inst.ID,
				)
				_ = db.ClearInstanceProviderInfo(ctx, r.pool, inst.ID)
				_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusTerminated)
			} else {
				_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
			}
			continue
		}

		if server.Status == "running" {
			r.logger.Info("reconciler: stale resuming instance is actually running, activating",
				"instance_id", inst.ID,
			)
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusActive)
		} else {
			r.logger.Warn("reconciler: stale resuming instance still not running",
				"instance_id", inst.ID,
				"actual_status", server.Status,
			)
			_ = db.UpdateInstanceStatus(ctx, r.pool, inst.ID, models.VpsStatusError)
		}
	}
}
