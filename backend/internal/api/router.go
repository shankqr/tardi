package api

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/billing"
	"github.com/shanq/tardi/internal/config"
	"github.com/shanq/tardi/internal/jobs"
	"github.com/shanq/tardi/internal/provider"
)

type Dependencies struct {
	Pool     *pgxpool.Pool
	Logger   *slog.Logger
	Config   *config.Config
	Billing  *billing.StripeService
	Registry *provider.Registry
	Resumer  *jobs.Resumer
	BGTasks  *sync.WaitGroup // Tracks background goroutines (snapshots, restores) for graceful shutdown
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
	authedMux.HandleFunc("POST /api/instances/{id}/reset-password", ResetPasswordHandler(deps))
	authedMux.HandleFunc("PATCH /api/instances/{id}", RenameInstanceHandler(deps))
	authedMux.HandleFunc("DELETE /api/instances/{id}", DeleteInstanceHandler(deps))
	authedMux.HandleFunc("GET /api/instances/{id}/config", GetAgentConfigHandler(deps))
	authedMux.HandleFunc("PUT /api/instances/{id}/config", UpdateAgentConfigHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/snapshots", CreateSnapshotHandler(deps))
	authedMux.HandleFunc("POST /api/snapshots/{snapshot_id}/restore", RestoreSnapshotHandler(deps))
	authedMux.HandleFunc("DELETE /api/snapshots/{snapshot_id}", DeleteSnapshotHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/whatsapp/qr", WhatsAppQRHandler(deps))
	authedMux.HandleFunc("GET /api/instances/{id}/whatsapp/status", WhatsAppStatusHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/telegram/connect", TelegramConnectHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/telegram/disconnect", TelegramDisconnectHandler(deps))
	authedMux.HandleFunc("GET /api/instances/{id}/diagnostics", DiagnosticsHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/sync-config", SyncConfigHandler(deps))
	authedMux.HandleFunc("GET /api/instances/{id}/sync-status", SyncStatusHandler(deps))
	authedMux.HandleFunc("POST /api/billing/portal", BillingPortalHandler(deps))

	// Agent phone-home endpoints (agent token auth, handled inside handlers)
	mux.HandleFunc("GET /api/agent/config", AgentConfigHandler(deps))
	mux.HandleFunc("POST /api/agent/heartbeat", AgentHeartbeatHandler(deps))

	// Admin endpoints (admin token auth)
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /api/admin/openclaw/version", AdminGetVersionHandler(deps))
	adminMux.HandleFunc("PUT /api/admin/openclaw/version", AdminSetGlobalVersionHandler(deps))
	adminMux.HandleFunc("PUT /api/admin/openclaw/version/{id}", AdminSetInstanceVersionHandler(deps))
	adminAuth := adminTokenAuth(deps.Config.AdminAPIToken)
	mux.Handle("/api/admin/", adminAuth(adminMux))

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
		middleware.RateLimitProvisioning(),
	)

	return handler
}

// adminTokenAuth returns middleware that validates the X-Admin-Token header.
func adminTokenAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				WriteError(w, http.StatusForbidden, "forbidden", "admin API not configured")
				return
			}
			provided := r.Header.Get("X-Admin-Token")
			if provided == "" || provided != token {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid admin token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
