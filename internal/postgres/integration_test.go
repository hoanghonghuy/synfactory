package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("SYNFACTORY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SYNFACTORY_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := Open(ctx, dsn, Options{MaxOpenConns: 20, MaxIdleConns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyMigrations(ctx); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
TRUNCATE evidence, runs, jobs, event_inbox, reconcile_state, workers, repositories
RESTART IDENTITY CASCADE`); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedRepository(t *testing.T, store *Store) Repository {
	t.Helper()
	repo, err := store.UpsertRepository(context.Background(), Repository{
		ID:            "repo-1",
		Provider:      "github",
		FullName:      "hoanghonghuy/synfactory-test",
		DefaultBranch: "develop",
		Enabled:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestEventAndJobDedupe(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()

	event := InboxEvent{
		DedupeKey:    "event-key-1",
		Provider:     "github",
		RepositoryID: repo.ID,
		Kind:         "pull_request.synchronize",
		Subject:      "55",
		Revision:     "abc123",
	}
	first, inserted, err := store.PutEvent(ctx, event)
	if err != nil || !inserted {
		t.Fatalf("first event insert: inserted=%v err=%v", inserted, err)
	}
	second, inserted, err := store.PutEvent(ctx, event)
	if err != nil || inserted {
		t.Fatalf("duplicate event insert: inserted=%v err=%v", inserted, err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected duplicate event to resolve existing id: %d != %d", first.ID, second.ID)
	}

	jobInput := NewJob{
		ID:            "job-1",
		DedupeKey:     "job-key-1",
		RepositoryID:  repo.ID,
		SourceEventID: &first.ID,
		Kind:          "pr_review",
		Role:          domain.RoleTeamLead,
		Subject:       "55",
		Revision:      "abc123",
		MaxAttempts:   3,
	}
	firstJob, inserted, err := store.CreateJob(ctx, jobInput)
	if err != nil || !inserted {
		t.Fatalf("first job insert: inserted=%v err=%v", inserted, err)
	}
	jobInput.ID = "job-2"
	secondJob, inserted, err := store.CreateJob(ctx, jobInput)
	if err != nil || inserted {
		t.Fatalf("duplicate job insert: inserted=%v err=%v", inserted, err)
	}
	if firstJob.ID != secondJob.ID {
		t.Fatalf("expected duplicate job to resolve existing id: %s != %s", firstJob.ID, secondJob.ID)
	}
}

func TestConcurrentWorkersClaimJobOnce(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	_, _, err := store.CreateJob(ctx, NewJob{
		ID:           "job-claim",
		DedupeKey:    "job-claim-key",
		RepositoryID: repo.ID,
		Kind:         "implementation",
		Role:         domain.RoleDev,
		Subject:      "2",
		MaxAttempts:  3,
		AvailableAt:  time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 8
	var wg sync.WaitGroup
	claimed := make(chan string, workers)
	now := time.Now().UTC()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			job, ok, err := store.ClaimJob(context.Background(), fmt.Sprintf("worker-%d", i), now, time.Minute)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if ok {
				claimed <- job.ID
			}
		}(i)
	}
	wg.Wait()
	close(claimed)

	count := 0
	for id := range claimed {
		if id != "job-claim" {
			t.Fatalf("unexpected claimed job: %s", id)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("expected one successful claim, got %d", count)
	}
}

func TestFutureJobIsNotClaimed(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	now := time.Now().UTC()
	_, _, err := store.CreateJob(context.Background(), NewJob{
		ID:           "job-future",
		DedupeKey:    "job-future-key",
		RepositoryID: repo.ID,
		Kind:         "implementation",
		Role:         domain.RoleDev,
		Subject:      "future",
		MaxAttempts:  3,
		AvailableAt:  now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := store.ClaimJob(context.Background(), "worker-1", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("future job must not be claimed")
	}
}

func TestExpiredRunningLeaseRecoversAndBudgetStops(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()
	_, _, err := store.CreateJob(ctx, NewJob{
		ID:           "job-recovery",
		DedupeKey:    "job-recovery-key",
		RepositoryID: repo.ID,
		Kind:         "ci_repair",
		Role:         domain.RoleDev,
		Subject:      "pr-55",
		MaxAttempts:  2,
		AvailableAt:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimJob(ctx, "worker-1", now, time.Second)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	running, err := store.StartJob(ctx, claimed.ID, "worker-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRun(ctx, Run{
		ID:      "run-1",
		JobID:   running.ID,
		Attempt: running.Attempt,
		Runtime: "codex",
		Status:  "running",
	}); err != nil {
		t.Fatal(err)
	}

	recoveryAt := now.Add(2 * time.Second)
	recovered, err := store.RecoverExpiredLeases(ctx, recoveryAt)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("expected one recovered lease, got %d", recovered)
	}
	job, err := store.GetJob(ctx, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobRetryWait || job.Attempt != 1 {
		t.Fatalf("expected retry_wait attempt 1, got status=%s attempt=%d", job.Status, job.Attempt)
	}

	claimed, ok, err = store.ClaimJob(ctx, "worker-2", recoveryAt, time.Minute)
	if err != nil || !ok {
		t.Fatalf("reclaim: ok=%v err=%v", ok, err)
	}
	running, err = store.StartJob(ctx, claimed.ID, "worker-2", recoveryAt)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := store.FailJob(ctx, running.ID, "worker-2", recoveryAt, recoveryAt, "ci still failing")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != domain.JobFailed || !failed.Terminal() {
		t.Fatalf("expected terminal failed job, got %s", failed.Status)
	}
}

func TestQueuedWorkSurvivesStoreRestart(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	dsn := os.Getenv("SYNFACTORY_TEST_DATABASE_URL")
	now := time.Now().UTC()
	_, _, err := store.CreateJob(context.Background(), NewJob{
		ID:           "job-restart",
		DedupeKey:    "job-restart-key",
		RepositoryID: repo.ID,
		Kind:         "implementation",
		Role:         domain.RoleDev,
		Subject:      "restart",
		MaxAttempts:  3,
		AvailableAt:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(context.Background(), dsn, Options{MaxOpenConns: 5, MaxIdleConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	job, ok, err := reopened.ClaimJob(context.Background(), "worker-restarted", now, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim after restart: ok=%v err=%v", ok, err)
	}
	if job.ID != "job-restart" {
		t.Fatalf("unexpected job after restart: %s", job.ID)
	}
}
