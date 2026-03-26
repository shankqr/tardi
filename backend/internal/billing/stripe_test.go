package billing

import (
	"log/slog"
	"testing"

	"github.com/shanq/tardi/internal/models"
)

func TestNewStripeService_StubMode(t *testing.T) {
	svc := NewStripeService("", "", slog.Default())
	if svc == nil {
		t.Fatal("NewStripeService returned nil")
	}
	if svc.secretKey != "" {
		t.Errorf("secretKey = %q, want empty", svc.secretKey)
	}
}

func TestNewStripeService_WithKey(t *testing.T) {
	svc := NewStripeService("sk_test_123", "whsec_456", slog.Default())
	if svc == nil {
		t.Fatal("NewStripeService returned nil")
	}
	if svc.secretKey != "sk_test_123" {
		t.Errorf("secretKey = %q, want %q", svc.secretKey, "sk_test_123")
	}
	if svc.webhookSecret != "whsec_456" {
		t.Errorf("webhookSecret = %q, want %q", svc.webhookSecret, "whsec_456")
	}
}

func TestVerifyWebhookSignature_DevMode(t *testing.T) {
	svc := NewStripeService("", "", slog.Default())

	payload := []byte(`{"id": "evt_test", "type": "customer.subscription.created"}`)

	event, err := svc.VerifyWebhookSignature(payload, "")
	if err != nil {
		t.Fatalf("VerifyWebhookSignature: %v", err)
	}
	if event.ID != "evt_test" {
		t.Errorf("event.ID = %q, want %q", event.ID, "evt_test")
	}
	if event.Type != "customer.subscription.created" {
		t.Errorf("event.Type = %q, want %q", event.Type, "customer.subscription.created")
	}
}

func TestVerifyWebhookSignature_DevMode_InvalidJSON(t *testing.T) {
	svc := NewStripeService("", "", slog.Default())

	_, err := svc.VerifyWebhookSignature([]byte("not json"), "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGetSubscriptionPlanTier_StubMode(t *testing.T) {
	svc := NewStripeService("", "", slog.Default())

	tier, err := svc.GetSubscriptionPlanTier("sub_test_123")
	if err != nil {
		t.Fatalf("GetSubscriptionPlanTier: %v", err)
	}
	if tier != models.PlanStandard {
		t.Errorf("tier = %q, want %q", tier, models.PlanStandard)
	}
}
