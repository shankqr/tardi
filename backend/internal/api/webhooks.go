package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v82"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

const maxWebhookBodyBytes = 65536

// StripeWebhookHandler handles incoming Stripe webhook events.
func StripeWebhookHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read raw body before any JSON parsing (required for signature verification)
		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Warn("stripe webhook: failed to read body", "error", err)
			WriteError(w, http.StatusBadRequest, "bad_request", "failed to read request body")
			return
		}

		// Verify signature
		sigHeader := r.Header.Get("Stripe-Signature")
		event, err := deps.Billing.VerifyWebhookSignature(payload, sigHeader)
		if err != nil {
			slog.Warn("stripe webhook: signature verification failed", "error", err)
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid signature")
			return
		}

		slog.Info("stripe webhook received", "type", event.Type, "id", event.ID)

		// Route by event type
		switch event.Type {
		case "checkout.session.completed":
			handleCheckoutCompleted(r, deps, event)
		case "customer.subscription.updated":
			handleSubscriptionUpdated(r, deps, event)
		case "customer.subscription.deleted":
			handleSubscriptionDeleted(r, deps, event)
		case "invoice.payment_failed":
			handleInvoicePaymentFailed(r, deps, event)
		default:
			slog.Info("stripe webhook: unhandled event type", "type", event.Type)
		}

		// Always return 200 to acknowledge receipt
		w.WriteHeader(http.StatusOK)
	}
}

func handleCheckoutCompleted(r *http.Request, deps Dependencies, event *stripe.Event) {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		slog.Error("stripe webhook: unmarshal checkout session", "error", err)
		return
	}

	firebaseUID := session.ClientReferenceID
	if firebaseUID == "" {
		slog.Error("stripe webhook: checkout session missing client_reference_id", "session_id", session.ID)
		return
	}

	// Look up user by Firebase UID
	user, err := db.GetUserByFirebaseUID(r.Context(), deps.Pool, firebaseUID)
	if err != nil {
		slog.Error("stripe webhook: user not found", "firebase_uid", firebaseUID, "error", err)
		return
	}

	// Idempotency: check if subscription already exists
	existing, err := db.GetSubscriptionByUserID(r.Context(), deps.Pool, user.ID)
	if err != nil {
		slog.Error("stripe webhook: check existing subscription", "error", err)
		return
	}
	if existing != nil {
		slog.Info("stripe webhook: subscription already exists, skipping",
			"user_id", user.ID, "stripe_sub", getSubscriptionID(&session))
		return
	}

	// Create subscription record
	subID := getSubscriptionID(&session)
	custID := getCustomerID(&session)

	sub := &models.Subscription{
		ID:                   uuid.New(),
		UserID:               user.ID,
		StripeSubscriptionID: subID,
		StripeCustomerID:     custID,
		PlanTier:             models.PlanStandard,
		Status:               models.SubStatusActive,
	}

	if err := db.CreateSubscription(r.Context(), deps.Pool, sub); err != nil {
		slog.Error("stripe webhook: create subscription", "error", err)
		return
	}

	// Audit log
	resourceID := sub.ID
	_ = db.InsertAuditLog(r.Context(), deps.Pool, &models.AuditLogEntry{
		ID:           uuid.New(),
		UserID:       user.ID,
		Action:       "create",
		ResourceType: "subscription",
		ResourceID:   &resourceID,
		Metadata:     map[string]any{"stripe_subscription_id": subID},
	})

	slog.Info("stripe webhook: subscription created",
		"user_id", user.ID, "stripe_sub", subID)
}

func handleSubscriptionUpdated(r *http.Request, deps Dependencies, event *stripe.Event) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		slog.Error("stripe webhook: unmarshal subscription", "error", err)
		return
	}

	status := mapStripeSubStatus(sub.Status)
	periodEnd := getSubscriptionPeriodEnd(&sub)

	if err := db.UpdateSubscriptionStatus(r.Context(), deps.Pool, sub.ID, status, periodEnd); err != nil {
		slog.Error("stripe webhook: update subscription", "stripe_sub", sub.ID, "error", err)
		return
	}

	slog.Info("stripe webhook: subscription updated",
		"stripe_sub", sub.ID, "status", status, "period_end", periodEnd)
}

func handleSubscriptionDeleted(r *http.Request, deps Dependencies, event *stripe.Event) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		slog.Error("stripe webhook: unmarshal subscription", "error", err)
		return
	}

	periodEnd := getSubscriptionPeriodEnd(&sub)
	if err := db.UpdateSubscriptionStatus(r.Context(), deps.Pool, sub.ID, models.SubStatusCanceled, periodEnd); err != nil {
		slog.Error("stripe webhook: delete subscription", "stripe_sub", sub.ID, "error", err)
		return
	}

	slog.Info("stripe webhook: subscription canceled", "stripe_sub", sub.ID)
}

func handleInvoicePaymentFailed(r *http.Request, deps Dependencies, event *stripe.Event) {
	// In stripe-go v82, Invoice.Subscription moved to Invoice.Parent.SubscriptionDetails.
	// Use raw JSON to extract the subscription ID reliably.
	var raw struct {
		ID     string `json:"id"`
		Parent struct {
			SubscriptionDetails struct {
				Subscription struct {
					ID string `json:"id"`
				} `json:"subscription"`
			} `json:"subscription_details"`
		} `json:"parent"`
	}
	if err := json.Unmarshal(event.Data.Raw, &raw); err != nil {
		slog.Error("stripe webhook: unmarshal invoice", "error", err)
		return
	}

	subID := raw.Parent.SubscriptionDetails.Subscription.ID
	if subID == "" {
		slog.Warn("stripe webhook: invoice.payment_failed with no subscription")
		return
	}

	if err := db.UpdateSubscriptionStatus(r.Context(), deps.Pool, subID, models.SubStatusPastDue, nil); err != nil {
		slog.Error("stripe webhook: mark past_due", "stripe_sub", subID, "error", err)
		return
	}

	slog.Info("stripe webhook: subscription marked past_due",
		"stripe_sub", subID, "invoice", raw.ID)
}

// getSubscriptionPeriodEnd extracts current_period_end from a subscription.
// In stripe-go v82, this field moved from Subscription to SubscriptionItem.
func getSubscriptionPeriodEnd(sub *stripe.Subscription) *time.Time {
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		t := time.Unix(sub.Items.Data[0].CurrentPeriodEnd, 0)
		return &t
	}
	return nil
}

// getSubscriptionID extracts the subscription ID from a checkout session.
// The Subscription field is an expandable *Subscription object.
func getSubscriptionID(session *stripe.CheckoutSession) string {
	if session.Subscription != nil {
		return session.Subscription.ID
	}
	return ""
}

// getCustomerID extracts the customer ID from a checkout session.
func getCustomerID(session *stripe.CheckoutSession) string {
	if session.Customer != nil {
		return session.Customer.ID
	}
	return ""
}

// mapStripeSubStatus converts Stripe subscription status to internal status.
func mapStripeSubStatus(stripeStatus stripe.SubscriptionStatus) models.SubscriptionStatus {
	switch stripeStatus {
	case stripe.SubscriptionStatusActive, stripe.SubscriptionStatusTrialing:
		return models.SubStatusActive
	case stripe.SubscriptionStatusPastDue:
		return models.SubStatusPastDue
	case stripe.SubscriptionStatusCanceled:
		return models.SubStatusCanceled
	case stripe.SubscriptionStatusPaused:
		return models.SubStatusSuspended
	default:
		return models.SubStatusActive
	}
}
