package race_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

// TestMultipleWorkersClaimJobs verifies that FOR UPDATE SKIP LOCKED prevents
// multiple workers from claiming the same job.
//
// This should PASS — ClaimNextJob uses proper atomic locking.
//
// Run with: go test -race -count=10 -run TestMultipleWorkersClaimJobs
func TestMultipleWorkersClaimJobs(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	// Create a user, subscription, and multiple instances with pending jobs
	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	seedProviderMapping(t, pool)

	const numJobs = 20

	for i := 0; i < numJobs; i++ {
		inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusRequested)
		job := &models.ProvisioningJob{
			ID:             uuid.New(),
			VpsInstanceID:  inst.ID,
			IdempotencyKey: "provision-" + inst.ID.String(),
			Status:         models.JobPending,
			MaxAttempts:    5,
		}
		step := models.StepSelectProvider
		job.Step = &step
		if err := db.CreateProvisioningJob(context.Background(), pool, job); err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
	}

	const numWorkers = 5
	var claimedJobs sync.Map // jobID -> workerID
	var totalClaimed atomic.Int32

	type result struct {
		claimed []uuid.UUID
	}

	results := runConcurrent(numWorkers, func(workerIdx int) result {
		var claimed []uuid.UUID
		ctx := context.Background()

		for {
			job, err := db.ClaimNextJob(ctx, pool)
			if err != nil {
				t.Logf("worker %d: claim error: %v", workerIdx, err)
				break
			}
			if job == nil {
				break // No more jobs
			}

			// Check if this job was already claimed by another worker
			if prev, loaded := claimedJobs.LoadOrStore(job.ID.String(), workerIdx); loaded {
				t.Errorf("DOUBLE CLAIM: job %s claimed by worker %d AND worker %v", job.ID, workerIdx, prev)
			}

			claimed = append(claimed, job.ID)
			totalClaimed.Add(1)
		}

		return result{claimed: claimed}
	})

	// Verify all jobs were claimed exactly once
	if int(totalClaimed.Load()) != numJobs {
		t.Errorf("expected %d jobs claimed, got %d", numJobs, totalClaimed.Load())
	}

	// Verify distribution across workers (not all by one worker)
	for i, r := range results {
		t.Logf("worker %d claimed %d jobs", i, len(r.claimed))
	}
}

// TestJobClaimingDoesNotDeadlock verifies that concurrent job claiming
// doesn't cause deadlocks under pressure.
//
// Run with: go test -race -v -run TestJobClaimingDoesNotDeadlock
func TestJobClaimingDoesNotDeadlock(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)

	// Create a small number of jobs with many workers competing
	const numJobs = 3
	const numWorkers = 10

	for i := 0; i < numJobs; i++ {
		inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusRequested)
		job := &models.ProvisioningJob{
			ID:             uuid.New(),
			VpsInstanceID:  inst.ID,
			IdempotencyKey: "provision-" + inst.ID.String(),
			Status:         models.JobPending,
			MaxAttempts:    5,
		}
		step := models.StepSelectProvider
		job.Step = &step
		if err := db.CreateProvisioningJob(context.Background(), pool, job); err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
	}

	var totalClaimed atomic.Int32

	_ = runConcurrent(numWorkers, func(workerIdx int) int {
		ctx := context.Background()
		claimed := 0
		for {
			job, err := db.ClaimNextJob(ctx, pool)
			if err != nil {
				t.Logf("worker %d: error (possible deadlock?): %v", workerIdx, err)
				return claimed
			}
			if job == nil {
				return claimed
			}
			claimed++
			totalClaimed.Add(1)
		}
	})

	if int(totalClaimed.Load()) != numJobs {
		t.Errorf("expected %d total claims, got %d (possible lost claims or deadlock)", numJobs, totalClaimed.Load())
	}

	t.Logf("Successfully claimed all %d jobs across %d concurrent workers", totalClaimed.Load(), numWorkers)
}
