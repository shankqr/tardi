package api

import (
	"log/slog"
	"net/http"
)

// StripeWebhookHandler handles incoming Stripe webhook events.
// Stub implementation — logs the event and returns 200.
func StripeWebhookHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO: Verify Stripe webhook signature
		// TODO: Parse event type and route to handlers
		slog.Info("stripe webhook received", "content_length", r.ContentLength)
		w.WriteHeader(http.StatusOK)
	}
}
