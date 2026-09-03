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
	if err := job.Start("worker-1", now); err != nil {
		t.Fatal(err)
	}
	if err := job.Fail("worker-1", now, "ci failed", now); err != nil {
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
	if err := job.Start("worker-2", now); err != nil {
		t.Fatal(err)
	}
	if err := job.Fail("worker-2", now, "ci still failed", now); err != nil {
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
	if err := job.Start("worker-2", now); err != ErrLeaseOwnerMismatch {
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

func TestJobCannotStartWithExpiredLease(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Second)
	job := Job{
		Status:      JobLeased,
		MaxAttempts: 1,
		LeaseOwner:  "worker-1",
		LeaseUntil:  &expired,
	}

	if err := job.Start("worker-1", now); err != ErrLeaseExpired {
		t.Fatalf("expected ErrLeaseExpired, got %v", err)
	}
}

func TestJobRenewLease(t *testing.T) {
	now := time.Now()
	until := now.Add(time.Minute)
	job := Job{Status: JobQueued, MaxAttempts: 1}
	if err := job.Lease("worker-1", now, until); err != nil {
		t.Fatal(err)
	}

	renewedUntil := now.Add(2 * time.Minute)
	if err := job.RenewLease("worker-1", now, renewedUntil); err != nil {
		t.Fatal(err)
	}
	if job.LeaseUntil == nil || !job.LeaseUntil.Equal(renewedUntil) {
		t.Fatalf("expected renewed lease until %v, got %v", renewedUntil, job.LeaseUntil)
	}
}
