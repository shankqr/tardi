package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/dns"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

type Worker struct {
	pool        *pgxpool.Pool
	registry    *provider.Registry
	logger      *slog.Logger
	interval    time.Duration
	provisioner *Provisioner
}

func NewWorker(pool *pgxpool.Pool, registry *provider.Registry, logger *slog.Logger, apiURL, openClawImageTag, backendEgressCIDRs, sshPublicKey string, sshPrivateKey []byte, dnsClient *dns.Client) *Worker {
	return &Worker{
		pool:     pool,
		registry: registry,
		logger:   logger,
		interval: 2 * time.Second,
		provisioner: &Provisioner{
			pool:               pool,
			registry:           registry,
			logger:             logger,
			apiURL:             apiURL,
			openClawImageTag:   openClawImageTag,
			dnsClient:          dnsClient,
			backendEgressCIDRs: backendEgressCIDRs,
			sshPublicKey:       sshPublicKey,
			sshPrivateKey:      sshPrivateKey,
		},
	}
}

// Start begins the job polling loop. Blocks until ctx is canceled.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("job worker started", "interval", w.interval)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("job worker stopped")
			return
		case <-ticker.C:
			// Don't claim new jobs if shutting down — select may pick
			// ticker.C over ctx.Done() randomly when both fire together.
			if ctx.Err() != nil {
				continue
			}
			w.poll(ctx)
		}
	}
}

// resolveImageTag reads the pinned OpenClaw version from the database.
// Falls back to the env-var-based tag if the DB has no pinned version or still says "latest".
func resolveImageTag(ctx context.Context, pool *pgxpool.Pool, fallback string) string {
	v, err := db.GetGlobalTargetVersion(ctx, pool)
	if err != nil || v == "" || v == "latest" {
		return fallback
	}
	return v
}

// resolveHermesVersion reads the target Hermes Docker image tag from the database.
// "latest" is intentional: Hermes VPSes track the official Docker image and
// heartbeat only recreates the container when the pulled image digest changes.
func resolveHermesVersion(ctx context.Context, pool *pgxpool.Pool) string {
	v, err := db.GetGlobalHermesVersion(ctx, pool)
	if err != nil || v == "" {
		return "latest"
	}
	return v
}

func resolveFrameworkVersion(ctx context.Context, pool *pgxpool.Pool, framework models.AgentFramework, openClawImageTag string) string {
	if framework == models.FrameworkHermes {
		return resolveHermesVersion(ctx, pool)
	}
	return resolveImageTag(ctx, pool, openClawImageTag)
}

func (w *Worker) poll(ctx context.Context) {
	job, err := db.ClaimNextJob(ctx, w.pool)
	if err != nil {
		w.logger.Error("worker: claim job", "error", err)
		return
	}
	if job == nil {
		return
	}

	w.logger.Info("worker: processing job",
		"job_id", job.ID,
		"instance_id", job.VpsInstanceID,
		"step", job.Step,
		"attempt", job.Attempts,
	)

	if err := w.provisioner.Execute(ctx, job); err != nil {
		w.logger.Error("worker: job failed",
			"job_id", job.ID,
			"error", err,
			"attempt", job.Attempts,
		)
	}
}
