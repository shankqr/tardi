package api

import (
	"log/slog"
	"net/http"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

func DashboardHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instances, err := db.GetInstancesByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			slog.Error("dashboard: get instances", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load instances")
			return
		}

		subscription, err := db.GetSubscriptionByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			slog.Error("dashboard: get subscription", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load subscription")
			return
		}

		pendingJobs, err := db.CountPendingJobsByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			slog.Error("dashboard: count pending jobs", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load jobs")
			return
		}

		snapshots, err := db.GetSnapshotsByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			slog.Error("dashboard: get snapshots", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load snapshots")
			return
		}

		// Auto-unlock instances stuck in snapshotting/restoring by checking provider state
		for i, inst := range instances {
			if inst.Status != models.VpsStatusSnapshotting && inst.Status != models.VpsStatusRestoring {
				continue
			}
			if inst.ProviderServerID == nil {
				continue
			}
			prov, err := deps.Registry.Get(inst.Provider)
			if err != nil {
				continue
			}
			server, err := prov.GetServer(r.Context(), *inst.ProviderServerID)
			if err != nil {
				continue
			}
			if server.Status != "running" {
				continue // operation still in progress at provider
			}

			// For snapshotting: also check no snapshots are still creating
			if inst.Status == models.VpsStatusSnapshotting {
				stillCreating := false
				for _, s := range snapshots {
					if s.VpsInstanceID == inst.ID && s.Status == models.SnapshotStatusCreating {
						stillCreating = true
						break
					}
				}
				if stillCreating {
					continue
				}
			}

			slog.Info("dashboard: provider shows operation complete, unlocking instance",
				"instance_id", inst.ID, "was_status", inst.Status,
			)
			_ = db.UpdateInstanceStatus(r.Context(), deps.Pool, inst.ID, models.VpsStatusActive)
			instances[i].Status = models.VpsStatusActive
		}

		instanceResponses := make([]models.InstanceResponse, 0, len(instances))
		for _, inst := range instances {
			instanceResponses = append(instanceResponses, models.ToInstanceResponse(inst))
		}

		snapshotResponses := make([]models.SnapshotResponse, 0, len(snapshots))
		for _, s := range snapshots {
			snapshotResponses = append(snapshotResponses, models.ToSnapshotResponse(s))
		}

		resp := models.DashboardStateResponse{
			Instances:    instanceResponses,
			Subscription: models.ToSubscriptionResponse(subscription),
			PendingJobs:  pendingJobs,
			Snapshots:    snapshotResponses,
		}

		WriteJSON(w, http.StatusOK, resp)
	}
}
