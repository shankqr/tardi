package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

const (
	// Grace period before suspending VPS after payment failure.
	suspendGracePeriod = 7 * 24 * time.Hour
	// Grace period before terminating suspended VPS.
	terminateGracePeriod = 30 * 24 * time.Hour
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
}

// suspendPastDue stops VPS instances for subscriptions that have been past_due > 7 days.
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

			e.logger.Info("enforcer: suspending instance due to past_due subscription",
				"instance_id", inst.ID,
				"sub_id", sub.ID,
			)

			_ = db.UpdateInstanceStatus(ctx, e.pool, inst.ID, models.VpsStatusSuspending)

			// Stop the server on the provider
			if inst.ProviderServerID != nil {
				prov, err := e.registry.Get(inst.Provider)
				if err != nil {
					e.logger.Error("enforcer: provider not found", "provider", inst.Provider)
					continue
				}
				if err := prov.StopServer(ctx, *inst.ProviderServerID); err != nil {
					e.logger.Error("enforcer: stop server failed", "instance_id", inst.ID, "error", err)
				}
			}

			_ = db.UpdateInstanceStatus(ctx, e.pool, inst.ID, models.VpsStatusSuspended)
		}

		// Update subscription status to suspended
		_ = db.UpdateSubscriptionStatus(ctx, e.pool, sub.StripeSubscriptionID, models.SubStatusSuspended, sub.CurrentPeriodEnd)
	}
}

// terminateSuspended destroys VPS instances for subscriptions suspended > 30 days.
func (e *Enforcer) terminateSuspended(ctx context.Context) {
	subs, err := db.GetSuspendedSubscriptions(ctx, e.pool, terminateGracePeriod)
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
	}
}
