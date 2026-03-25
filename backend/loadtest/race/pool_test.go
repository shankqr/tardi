package race_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shanq/tardi/internal/models"
)

// TestConnectionPoolExhaustion verifies behavior when all connections are in use.
// With MaxConns=10, the 11th connection should wait (or timeout).
//
// Run with: go test -race -v -run TestConnectionPoolExhaustion
func TestConnectionPoolExhaustion(t *testing.T) {
	pool := getTestPool(t)

	// Hold 10 connections for 2 seconds each
	const holders = 10
	const waiters = 5

	var acquired atomic.Int32
	var waited atomic.Int32
	var timedOut atomic.Int32

	var wg sync.WaitGroup
	ready := make(chan struct{})

	// Holders: acquire and hold connections
	for i := 0; i < holders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Logf("holder failed to acquire: %v", err)
				return
			}
			acquired.Add(1)

			// Hold the connection for 2 seconds
			time.Sleep(2 * time.Second)
			conn.Release()
		}()
	}

	// Waiters: try to acquire while all connections are held
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready

			// Small delay to ensure holders acquire first
			time.Sleep(100 * time.Millisecond)

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			start := time.Now()
			conn, err := pool.Acquire(ctx)
			elapsed := time.Since(start)

			if err != nil {
				timedOut.Add(1)
				t.Logf("waiter timed out after %v: %v", elapsed, err)
				return
			}
			waited.Add(1)
			t.Logf("waiter acquired after waiting %v", elapsed)
			conn.Release()
		}()
	}

	close(ready)
	wg.Wait()

	t.Logf("Results: %d holders acquired, %d waiters eventually got through, %d timed out",
		acquired.Load(), waited.Load(), timedOut.Load())

	// Verify pool is still healthy
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		t.Errorf("pool unhealthy after exhaustion test: %v", err)
	}
}

// TestConcurrentDashboardQueries simulates the dashboard endpoint's DB access
// pattern under heavy concurrency. Each "request" does 4 sequential queries.
//
// Run with: go test -race -v -run TestConcurrentDashboardQueries
func TestConcurrentDashboardQueries(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	// Create some test data
	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusActive)
	createTestSnapshot(t, pool, inst.ID, models.SnapshotStatusReady)

	const concurrency = 20
	var errors atomic.Int32
	var successes atomic.Int32

	type dashboardResult struct {
		err     error
		elapsed time.Duration
	}

	results := runConcurrent(concurrency, func(i int) dashboardResult {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Simulate the 4 dashboard queries
		// 1. Get instances
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM vps_instances WHERE user_id = $1 AND status != 'terminated'`, user.ID).Scan(&count); err != nil {
			errors.Add(1)
			return dashboardResult{err: err, elapsed: time.Since(start)}
		}

		// 2. Get subscription
		var subCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM subscriptions WHERE user_id = $1`, user.ID).Scan(&subCount); err != nil {
			errors.Add(1)
			return dashboardResult{err: err, elapsed: time.Since(start)}
		}

		// 3. Get pending jobs
		var jobCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM provisioning_jobs j JOIN vps_instances i ON j.vps_instance_id = i.id WHERE i.user_id = $1 AND j.status IN ('pending','running','failed')`, user.ID).Scan(&jobCount); err != nil {
			errors.Add(1)
			return dashboardResult{err: err, elapsed: time.Since(start)}
		}

		// 4. Get snapshots
		var snapCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM snapshots WHERE vps_instance_id = $1`, inst.ID).Scan(&snapCount); err != nil {
			errors.Add(1)
			return dashboardResult{err: err, elapsed: time.Since(start)}
		}

		successes.Add(1)
		return dashboardResult{elapsed: time.Since(start)}
	})

	// Report results
	var maxLatency time.Duration
	for _, r := range results {
		if r.err != nil {
			t.Logf("query failed: %v", r.err)
		}
		if r.elapsed > maxLatency {
			maxLatency = r.elapsed
		}
	}

	t.Logf("Results: %d/%d succeeded, max latency: %v", successes.Load(), concurrency, maxLatency)

	if errors.Load() > 0 {
		t.Errorf("%d queries failed due to pool exhaustion (expected 0 with per-query acquire/release)", errors.Load())
	}
}
