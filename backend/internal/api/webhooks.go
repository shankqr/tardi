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

	// Upsert user — they may not have hit an authenticated endpoint yet
	email := ""
	if session.CustomerDetails != nil {
		email = session.CustomerDetails.Email
	}
	user, err := db.UpsertUser(r.Context(), deps.Pool, firebaseUID, email, nil)
	if err != nil {
		slog.Error("stripe webhook: upsert user failed", "firebase_uid", firebaseUID, "error", err)
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

	// Determine plan tier from Stripe price metadata
	planTier, err := deps.Billing.GetSubscriptionPlanTier(subID)
	if err != nil {
		slog.Warn("stripe webhook: could not determine plan tier, defaulting to standard",
			"stripe_sub", subID, "error", err)
		planTier = models.PlanStandard
	}

	sub := &models.Subscription{
		ID:                   uuid.New(),
		UserID:               user.ID,
		StripeSubscriptionID: subID,
		StripeCustomerID:     custID,
		PlanTier:             planTier,
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
	cancelAtPeriodEnd := sub.CancelAtPeriodEnd

	// Check if this is a reactivation from suspended — must query BEFORE updating status
	var wasSuspended bool
	if status == models.SubStatusActive {
		prevSub, _ := db.GetSubscriptionByStripeSubID(r.Context(), deps.Pool, sub.ID)
		if prevSub != nil && prevSub.Status == models.SubStatusSuspended {
			wasSuspended = true
		}
	}

	if err := db.UpdateSubscriptionStatus(r.Context(), deps.Pool, sub.ID, status, periodEnd, cancelAtPeriodEnd); err != nil {
		slog.Error("stripe webhook: update subscription", "stripe_sub", sub.ID, "error", err)
		return
	}

	// Trigger resume for suspended instances when subscription reactivates
	if wasSuspended {
		prevSub, _ := db.GetSubscriptionByStripeSubID(r.Context(), deps.Pool, sub.ID)
		if prevSub != nil {
			instances, _ := db.GetInstancesBySubscriptionID(r.Context(), deps.Pool, prevSub.ID)
			for i := range instances {
				if instances[i].Status == models.VpsStatusSuspended {
					slog.Info("stripe webhook: triggering resume for suspended instance",
						"instance_id", instances[i].ID, "stripe_sub", sub.ID)
					deps.Resumer.ResumeInstance(&instances[i])
				}
			}
		}
	}

	// Detect plan tier change (upgrade/downgrade via Stripe Customer Portal)
	if status == models.SubStatusActive && !wasSuspended && deps.Upgrader != nil {
		newTier := extractPlanTierFromSubscription(&sub)
		if newTier != "" {
			prevSub, _ := db.GetSubscriptionByStripeSubID(r.Context(), deps.Pool, sub.ID)
			if prevSub != nil && prevSub.PlanTier != newTier {
				// Update plan tier immediately so the billing page reflects the change
				if err := db.UpdateSubscriptionPlanTier(r.Context(), deps.Pool, prevSub.ID, newTier); err != nil {
					slog.Error("stripe webhook: update plan tier", "error", err)
				}
				instances, _ := db.GetInstancesBySubscriptionID(r.Context(), deps.Pool, prevSub.ID)
				for i := range instances {
					if instances[i].Status == models.VpsStatusActive {
						if newTier == models.PlanPro && prevSub.PlanTier == models.PlanStandard {
							slog.Info("stripe webhook: triggering upgrade to pro",
								"instance_id", instances[i].ID, "stripe_sub", sub.ID)
							deps.Upgrader.UpgradeInstance(&instances[i], newTier)
						} else if newTier == models.PlanStandard && prevSub.PlanTier == models.PlanPro {
							slog.Info("stripe webhook: triggering downgrade to standard",
								"instance_id", instances[i].ID, "stripe_sub", sub.ID)
							deps.Upgrader.DowngradeInstance(&instances[i], newTier)
						}
					}
				}
			}
		}
	}

	slog.Info("stripe webhook: subscription updated",
		"stripe_sub", sub.ID, "status", status, "cancel_at_period_end", cancelAtPeriodEnd)
}

func handleSubscriptionDeleted(r *http.Request, deps Dependencies, event *stripe.Event) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		slog.Error("stripe webhook: unmarshal subscription", "error", err)
		return
	}

	periodEnd := getSubscriptionPeriodEnd(&sub)
	if err := db.UpdateSubscriptionStatus(r.Context(), deps.Pool, sub.ID, models.SubStatusCanceled, periodEnd, false); err != nil {
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

	if err := db.UpdateSubscriptionStatus(r.Context(), deps.Pool, subID, models.SubStatusPastDue, nil, false); err != nil {
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

// extractPlanTierFromSubscription reads plan_tier from the first item's price metadata.
func extractPlanTierFromSubscription(sub *stripe.Subscription) models.PlanTier {
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		price := sub.Items.Data[0].Price
		if price != nil && price.Metadata != nil {
			if tier, ok := price.Metadata["plan_tier"]; ok {
				switch tier {
				case "pro":
					return models.PlanPro
				case "standard":
					return models.PlanStandard
				}
			}
		}
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
