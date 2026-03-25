package race_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shanq/tardi/internal/api"
	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/models"
)

// TestConcurrentRestartAndDelete verifies that concurrent restart and delete
// requests on the same instance don't both succeed.
//
// BUG: Both handlers check status == active then update — no lock.
// Two concurrent requests can both pass the state check.
//
// Run with: go test -race -count=100 -run TestConcurrentRestartAndDelete
func TestConcurrentRestartAndDelete(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusActive)

	deps := makeDeps(t, pool)
	restartHandler := api.RestartInstanceHandler(deps)
	deleteHandler := api.DeleteInstanceHandler(deps)

	type result struct {
		operation string
		status    int
	}

	results := make([]result, 2)
	ready := make(chan struct{})

	// Launch restart goroutine
	go func() {
		<-ready
		req := httptest.NewRequest(http.MethodPost, "/api/instances/"+inst.ID.String()+"/restart", nil)
		req.SetPathValue("id", inst.ID.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()
		restartHandler.ServeHTTP(w, req)
		results[0] = result{operation: "restart", status: w.Code}
	}()

	// Launch delete goroutine
	go func() {
		<-ready
		req := httptest.NewRequest(http.MethodDelete, "/api/instances/"+inst.ID.String(), nil)
		req.SetPathValue("id", inst.ID.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()
		deleteHandler.ServeHTTP(w, req)
		results[1] = result{operation: "delete", status: w.Code}
	}()

	// Release both simultaneously
	close(ready)

	// Wait for background tasks to settle
	deps.BGTasks.Wait()

	// Check final instance state
	var finalStatus string
	err := pool.QueryRow(context.Background(), `
		SELECT status FROM vps_instances WHERE id = $1
	`, inst.ID).Scan(&finalStatus)
	if err != nil {
		t.Fatalf("query final status: %v", err)
	}

	// Count how many got 200 OK
	okCount := 0
	for _, r := range results {
		if r.status == http.StatusOK {
			okCount++
		}
	}

	if okCount > 1 {
		t.Errorf("RACE CONDITION DETECTED: both restart and delete succeeded (both got 200). Final status: %s", finalStatus)
		for _, r := range results {
			t.Logf("  %s: HTTP %d", r.operation, r.status)
		}
	}
}

// TestConcurrentMultipleRestarts verifies that only one restart succeeds
// when multiple restart requests arrive simultaneously.
//
// Run with: go test -race -count=100 -run TestConcurrentMultipleRestarts
func TestConcurrentMultipleRestarts(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusActive)

	deps := makeDeps(t, pool)
	handler := api.RestartInstanceHandler(deps)

	const concurrency = 5

	type result struct {
		status int
	}

	results := runConcurrent(concurrency, func(i int) result {
		req := httptest.NewRequest(http.MethodPost, "/api/instances/"+inst.ID.String()+"/restart", nil)
		req.SetPathValue("id", inst.ID.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		return result{status: w.Code}
	})

	okCount := 0
	for _, r := range results {
		if r.status == http.StatusOK {
			okCount++
		}
	}

	if okCount > 1 {
		t.Errorf("RACE CONDITION DETECTED: expected at most 1 restart to succeed, got %d", okCount)
	}

	// Wait for any background goroutines
	deps.BGTasks.Wait()
}

// TestConcurrentMultipleDeletes verifies that only one delete succeeds
// when multiple delete requests arrive simultaneously.
func TestConcurrentMultipleDeletes(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusActive)

	deps := makeDeps(t, pool)
	handler := api.DeleteInstanceHandler(deps)

	const concurrency = 5

	type result struct {
		status int
	}

	results := runConcurrent(concurrency, func(i int) result {
		req := httptest.NewRequest(http.MethodDelete, "/api/instances/"+inst.ID.String(), nil)
		req.SetPathValue("id", inst.ID.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		return result{status: w.Code}
	})

	okCount := 0
	for _, r := range results {
		if r.status == http.StatusOK {
			okCount++
		}
	}

	if okCount > 1 {
		t.Errorf("RACE CONDITION DETECTED: expected at most 1 delete to succeed, got %d", okCount)
	}

	deps.BGTasks.Wait()
}
