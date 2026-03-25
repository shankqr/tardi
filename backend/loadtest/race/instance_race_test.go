package race_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/api"
	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

// TestInstanceCreationRace_OneAgentLimit verifies that concurrent instance
// creation requests respect the 1-agent-per-user limit.
//
// BUG: CountActiveInstancesByUserID → CreateInstance has no transaction.
// Two concurrent requests can both see count=0 and both create an instance.
//
// Run with: go test -race -count=100 -run TestInstanceCreationRace_OneAgentLimit
func TestInstanceCreationRace_OneAgentLimit(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	// Setup: user with active subscription, no instances
	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	seedProviderMapping(t, pool)
	_ = sub // subscription exists, that's what matters

	deps := makeDeps(t, pool)
	handler := api.CreateInstanceHandler(deps)

	const concurrency = 10

	type result struct {
		status int
		body   string
	}

	results := runConcurrent(concurrency, func(i int) result {
		body, _ := json.Marshal(map[string]string{
			"name":   "agent",
			"region": "eu-central",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/instances", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		return result{status: w.Code, body: w.Body.String()}
	})

	// Count how many succeeded (201 Created)
	created := 0
	for _, r := range results {
		if r.status == http.StatusCreated {
			created++
		}
	}

	// Also verify DB state
	count, err := countAllActiveInstances(pool, user.ID)
	if err != nil {
		t.Fatalf("count instances: %v", err)
	}

	if created != 1 || count != 1 {
		t.Errorf("RACE CONDITION DETECTED: expected exactly 1 instance created, got %d HTTP 201s and %d in DB", created, count)
		for i, r := range results {
			t.Logf("  goroutine %d: HTTP %d — %s", i, r.status, r.body)
		}
	}
}

// TestInstanceCreationRace_MultipleUsers verifies that concurrent requests from
// different users don't interfere with each other.
func TestInstanceCreationRace_MultipleUsers(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	seedProviderMapping(t, pool)
	deps := makeDeps(t, pool)
	handler := api.CreateInstanceHandler(deps)

	const numUsers = 10

	// Create users with subscriptions
	users := make([]*models.User, numUsers)
	for i := 0; i < numUsers; i++ {
		users[i] = createTestUser(t, pool)
		createTestSubscription(t, pool, users[i].ID)
	}

	type result struct {
		status int
		userID string
	}

	results := runConcurrent(numUsers, func(i int) result {
		body, _ := json.Marshal(map[string]string{
			"name":   "agent",
			"region": "eu-central",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/instances", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, users[i]))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		return result{status: w.Code, userID: users[i].ID.String()}
	})

	// Each user should get exactly 1 instance
	for i, r := range results {
		if r.status != http.StatusCreated {
			t.Errorf("user %d (%s): expected 201, got %d", i, r.userID, r.status)
		}
	}
}

// countAllActiveInstances queries DB directly for verification.
func countAllActiveInstances(pool *pgxpool.Pool, userID interface{}) (int, error) {
	var count int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM vps_instances
		WHERE user_id = $1 AND status NOT IN ('terminated', 'error')
	`, userID).Scan(&count)
	return count, err
}

// makeDeps creates minimal Dependencies for handler tests.
func makeDeps(t *testing.T, pool *pgxpool.Pool) api.Dependencies {
	t.Helper()
	registry := provider.NewRegistry()
	// No provider registered — tests don't reach provisioning step

	return api.Dependencies{
		Pool:     pool,
		Logger:   slog.Default(),
		BGTasks:  &sync.WaitGroup{},
		Registry: registry,
	}
}
