package billing

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/stripe/stripe-go/v82"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	"github.com/stripe/stripe-go/v82/webhook"
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

// CreateCustomerPortalSession creates a Stripe Billing Portal session and returns its URL.
func (s *StripeService) CreateCustomerPortalSession(customerID, returnURL string) (string, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	sess, err := portalsession.New(params)
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return sess.URL, nil
}
