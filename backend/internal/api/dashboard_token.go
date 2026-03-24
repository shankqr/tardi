package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/sshexec"
)

// DashboardTokenHandler reads the current gateway auth token from the VPS.
// OpenClaw may regenerate this token on startup (overwriting openclaw.json),
// so we always read it fresh rather than relying on the DB value.
// With allowInsecureAuth: true, this token grants full operator scopes.
//
// Also syncs the token back to DB so backend RPC calls (whatsapp.go) stay
// in sync with the actual gateway token.
func DashboardTokenHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.IPv4 == nil || *inst.IPv4 == "" || inst.RootPassword == nil || *inst.RootPassword == "" {
			WriteError(w, http.StatusConflict, "conflict", "instance not reachable")
			return
		}

		// Read the actual gateway token from openclaw.json on the VPS.
		// OpenClaw overwrites this file on startup and may generate a new token,
		// so the DB value can be stale.
		script := `cat /opt/openclaw/data/openclaw/openclaw.json | jq -r '.gateway.auth.token // empty'`

		out, err := sshexec.RunCommand(*inst.IPv4, *inst.RootPassword, script, 15*time.Second)
		if err != nil {
			slog.Error("dashboard-token: ssh failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to read dashboard token")
			return
		}

		token := strings.TrimSpace(out)
		if token == "" {
			WriteError(w, http.StatusBadGateway, "gateway_error", "no gateway token found on agent")
			return
		}

		// Sync token to DB if it changed (so backend RPC uses the correct token)
		if inst.OpenClawAuthToken == nil || *inst.OpenClawAuthToken != token {
			if err := db.UpdateInstanceOpenClawAuthToken(r.Context(), deps.Pool, instanceID, token); err != nil {
				slog.Warn("dashboard-token: failed to sync token to DB", "error", err, "instance_id", instanceID)
				// Non-fatal — token still works for dashboard
			}
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"token": token,
		})
	}
}
