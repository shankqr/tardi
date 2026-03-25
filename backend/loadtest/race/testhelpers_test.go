package race_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

var (
	testPool     *pgxpool.Pool
	testPoolOnce sync.Once
)

// getTestPool returns a shared connection pool for tests.
// Set DATABASE_URL env var to point to a test PostgreSQL instance.
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	testPoolOnce.Do(func() {
		dbURL := os.Getenv("DATABASE_URL")
		if dbURL == "" {
			dbURL = "postgres://postgres:postgres@localhost:5432/tardi_test?sslmode=disable"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var err error
		testPool, err = db.Connect(ctx, dbURL)
		if err != nil {
			// Will be nil if DB isn't available — tests will skip
			fmt.Fprintf(os.Stderr, "WARNING: Could not connect to test database: %v\n", err)
		}
	})

	if testPool == nil {
		t.Skip("Test database not available. Set DATABASE_URL or start docker-compose postgres.")
	}
	return testPool
}

// cleanupTables truncates all tables in dependency order.
func cleanupTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	tables := []string{
		"audit_log",
		"google_oauth_tokens",
		"snapshots",
		"agent_configs",
		"provisioning_jobs",
		"vps_instances",
		"subscriptions",
		"users",
	}
	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			t.Fatalf("cleanup table %s: %v", table, err)
		}
	}
}

// createTestUser inserts a test user and returns it.
func createTestUser(t *testing.T, pool *pgxpool.Pool) *models.User {
	t.Helper()
	ctx := context.Background()

	uid := "test-uid-" + uuid.NewString()[:8]
	email := uid + "@test.tardi.app"
	user, err := db.UpsertUser(ctx, pool, uid, email, nil)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return user
}

// createTestSubscription creates an active subscription for a user.
func createTestSubscription(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) *models.Subscription {
	t.Helper()
	ctx := context.Background()

	sub := &models.Subscription{
		ID:                   uuid.New(),
		UserID:               userID,
		StripeSubscriptionID: "sub_test_" + uuid.NewString()[:8],
		StripeCustomerID:     "cus_test_" + uuid.NewString()[:8],
		PlanTier:             models.PlanStandard,
		Status:               models.SubStatusActive,
	}
	if err := db.CreateSubscription(ctx, pool, sub); err != nil {
		t.Fatalf("create test subscription: %v", err)
	}
	return sub
}

// createTestInstance inserts an instance with the given status.
func createTestInstance(t *testing.T, pool *pgxpool.Pool, userID, subID uuid.UUID, status models.VpsStatus) *models.VpsInstance {
	t.Helper()
	ctx := context.Background()

	inst := &models.VpsInstance{
		ID:             uuid.New(),
		UserID:         userID,
		SubscriptionID: subID,
		Provider:       "hetzner",
		Name:           "test-agent-" + uuid.NewString()[:8],
		Region:         "eu-central",
		Status:         status,
		CreatedAt:      time.Now(),
	}
	if err := db.CreateInstance(ctx, pool, inst); err != nil {
		t.Fatalf("create test instance: %v", err)
	}

	// If active, set a provider_server_id so handlers work
	if status == models.VpsStatusActive {
		serverID := "srv-" + uuid.NewString()[:8]
		inst.ProviderServerID = &serverID
		if err := db.UpdateInstanceProviderInfo(ctx, pool, inst.ID, serverID, nil); err != nil {
			t.Fatalf("update test instance provider info: %v", err)
		}
	}

	return inst
}

// createTestSnapshot inserts a snapshot with the given status.
func createTestSnapshot(t *testing.T, pool *pgxpool.Pool, instanceID uuid.UUID, status models.SnapshotStatus) *models.Snapshot {
	t.Helper()
	ctx := context.Background()

	snap := &models.Snapshot{
		ID:            uuid.New(),
		VpsInstanceID: instanceID,
		Name:          "snap-" + uuid.NewString()[:8],
		Status:        status,
	}
	if err := db.CreateSnapshot(ctx, pool, snap); err != nil {
		t.Fatalf("create test snapshot: %v", err)
	}
	return snap
}

// seedProviderMapping ensures a provider mapping exists for tests.
func seedProviderMapping(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO provider_plan_mappings (id, plan_tier, provider, region, provider_server_type, provider_region, provider_image, monthly_cost_cents, is_available)
		VALUES ($1, 'standard', 'hetzner', 'eu-central', 'cx22', 'fsn1', 'ubuntu-24.04', 2900, true)
		ON CONFLICT DO NOTHING
	`, uuid.New())
	if err != nil {
		t.Fatalf("seed provider mapping: %v", err)
	}
}

// runConcurrent launches n goroutines that all start at the same time.
// Returns a slice of results (one per goroutine).
func runConcurrent[T any](n int, fn func(i int) T) []T {
	results := make([]T, n)
	var wg sync.WaitGroup
	wg.Add(n)

	// Barrier: all goroutines wait until ready, then start together
	ready := make(chan struct{})

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-ready // wait for barrier
			results[idx] = fn(idx)
		}(i)
	}

	// Release all goroutines simultaneously
	close(ready)
	wg.Wait()
	return results
}
