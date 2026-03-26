package api

import (
	"testing"
	"time"

	"github.com/stripe/stripe-go/v82"

	"github.com/shanq/tardi/internal/models"
)

func TestMapStripeSubStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  stripe.SubscriptionStatus
		expect models.SubscriptionStatus
	}{
		{"active maps to SubStatusActive", stripe.SubscriptionStatusActive, models.SubStatusActive},
		{"trialing maps to SubStatusActive", stripe.SubscriptionStatusTrialing, models.SubStatusActive},
		{"past_due maps to SubStatusPastDue", stripe.SubscriptionStatusPastDue, models.SubStatusPastDue},
		{"canceled maps to SubStatusCanceled", stripe.SubscriptionStatusCanceled, models.SubStatusCanceled},
		{"paused maps to SubStatusSuspended", stripe.SubscriptionStatusPaused, models.SubStatusSuspended},
		{"unknown defaults to SubStatusActive", stripe.SubscriptionStatus("unknown"), models.SubStatusActive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapStripeSubStatus(tt.input)
			if got != tt.expect {
				t.Errorf("mapStripeSubStatus(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestExtractPlanTierFromSubscription(t *testing.T) {
	tests := []struct {
		name   string
		sub    *stripe.Subscription
		expect models.PlanTier
	}{
		{
			name: "pro metadata returns PlanPro",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{
							Price: &stripe.Price{
								Metadata: map[string]string{"plan_tier": "pro"},
							},
						},
					},
				},
			},
			expect: models.PlanPro,
		},
		{
			name: "standard metadata returns PlanStandard",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{
							Price: &stripe.Price{
								Metadata: map[string]string{"plan_tier": "standard"},
							},
						},
					},
				},
			},
			expect: models.PlanStandard,
		},
		{
			name:   "nil Items returns empty string",
			sub:    &stripe.Subscription{Items: nil},
			expect: "",
		},
		{
			name: "no metadata returns empty string",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{
							Price: &stripe.Price{
								Metadata: nil,
							},
						},
					},
				},
			},
			expect: "",
		},
		{
			name: "unknown tier value returns empty string",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{
							Price: &stripe.Price{
								Metadata: map[string]string{"plan_tier": "enterprise"},
							},
						},
					},
				},
			},
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPlanTierFromSubscription(tt.sub)
			if got != tt.expect {
				t.Errorf("extractPlanTierFromSubscription() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestGetSubscriptionPeriodEnd(t *testing.T) {
	t.Run("with items and CurrentPeriodEnd returns time", func(t *testing.T) {
		var ts int64 = 1700000000
		sub := &stripe.Subscription{
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{CurrentPeriodEnd: ts},
				},
			},
		}
		got := getSubscriptionPeriodEnd(sub)
		if got == nil {
			t.Fatal("expected non-nil time, got nil")
		}
		expected := time.Unix(ts, 0)
		if !got.Equal(expected) {
			t.Errorf("getSubscriptionPeriodEnd() = %v, want %v", *got, expected)
		}
	})

	t.Run("nil items returns nil", func(t *testing.T) {
		sub := &stripe.Subscription{Items: nil}
		got := getSubscriptionPeriodEnd(sub)
		if got != nil {
			t.Errorf("expected nil, got %v", *got)
		}
	})

	t.Run("empty items data returns nil", func(t *testing.T) {
		sub := &stripe.Subscription{
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{},
			},
		}
		got := getSubscriptionPeriodEnd(sub)
		if got != nil {
			t.Errorf("expected nil, got %v", *got)
		}
	})
}

func TestGetSubscriptionID(t *testing.T) {
	t.Run("with subscription returns ID", func(t *testing.T) {
		session := &stripe.CheckoutSession{
			Subscription: &stripe.Subscription{ID: "sub_12345"},
		}
		got := getSubscriptionID(session)
		if got != "sub_12345" {
			t.Errorf("getSubscriptionID() = %q, want %q", got, "sub_12345")
		}
	})

	t.Run("nil subscription returns empty string", func(t *testing.T) {
		session := &stripe.CheckoutSession{Subscription: nil}
		got := getSubscriptionID(session)
		if got != "" {
			t.Errorf("getSubscriptionID() = %q, want %q", got, "")
		}
	})
}

func TestGetCustomerID(t *testing.T) {
	t.Run("with customer returns ID", func(t *testing.T) {
		session := &stripe.CheckoutSession{
			Customer: &stripe.Customer{ID: "cus_67890"},
		}
		got := getCustomerID(session)
		if got != "cus_67890" {
			t.Errorf("getCustomerID() = %q, want %q", got, "cus_67890")
		}
	})

	t.Run("nil customer returns empty string", func(t *testing.T) {
		session := &stripe.CheckoutSession{Customer: nil}
		got := getCustomerID(session)
		if got != "" {
			t.Errorf("getCustomerID() = %q, want %q", got, "")
		}
	})
}
