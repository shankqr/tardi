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
