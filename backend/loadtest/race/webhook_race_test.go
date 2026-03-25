package race_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

// TestConcurrentSubscriptionStatusUpdates verifies that concurrent webhook
// events updating the same subscription don't corrupt the data.
//
// BUG: handleSubscriptionUpdated reads prevSub BEFORE updating status.
// Another webhook can interleave, causing the reactivation logic to trigger
// on stale state.
//
// Run with: go test -race -count=100 -run TestConcurrentSubscriptionStatusUpdates
func TestConcurrentSubscriptionStatusUpdates(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)

	// Simulate concurrent status updates at the DB level
	// (testing the DB layer directly since webhook handlers need Stripe signature verification)
	const concurrency = 10

	type result struct {
		err error
	}

	// Half set active, half set past_due
	results := runConcurrent(concurrency, func(i int) result {
		var status models.SubscriptionStatus
		if i%2 == 0 {
			status = models.SubStatusActive
		} else {
			status = models.SubStatusPastDue
		}

		now := time.Now()
		err := db.UpdateSubscriptionStatus(
			context.Background(), pool,
			sub.StripeSubscriptionID, status, &now, false,
		)
		return result{err: err}
	})

	for i, r := range results {
		if r.err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, r.err)
		}
	}

	// Final state should be one of the two statuses (not corrupted)
	finalSub, err := db.GetSubscriptionByStripeSubID(context.Background(), pool, sub.StripeSubscriptionID)
	if err != nil {
		t.Fatalf("get final subscription: %v", err)
	}

	if finalSub.Status != models.SubStatusActive && finalSub.Status != models.SubStatusPastDue {
		t.Errorf("unexpected final status: %s (expected 'active' or 'past_due')", finalSub.Status)
	}
}

// TestDoubleReactivation verifies that concurrent reactivation from suspended
// state doesn't trigger duplicate resume operations.
//
// This tests the DB-level race: two concurrent reads of prevSub both see
// "suspended", both decide to trigger resume. The resume itself should be
// idempotent, but the double-trigger wastes resources.
//
// Run with: go test -race -count=100 -run TestDoubleReactivation
func TestDoubleReactivation(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)

	// Create subscription in suspended state
	sub := &models.Subscription{
		ID:                   uuid.New(),
		UserID:               user.ID,
		StripeSubscriptionID: "sub_reactivation_" + uuid.NewString()[:8],
		StripeCustomerID:     "cus_test_" + uuid.NewString()[:8],
		PlanTier:             models.PlanStandard,
		Status:               models.SubStatusSuspended,
	}
	if err := db.CreateSubscription(context.Background(), pool, sub); err != nil {
		t.Fatalf("create suspended subscription: %v", err)
	}

	const concurrency = 5

	type result struct {
		wasSuspended bool
	}

	// Simulate the reactivation check pattern from handleSubscriptionUpdated
	results := runConcurrent(concurrency, func(i int) result {
		// Step 1: Read previous status (the race window)
		prevSub, _ := db.GetSubscriptionByStripeSubID(context.Background(), pool, sub.StripeSubscriptionID)
		wasSuspended := prevSub != nil && prevSub.Status == models.SubStatusSuspended

		// Step 2: Update to active
		now := time.Now()
		_ = db.UpdateSubscriptionStatus(
			context.Background(), pool,
			sub.StripeSubscriptionID, models.SubStatusActive, &now, false,
		)

		return result{wasSuspended: wasSuspended}
	})

	// Count how many goroutines thought they should trigger resume
	resumeTriggers := 0
	for _, r := range results {
		if r.wasSuspended {
			resumeTriggers++
		}
	}

	if resumeTriggers > 1 {
		t.Errorf("RACE CONDITION: %d goroutines would trigger resume (expected 1). "+
			"Multiple concurrent webhooks all see 'suspended' before any update completes.", resumeTriggers)
	}
}

// TestConcurrentSubscriptionCreation verifies that the idempotency check in
// handleCheckoutCompleted prevents duplicate subscriptions.
func TestConcurrentSubscriptionCreation(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)

	const concurrency = 5
	stripeSubID := "sub_checkout_" + uuid.NewString()[:8]

	type result struct {
		created bool
		err     error
	}

	// Simulate the idempotency pattern: check if exists, then create
	results := runConcurrent(concurrency, func(i int) result {
		ctx := context.Background()

		// Step 1: Check if subscription exists (the race window)
		existing, err := db.GetSubscriptionByUserID(ctx, pool, user.ID)
		if err != nil {
			return result{err: err}
		}
		if existing != nil {
			return result{created: false}
		}

		// Step 2: Create subscription
		sub := &models.Subscription{
			ID:                   uuid.New(),
			UserID:               user.ID,
			StripeSubscriptionID: stripeSubID + "-" + uuid.NewString()[:4],
			StripeCustomerID:     "cus_test_" + uuid.NewString()[:8],
			PlanTier:             models.PlanStandard,
			Status:               models.SubStatusActive,
		}
		err = db.CreateSubscription(ctx, pool, sub)
		if err != nil {
			return result{err: err}
		}
		return result{created: true}
	})

	createdCount := 0
	for _, r := range results {
		if r.created {
			createdCount++
		}
	}

	// Verify DB state
	var subCount int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM subscriptions WHERE user_id = $1
	`, user.ID).Scan(&subCount)
	if err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}

	if subCount > 1 {
		t.Errorf("RACE CONDITION: %d subscriptions created for same user (expected 1). "+
			"HTTP 201 responses: %d, DB count: %d", subCount, createdCount, subCount)
	}
}
