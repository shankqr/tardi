package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	"github.com/shanq/tardi/internal/jobs"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
	"github.com/shanq/tardi/internal/provider/hetzner"
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

	// Start background workers
	worker := jobs.NewWorker(pool, registry, logger, cfg.APIURL, cfg.OpenClawImageTag)
	go worker.Start(ctx)

	reconciler := jobs.NewReconciler(pool, registry, logger)
	go reconciler.Start(ctx)

	enforcer := jobs.NewEnforcer(pool, registry, logger)
	go enforcer.Start(ctx)

	resumer := jobs.NewResumer(pool, registry, logger, cfg.APIURL, cfg.OpenClawImageTag)

	// Build router with all endpoints
	deps := api.Dependencies{
		Pool:     pool,
		Logger:   logger,
		Config:   cfg,
		Billing:  stripeSvc,
		Registry: registry,
		Resumer:  resumer,
	}
	handler := api.NewRouter(deps)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down...")
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	logger.Info("server starting", "port", cfg.Port, "environment", cfg.Environment)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
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

