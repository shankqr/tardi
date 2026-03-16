package billing

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/stripe/stripe-go/v82"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	"github.com/stripe/stripe-go/v82/subscription"
	"github.com/stripe/stripe-go/v82/webhook"

	"github.com/shanq/tardi/internal/models"
)

// StripeService handles Stripe billing operations.
type StripeService struct {
	secretKey     string
	webhookSecret string
	logger        *slog.Logger
}

func NewStripeService(secretKey string, webhookSecret string, logger *slog.Logger) *StripeService {
	if secretKey == "" {
		logger.Info("stripe: running in stub mode (no secret key)")
	} else {
		stripe.Key = secretKey
	}
	return &StripeService{
		secretKey:     secretKey,
		webhookSecret: webhookSecret,
		logger:        logger,
	}
}

// VerifyWebhookSignature validates a Stripe webhook payload using the signature header.
// In dev mode (empty webhookSecret), it parses the JSON without signature verification.
func (s *StripeService) VerifyWebhookSignature(payload []byte, sigHeader string) (*stripe.Event, error) {
	if s.webhookSecret == "" {
		// Dev mode: parse JSON directly without signature verification
		var event stripe.Event
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("parse webhook event (dev): %w", err)
		}
		return &event, nil
	}

	event, err := webhook.ConstructEventWithOptions(payload, sigHeader, s.webhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		return nil, fmt.Errorf("verify webhook signature: %w", err)
	}
	return &event, nil
}

// GetSubscriptionPlanTier fetches a Stripe subscription and reads the plan_tier metadata
// from the first line item's price. Falls back to PlanStandard if metadata is missing.
func (s *StripeService) GetSubscriptionPlanTier(subscriptionID string) (models.PlanTier, error) {
	if s.secretKey == "" {
		return models.PlanStandard, nil
	}

	params := &stripe.SubscriptionParams{}
	params.AddExpand("items.data.price")
	sub, err := subscription.Get(subscriptionID, params)
	if err != nil {
		return "", fmt.Errorf("get subscription: %w", err)
	}

	if sub.Items != nil && len(sub.Items.Data) > 0 {
		price := sub.Items.Data[0].Price
		if price != nil && price.Metadata != nil {
			if tier, ok := price.Metadata["plan_tier"]; ok {
				switch tier {
				case "pro":
					return models.PlanPro, nil
				case "standard":
					return models.PlanStandard, nil
				}
			}
		}
	}

	return models.PlanStandard, nil
}

// CreateCustomerPortalSession creates a Stripe Billing Portal session and returns its URL.
// If subscriptionID is non-empty, the session deep-links to the plan update flow.
func (s *StripeService) CreateCustomerPortalSession(customerID, returnURL, subscriptionID string) (string, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}

	if subscriptionID != "" {
		params.FlowData = &stripe.BillingPortalSessionFlowDataParams{
			Type: stripe.String("subscription_update"),
			SubscriptionUpdate: &stripe.BillingPortalSessionFlowDataSubscriptionUpdateParams{
				Subscription: stripe.String(subscriptionID),
			},
		}
	}

	sess, err := portalsession.New(params)
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return sess.URL, nil
}
