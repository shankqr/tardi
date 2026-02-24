package billing

import (
	"log/slog"
)

// StripeService handles Stripe billing operations.
// Stub implementation for now.
type StripeService struct {
	logger *slog.Logger
}

func NewStripeService(secretKey string, webhookSecret string, logger *slog.Logger) *StripeService {
	if secretKey == "" {
		logger.Info("stripe: running in stub mode (no secret key)")
	}
	return &StripeService{logger: logger}
}
