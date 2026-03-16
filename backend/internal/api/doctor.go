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

// DoctorHandler runs `openclaw doctor` inside the VPS container and returns
// the output. This helps users diagnose issues with their agent.
// POST /api/instances/{id}/doctor
func DoctorHandler(deps Dependencies) http.HandlerFunc {
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

		cmd := `docker exec openclaw-gateway openclaw doctor 2>&1`

		out, err := sshexec.RunCommand(*inst.IPv4, *inst.RootPassword, cmd, 30*time.Second)
		if err != nil {
			// If the command ran but exited non-zero, we still have useful output
			if out != "" {
				slog.Warn("doctor: command exited with error but produced output",
					"instance_id", instanceID, "error", err)
				WriteJSON(w, http.StatusOK, map[string]any{
					"output": out,
				})
				return
			}
			slog.Error("doctor: ssh failed", "error", err, "instance_id", instanceID)
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
