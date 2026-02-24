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

		instanceResponses := make([]models.InstanceResponse, 0, len(instances))
		for _, inst := range instances {
			instanceResponses = append(instanceResponses, models.ToInstanceResponse(inst))
		}

		resp := models.DashboardStateResponse{
			Instances:    instanceResponses,
			Subscription: models.ToSubscriptionResponse(subscription),
			PendingJobs:  pendingJobs,
		}

		WriteJSON(w, http.StatusOK, resp)
	}
}
