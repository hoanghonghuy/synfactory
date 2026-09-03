package domain

import (
	"testing"
	"time"
)

func TestJobStopsAfterRetryBudget(t *testing.T) {
	now := time.Now()
	job := Job{Status: JobQueued, MaxAttempts: 2}

	if err := job.Lease("worker-1", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := job.Start("worker-1"); err != nil {
		t.Fatal(err)
	}
	if err := job.Fail("worker-1", "ci failed", now); err != nil {
		t.Fatal(err)
	}
	if job.Status != JobRetryWait {
		t.Fatalf("expected retry_wait, got %s", job.Status)
	}

	if err := job.Requeue(now); err != nil {
		t.Fatal(err)
	}
	if err := job.Lease("worker-2", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := job.Start("worker-2"); err != nil {
		t.Fatal(err)
	}
	if err := job.Fail("worker-2", "ci still failed", now); err != nil {
		t.Fatal(err)
	}

	if job.Status != JobFailed {
		t.Fatalf("expected failed after budget exhaustion, got %s", job.Status)
	}
	if !job.Terminal() {
		t.Fatal("expected failed job to be terminal")
	}
}

func TestJobLeaseOwnerIsEnforced(t *testing.T) {
	job := Job{Status: JobQueued, MaxAttempts: 1}
	now := time.Now()
	if err := job.Lease("worker-1", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := job.Start("worker-2"); err != ErrLeaseOwnerMismatch {
		t.Fatalf("expected ErrLeaseOwnerMismatch, got %v", err)
	}
}

func TestJobCannotBeLeasedBeforeRetryDelay(t *testing.T) {
	now := time.Now()
	job := Job{Status: JobRetryWait, MaxAttempts: 2, AvailableAt: now.Add(time.Minute)}

	if err := job.Lease("worker-1", now, now.Add(2*time.Minute)); err != ErrJobNotAvailable {
		t.Fatalf("expected ErrJobNotAvailable, got %v", err)
	}
}
