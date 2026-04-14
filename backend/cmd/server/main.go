package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/shanq/tardi/internal/api"
	"github.com/shanq/tardi/internal/auth"
	"github.com/shanq/tardi/internal/billing"
	"github.com/shanq/tardi/internal/config"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/dns"
	"github.com/shanq/tardi/internal/jobs"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
	"github.com/shanq/tardi/internal/provider/hetzner"
	"github.com/shanq/tardi/internal/scripts"
	"github.com/shanq/tardi/migrations"
)

func main() {
	cfg := config.Load()

	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	var logHandler slog.Handler
	if cfg.IsDev() {
		logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	} else {
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	}
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	// Eagerly load the dashboard-shim binary so we surface the success/failure
	// log line at startup rather than at the first /api/agent/dashboard-shim hit.
	scripts.LoadDashboardShim()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to database
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	logger.Info("connected to database")

	// Run migrations
	if err := db.Migrate(cfg.DatabaseURL, migrations.FS); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations complete")

	// Initialize Firebase Auth
	if err := auth.InitFirebase(cfg.FirebaseProjectID, cfg.IsDev()); err != nil {
		logger.Error("failed to initialize firebase", "error", err)
		os.Exit(1)
	}

	// Seed dev data
	if cfg.IsDev() {
		seedDevData(ctx, pool, logger)
	}

	// Initialize Stripe billing
	stripeSvc := billing.NewStripeService(cfg.StripeSecretKey, cfg.StripeWebhookSecret, logger)

	// Provider registry
	registry := provider.NewRegistry()
	if cfg.HetznerAPIToken != "" {
		registry.Register("hetzner", hetzner.New(cfg.HetznerAPIToken, logger))
		logger.Info("registered hetzner provider")
	} else {
		mockCfg := provider.MockConfig{
			InitDelay:      cfg.MockInitDelay,
			HeartbeatDelay: cfg.MockHeartbeatDelay,
			StopDelay:      cfg.MockStopDelay,
			StartDelay:     cfg.MockStartDelay,
			RestartDelay:   cfg.MockRestartDelay,
		}
		registry.Register("hetzner", provider.NewMockProvider(pool, logger, mockCfg))
		logger.Info("registered mock provider (no HETZNER_API_TOKEN)",
			"init_delay", mockCfg.InitDelay,
			"heartbeat_delay", mockCfg.HeartbeatDelay,
		)
	}

	// Cloudflare DNS client (nil if not configured — instances will use self-signed TLS)
	dnsClient := dns.NewClient(cfg.CloudflareAPIToken, cfg.CloudflareZoneID, cfg.CloudflareBaseDomain)
	if dnsClient != nil {
		logger.Info("cloudflare DNS configured", "base_domain", cfg.CloudflareBaseDomain)
	}

	// WaitGroup for background workers (provisioner, reconciler, etc.)
	// Shutdown must wait for these to drain before closing the DB pool.
	var workerWg sync.WaitGroup

	// Start background workers
	worker := jobs.NewWorker(pool, registry, logger, cfg.APIURL, cfg.OpenClawImageTag, cfg.BackendEgressCIDRs, cfg.SSHPublicKey, dnsClient)
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		worker.Start(ctx)
	}()

	reconciler := jobs.NewReconciler(pool, registry, logger)
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		reconciler.Start(ctx)
	}()

	enforcer := jobs.NewEnforcer(pool, registry, logger)
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		enforcer.Start(ctx)
	}()

	scriptPusher := jobs.NewScriptPusher(pool, logger, cfg.SSHPrivateKey, cfg.SSHPublicKey)
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		scriptPusher.Start(ctx)
	}()

	tokenRefresher := jobs.NewTokenRefresher(pool, logger, cfg)
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		tokenRefresher.Start(ctx)
	}()

	resumer := jobs.NewResumer(pool, registry, logger, cfg.APIURL, cfg.OpenClawImageTag, cfg.BackendEgressCIDRs, cfg.SSHPublicKey)
	upgrader := jobs.NewUpgrader(pool, registry, logger, cfg.APIURL, cfg.OpenClawImageTag, cfg.BackendEgressCIDRs, cfg.SSHPublicKey, dnsClient)

	// WaitGroup for background goroutines (snapshot create/restore/delete, restart, etc.)
	var bgTasks sync.WaitGroup

	// Build router with all endpoints
	deps := api.Dependencies{
		Pool:      pool,
		Logger:    logger,
		Config:    cfg,
		Billing:   stripeSvc,
		Registry:  registry,
		Resumer:   resumer,
		Upgrader:  upgrader,
		DNSClient: dnsClient,
		BGTasks:   &bgTasks,
	}
	handler := api.NewRouter(deps)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown — wait for all workers and background tasks before
	// main() returns and defer pool.Close() fires. Without this, the DB pool
	// can close while the provisioner is still trying to schedule a retry,
	// causing "closed pool" errors and orphaned jobs.
	shutdownDone := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down...")
		cancel()

		// Stop accepting new requests
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)

		// Wait for background workers (provisioner, reconciler, enforcer, etc.)
		// to finish their in-flight operations — especially retry scheduling.
		logger.Info("waiting for workers to drain...")
		workerWg.Wait()
		logger.Info("workers drained")

		// Wait for in-flight background goroutines (snapshot/restore/delete/restart)
		// with a hard deadline so we don't hang forever
		done := make(chan struct{})
		go func() {
			bgTasks.Wait()
			close(done)
		}()
		select {
		case <-done:
			logger.Info("all background tasks completed")
		case <-time.After(14 * time.Minute):
			logger.Warn("shutdown deadline reached, some background tasks may not have completed")
		}

		close(shutdownDone)
	}()

	logger.Info("server starting", "port", cfg.Port, "environment", cfg.Environment)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}

	// Block until shutdown is fully complete — pool.Close() (deferred above)
	// must not run until all workers have finished their DB writes.
	<-shutdownDone
}

// seedDevData creates a mock user and subscription for local development.
func seedDevData(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	user, err := db.UpsertUser(ctx, pool, "mock-uid-12345", "demo@tardi.app", nil)
	if err != nil {
		logger.Warn("failed to seed dev user", "error", err)
		return
	}

	// Check if subscription already exists
	sub, err := db.GetSubscriptionByUserID(ctx, pool, user.ID)
	if err != nil {
		logger.Warn("failed to check dev subscription", "error", err)
		return
	}
	if sub != nil {
		logger.Info("dev data already seeded")
		return
	}

	periodEnd := time.Now().AddDate(0, 1, 0)
	err = db.CreateSubscription(ctx, pool, &models.Subscription{
		ID:                   uuid.New(),
		UserID:               user.ID,
		StripeSubscriptionID: "sub_dev_mock",
		StripeCustomerID:     "cus_dev_mock",
		PlanTier:             models.PlanStandard,
		Status:               models.SubStatusActive,
		CurrentPeriodEnd:     &periodEnd,
	})
	if err != nil {
		logger.Warn("failed to seed dev subscription", "error", err)
		return
	}
	logger.Info("dev data seeded", "user_id", user.ID)
}

