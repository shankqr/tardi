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

// SyncConfigHandler triggers an immediate config sync on the VPS by
// SSH-ing in and running the heartbeat script. This avoids the user
// having to wait up to 5 minutes for the next scheduled heartbeat.
func SyncConfigHandler(deps Dependencies) http.HandlerFunc {
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
			slog.Error("sync config: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.Status != "active" {
			WriteError(w, http.StatusConflict, "conflict", "instance is not active")
			return
		}

		if inst.IPv4 == nil || *inst.IPv4 == "" {
			WriteError(w, http.StatusConflict, "conflict", "instance has no IP address")
			return
		}

		if inst.RootPassword == nil || *inst.RootPassword == "" {
			WriteError(w, http.StatusConflict, "conflict", "instance has no root password")
			return
		}

		slog.Info("sync config: triggering heartbeat via SSH",
			"instance_id", instanceID,
			"ip", *inst.IPv4,
		)

		// Fire-and-forget: the heartbeat script can take 60-90s (docker pull,
		// container recreate, etc.) which causes browser timeouts. Run it in
		// the background and return immediately.
		ip := *inst.IPv4
		pw := *inst.RootPassword
		deps.BGTasks.Add(1)
		go func() {
			defer deps.BGTasks.Done()
			output, err := sshexec.RunCommand(ip, pw, "/opt/openclaw/heartbeat.sh", 120*time.Second)
			if err != nil {
				slog.Error("sync config: SSH command failed",
					"instance_id", instanceID,
					"error", err,
					"output", output,
				)
				return
			}
			slog.Info("sync config: heartbeat completed", "instance_id", instanceID)
		}()

		WriteJSON(w, http.StatusOK, map[string]any{
			"synced": true,
		})
	}
}
