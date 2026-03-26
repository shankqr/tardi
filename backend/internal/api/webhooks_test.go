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
		{"active", stripe.SubscriptionStatusActive, models.SubStatusActive},
		{"trialing", stripe.SubscriptionStatusTrialing, models.SubStatusActive},
		{"past_due", stripe.SubscriptionStatusPastDue, models.SubStatusPastDue},
		{"canceled", stripe.SubscriptionStatusCanceled, models.SubStatusCanceled},
		{"paused", stripe.SubscriptionStatusPaused, models.SubStatusSuspended},
		{"unknown status defaults to active", "some_unknown_status", models.SubStatusActive},
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
			name:   "nil items",
			sub:    &stripe.Subscription{},
			expect: "",
		},
		{
			name: "empty items",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{},
				},
			},
			expect: "",
		},
		{
			name: "no price",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{{}},
				},
			},
			expect: "",
		},
		{
			name: "no metadata",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{Price: &stripe.Price{}},
					},
				},
			},
			expect: "",
		},
		{
			name: "metadata without plan_tier",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{Price: &stripe.Price{Metadata: map[string]string{"other": "value"}}},
					},
				},
			},
			expect: "",
		},
		{
			name: "standard tier",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{Price: &stripe.Price{Metadata: map[string]string{"plan_tier": "standard"}}},
					},
				},
			},
			expect: models.PlanStandard,
		},
		{
			name: "pro tier",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{Price: &stripe.Price{Metadata: map[string]string{"plan_tier": "pro"}}},
					},
				},
			},
			expect: models.PlanPro,
		},
		{
			name: "unrecognized tier",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{Price: &stripe.Price{Metadata: map[string]string{"plan_tier": "enterprise"}}},
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

func TestGetSubscriptionID(t *testing.T) {
	tests := []struct {
		name    string
		session *stripe.CheckoutSession
		expect  string
	}{
		{
			name:    "nil subscription",
			session: &stripe.CheckoutSession{},
			expect:  "",
		},
		{
			name: "with subscription",
			session: &stripe.CheckoutSession{
				Subscription: &stripe.Subscription{ID: "sub_123"},
			},
			expect: "sub_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSubscriptionID(tt.session)
			if got != tt.expect {
				t.Errorf("getSubscriptionID() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestGetCustomerID(t *testing.T) {
	tests := []struct {
		name    string
		session *stripe.CheckoutSession
		expect  string
	}{
		{
			name:    "nil customer",
			session: &stripe.CheckoutSession{},
			expect:  "",
		},
		{
			name: "with customer",
			session: &stripe.CheckoutSession{
				Customer: &stripe.Customer{ID: "cus_456"},
			},
			expect: "cus_456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getCustomerID(tt.session)
			if got != tt.expect {
				t.Errorf("getCustomerID() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestGetSubscriptionPeriodEnd(t *testing.T) {
	tests := []struct {
		name   string
		sub    *stripe.Subscription
		isNil  bool
		expect int64
	}{
		{
			name:  "nil items",
			sub:   &stripe.Subscription{},
			isNil: true,
		},
		{
			name: "empty items",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{},
				},
			},
			isNil: true,
		},
		{
			name: "with period end",
			sub: &stripe.Subscription{
				Items: &stripe.SubscriptionItemList{
					Data: []*stripe.SubscriptionItem{
						{CurrentPeriodEnd: 1700000000},
					},
				},
			},
			isNil:  false,
			expect: 1700000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSubscriptionPeriodEnd(tt.sub)
			if tt.isNil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
			} else {
				if got == nil {
					t.Fatal("expected non-nil, got nil")
				}
				expected := time.Unix(tt.expect, 0)
				if !got.Equal(expected) {
					t.Errorf("got %v, want %v", got, expected)
				}
			}
		})
	}
}
