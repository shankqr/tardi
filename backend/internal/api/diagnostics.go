package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/sshexec"
)

// DiagnosticsHandler SSHes into the VPS and collects diagnostic info:
// container state, recent logs, openclaw.json, and .env (masked).
// GET /api/instances/{id}/diagnostics
func DiagnosticsHandler(deps Dependencies) http.HandlerFunc {
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

		cmd := `echo "=== CONTAINER STATE ===" && docker inspect openclaw-gateway --format='Status={{.State.Status}} Health={{.State.Health.Status}} Restarts={{.RestartCount}} StartedAt={{.State.StartedAt}}' 2>/dev/null || echo "container not found" && echo "" && echo "=== LAST 80 LOG LINES ===" && docker logs openclaw-gateway --tail 80 2>&1 && echo "" && echo "=== OPENCLAW.JSON ===" && cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null && echo "" && echo "=== ENV (masked) ===" && grep -v -E 'KEY=|TOKEN=|PASSWORD=' /opt/openclaw/.env 2>/dev/null && echo "" && echo "=== DISK ===" && df -h / 2>/dev/null && echo "" && echo "=== MEMORY ===" && free -m 2>/dev/null`

		out, err := sshexec.RunCommand(*inst.IPv4, *inst.RootPassword, cmd, 30*time.Second)
		if err != nil {
			slog.Error("diagnostics: ssh failed", "error", err, "instance_id", instanceID)
			WriteJSON(w, http.StatusOK, map[string]any{
				"error":  "SSH connection failed",
				"detail": err.Error(),
			})
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"output": out,
		})
	}
}
