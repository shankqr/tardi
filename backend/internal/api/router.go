package api

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/billing"
	"github.com/shanq/tardi/internal/config"
)

type Dependencies struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Config  *config.Config
	Billing *billing.StripeService
}

func NewRouter(deps Dependencies) http.Handler {
	mux := http.NewServeMux()

	// Health checks (no auth)
	mux.HandleFunc("GET /healthz", HealthzHandler())
	mux.HandleFunc("GET /readyz", ReadyzHandler(deps.Pool))

	// Authed API routes
	authedMux := http.NewServeMux()
	authedMux.HandleFunc("GET /api/dashboard/state", DashboardHandler(deps))
	authedMux.HandleFunc("POST /api/instances", CreateInstanceHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/restart", RestartInstanceHandler(deps))
	authedMux.HandleFunc("DELETE /api/instances/{id}", DeleteInstanceHandler(deps))
	authedMux.HandleFunc("POST /api/billing/portal", BillingPortalHandler(deps))

	// Agent phone-home endpoints (agent token auth, handled inside handlers)
	mux.HandleFunc("GET /api/agent/config", AgentConfigHandler(deps))
	mux.HandleFunc("POST /api/agent/heartbeat", AgentHeartbeatHandler(deps))

	// Stripe webhook (signature verification, not JWT auth)
	mux.HandleFunc("POST /api/webhooks/stripe", StripeWebhookHandler(deps))

	// Mount authed routes with auth middleware
	authMw := middleware.Auth(deps.Pool, deps.Config.IsDev())
	mux.Handle("/api/", authMw(authedMux))

	// Apply global middleware
	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.Logger(deps.Logger),
		middleware.CORS(deps.Config.AllowedOrigins),
		middleware.RateLimit(60),
	)

	return handler
}
