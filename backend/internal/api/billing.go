package api

import (
	"log/slog"
	"net/http"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
)

type portalSessionResponse struct {
	URL string `json:"url"`
}

// BillingPortalHandler creates a Stripe Customer Portal session and returns its URL.
func BillingPortalHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		sub, err := db.GetSubscriptionByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			slog.Error("billing portal: get subscription", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load subscription")
			return
		}
		if sub == nil {
			WriteError(w, http.StatusNotFound, "not_found", "no subscription found")
			return
		}

		returnURL := r.Header.Get("Referer")
		if returnURL == "" {
			returnURL = deps.Config.AllowedOrigins[0] + "/dashboard/billing"
		}

		url, err := deps.Billing.CreateCustomerPortalSession(sub.StripeCustomerID, returnURL)
		if err != nil {
			slog.Error("billing portal: create session", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create billing portal session")
			return
		}

		WriteJSON(w, http.StatusOK, portalSessionResponse{URL: url})
	}
}
