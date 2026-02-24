package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
		var id, providerName, serverID string
		if err := rows.Scan(&id, &providerName, &serverID); err != nil {
			r.logger.Error("reconciler: scan row", "error", err)
			continue
		}

		prov, err := r.registry.Get(providerName)
		if err != nil {
			r.logger.Warn("reconciler: provider not registered", "provider", providerName)
			continue
		}

		server, err := prov.GetServer(ctx, serverID)
		if err != nil {
			r.logger.Warn("reconciler: failed to get server from provider",
				"instance_id", id,
				"server_id", serverID,
				"error", err,
			)
			continue
		}

		if server.Status != "running" {
			r.logger.Warn("reconciler: drift detected",
				"instance_id", id,
				"server_id", serverID,
				"expected_status", "running",
				"actual_status", server.Status,
			)
		}
	}

	if err := rows.Err(); err != nil {
		r.logger.Error("reconciler: rows error", "error", err)
	}
}
