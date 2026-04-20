package api

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/billing"
	"github.com/shanq/tardi/internal/config"
	"github.com/shanq/tardi/internal/dns"
	"github.com/shanq/tardi/internal/jobs"
	"github.com/shanq/tardi/internal/provider"
)

type Dependencies struct {
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Config    *config.Config
	Billing   *billing.StripeService
	Registry  *provider.Registry
	Resumer   *jobs.Resumer
	Upgrader  *jobs.Upgrader
	DNSClient *dns.Client   // nil if Cloudflare DNS not configured
	BGTasks   *sync.WaitGroup // Tracks background goroutines (snapshots, restores) for graceful shutdown
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
	authedMux.HandleFunc("GET /api/instances/{id}/diagnostics", DiagnosticsHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/dashboard-token", DashboardTokenHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/terminal/ticket", TerminalTicketHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/doctor", DoctorHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/sync-config", SyncConfigHandler(deps))
	authedMux.HandleFunc("GET /api/instances/{id}/sync-status", SyncStatusHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/codex/link/start", CodexLinkStartHandler(deps))
	authedMux.HandleFunc("GET /api/instances/{id}/codex/link/status", CodexLinkStatusHandler(deps))
	authedMux.HandleFunc("POST /api/instances/{id}/codex/unlink", CodexUnlinkHandler(deps))
	authedMux.HandleFunc("POST /api/billing/portal", BillingPortalHandler(deps))

	// Google OAuth (delegated account access)
	authedMux.HandleFunc("GET /api/oauth/google/authorize", GoogleOAuthAuthorizeHandler(deps))
	authedMux.HandleFunc("GET /api/oauth/google/status", GoogleOAuthStatusHandler(deps))
	authedMux.HandleFunc("POST /api/oauth/google/disconnect", GoogleOAuthDisconnectHandler(deps))

	// Public API (no auth)
	mux.HandleFunc("GET /api/models", ListModelsHandler(deps))
	mux.HandleFunc("GET /api/oauth/google/callback", GoogleOAuthCallbackHandler(deps))

	// Web-terminal WebSocket. Auth is via short-lived HMAC ticket in ?t=
	// (browsers can't set Authorization on the WS upgrade).
	mux.HandleFunc("GET /api/instances/{id}/terminal/ws", TerminalWebSocketHandler(deps))

	// Agent phone-home endpoints (agent token auth, handled inside handlers)
	mux.HandleFunc("GET /api/agent/config", AgentConfigHandler(deps))
	mux.HandleFunc("POST /api/agent/heartbeat", AgentHeartbeatHandler(deps))
	mux.HandleFunc("GET /api/agent/heartbeat-script", AgentHeartbeatScriptHandler(deps))
	mux.HandleFunc("GET /api/agent/dashboard-shim", AgentDashboardShimHandler(deps))
	mux.HandleFunc("GET /api/agent/dashboard-shim-sha", AgentDashboardShimSHAHandler(deps))

	// Admin endpoints (admin token auth)
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /api/admin/openclaw/version", AdminGetVersionHandler(deps))
	adminMux.HandleFunc("PUT /api/admin/openclaw/version", AdminSetGlobalVersionHandler(deps))
	adminMux.HandleFunc("PUT /api/admin/openclaw/version/{id}", AdminSetInstanceVersionHandler(deps))
	adminMux.HandleFunc("GET /api/admin/hermes/version", AdminGetHermesVersionHandler(deps))
	adminMux.HandleFunc("PUT /api/admin/hermes/version", AdminSetHermesVersionHandler(deps))
	adminMux.HandleFunc("POST /api/admin/reset-password", AdminResetPasswordByIPHandler(deps))
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
