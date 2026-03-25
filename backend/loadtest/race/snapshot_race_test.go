package race_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/api"
	"github.com/shanq/tardi/internal/models"
)

// TestSnapshotCreationRace_ThreeSnapshotLimit verifies that concurrent snapshot
// creation requests respect the 3-snapshot-per-instance limit.
//
// BUG: CountActiveSnapshotsByInstanceID → CreateSnapshot has no transaction.
// Four concurrent requests can all see count=2 and all create a snapshot.
//
// Run with: go test -race -count=100 -run TestSnapshotCreationRace_ThreeSnapshotLimit
func TestSnapshotCreationRace_ThreeSnapshotLimit(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	// Setup: user with active instance and 2 existing snapshots
	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusActive)
	createTestSnapshot(t, pool, inst.ID, models.SnapshotStatusReady)
	createTestSnapshot(t, pool, inst.ID, models.SnapshotStatusReady)

	deps := makeDeps(t, pool)
	handler := api.CreateSnapshotHandler(deps)

	const concurrency = 5

	type result struct {
		status int
		body   string
	}

	results := runConcurrent(concurrency, func(i int) result {
		body, _ := json.Marshal(map[string]string{
			"name": "snap-concurrent",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/instances/"+inst.ID.String()+"/snapshots", bytes.NewReader(body))
		req.SetPathValue("id", inst.ID.String())
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

	// Verify DB state: should be at most 3 total (2 existing + 1 new)
	var totalSnapshots int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM snapshots
		WHERE vps_instance_id = $1 AND status NOT IN ('deleted', 'error')
	`, inst.ID).Scan(&totalSnapshots)
	if err != nil {
		t.Fatalf("count snapshots: %v", err)
	}

	if created > 1 || totalSnapshots > 3 {
		t.Errorf("RACE CONDITION DETECTED: expected at most 1 new snapshot (3 total), got %d HTTP 201s and %d total in DB", created, totalSnapshots)
		for i, r := range results {
			t.Logf("  goroutine %d: HTTP %d — %s", i, r.status, r.body)
		}
	}
}

// TestSnapshotCreationRace_FromZero verifies concurrent snapshot creation from
// an instance with no existing snapshots.
func TestSnapshotCreationRace_FromZero(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusActive)

	deps := makeDeps(t, pool)
	handler := api.CreateSnapshotHandler(deps)

	const concurrency = 5

	type result struct {
		status int
	}

	results := runConcurrent(concurrency, func(i int) result {
		body, _ := json.Marshal(map[string]string{
			"name": "snap-from-zero",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/instances/"+inst.ID.String()+"/snapshots", bytes.NewReader(body))
		req.SetPathValue("id", inst.ID.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		return result{status: w.Code}
	})

	created := 0
	for _, r := range results {
		if r.status == http.StatusCreated {
			created++
		}
	}

	var totalSnapshots int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM snapshots
		WHERE vps_instance_id = $1 AND status NOT IN ('deleted', 'error')
	`, inst.ID).Scan(&totalSnapshots)
	if err != nil {
		t.Fatalf("count snapshots: %v", err)
	}

	if totalSnapshots > 3 {
		t.Errorf("RACE CONDITION DETECTED: expected at most 3 snapshots, got %d in DB (%d HTTP 201s)", totalSnapshots, created)
	}
}
