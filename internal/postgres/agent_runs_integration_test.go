package postgres

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

func TestAcquireAgentRunAtomicallyCreatesDurableIdentityAndReconnects(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	metadata, err := json.Marshal(map[string]any{"capabilities": domain.WorkerCapabilities{
		Version: domain.WorkerCapabilityVersion, OS: "linux", Architecture: "amd64", Runtimes: []string{"codex"},
	}.Normalized()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.HeartbeatAgentWorker(ctx, Worker{ID: "worker-1", Capacity: 1, Metadata: metadata}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateJob(ctx, NewJob{
		ID: "job-agent", DedupeKey: "job-agent-key", RepositoryID: repo.ID,
		Kind: "implementation", Role: domain.RoleDev, Subject: "84", Revision: "abc123",
		AvailableAt: now, Requirements: domain.JobRequirements{OS: "linux", Architecture: "amd64", Runtimes: []string{"codex"}},
	}); err != nil {
		t.Fatal(err)
	}

	first, ok, err := store.AcquireAgentRun(ctx, "worker-1", now, time.Minute)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	if first.Job.Status != domain.JobRunning || first.Job.LeaseOwner != "worker-1" || first.Job.Attempt != 1 {
		t.Fatalf("unexpected running job: %#v", first.Job)
	}
	if first.Run.ID == "" || first.Run.JobID != first.Job.ID || first.Run.Attempt != first.Job.Attempt || first.Run.Status != "running" {
		t.Fatalf("unexpected durable run: %#v", first.Run)
	}

	second, ok, err := store.AcquireAgentRun(ctx, "worker-1", now.Add(time.Second), time.Minute)
	if err != nil || !ok {
		t.Fatalf("reconnect acquire: ok=%v err=%v", ok, err)
	}
	if second.Job.ID != first.Job.ID || second.Run.ID != first.Run.ID {
		t.Fatalf("reconnect changed identity: first=%s/%s second=%s/%s", first.Job.ID, first.Run.ID, second.Job.ID, second.Run.ID)
	}

	var runCount int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE job_id = $1`, first.Job.ID).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Fatalf("expected one durable run, got %d", runCount)
	}
}

func TestConcurrentAgentPollsReuseOneRun(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := store.HeartbeatAgentWorker(ctx, Worker{ID: "worker-1", Capacity: 1}, now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"job-a", "job-b"} {
		if _, _, err := store.CreateJob(ctx, NewJob{
			ID: id, DedupeKey: id + "-key", RepositoryID: repo.ID, Kind: "implementation",
			Role: domain.RoleDev, Subject: id, Revision: id + "-rev", AvailableAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	results := make(chan AgentRun, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, ok, err := store.AcquireAgentRun(context.Background(), "worker-1", now, time.Minute)
			if err != nil {
				errs <- err
				return
			}
			if !ok {
				errs <- context.Canceled
				return
			}
			results <- value
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent acquire: %v", err)
	}
	var first *AgentRun
	count := 0
	for value := range results {
		value := value
		if first == nil {
			first = &value
		} else if value.Job.ID != first.Job.ID || value.Run.ID != first.Run.ID {
			t.Fatalf("concurrent poll created another identity: %#v vs %#v", *first, value)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("expected two idempotent responses, got %d", count)
	}
}

func TestAgentHeartbeatPreservesDrainAndAcquireRespectsIt(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := store.HeartbeatAgentWorker(ctx, Worker{ID: "worker-1", Capacity: 1}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkerDraining(ctx, "worker-1", true, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.HeartbeatAgentWorker(ctx, Worker{ID: "worker-1", Capacity: 1}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateJob(ctx, NewJob{
		ID: "job-drain", DedupeKey: "job-drain-key", RepositoryID: repo.ID, Kind: "implementation",
		Role: domain.RoleDev, Subject: "drain", Revision: "drain-rev", AvailableAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := store.AcquireAgentRun(ctx, "worker-1", now.Add(3*time.Second), time.Minute); err != nil || ok {
		t.Fatalf("draining worker acquire: ok=%v err=%v", ok, err)
	}
	var draining bool
	if err := store.db.QueryRowContext(ctx, `SELECT draining FROM workers WHERE id = 'worker-1'`).Scan(&draining); err != nil {
		t.Fatal(err)
	}
	if !draining {
		t.Fatal("agent heartbeat must not clear operator draining state")
	}
}

func TestExpiredAgentRunUsesExistingRecoverySemantics(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := store.HeartbeatAgentWorker(ctx, Worker{ID: "worker-1", Capacity: 1}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateJob(ctx, NewJob{
		ID: "job-expire", DedupeKey: "job-expire-key", RepositoryID: repo.ID, Kind: "implementation",
		Role: domain.RoleDev, Subject: "expire", Revision: "expire-rev", AvailableAt: now, MaxAttempts: 2,
	}); err != nil {
		t.Fatal(err)
	}
	value, ok, err := store.AcquireAgentRun(ctx, "worker-1", now, time.Second)
	if err != nil || !ok {
		t.Fatalf("acquire: ok=%v err=%v", ok, err)
	}

	recovered, err := store.RecoverExpiredLeases(ctx, now.Add(2*time.Second))
	if err != nil || recovered != 1 {
		t.Fatalf("recover: count=%d err=%v", recovered, err)
	}
	job, err := store.GetJob(ctx, value.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobRetryWait || job.LeaseOwner != "" {
		t.Fatalf("unexpected recovered job: %#v", job)
	}
	var status string
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM runs WHERE id = $1`, value.Run.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "timed_out" {
		t.Fatalf("run status = %s", status)
	}
}

func TestAcquireAgentRunFailsClosedWhenRunningLeaseHasNoMatchingActiveRun(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := store.HeartbeatAgentWorker(ctx, Worker{ID: "worker-1", Capacity: 1}, now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"job-owned", "job-next"} {
		if _, _, err := store.CreateJob(ctx, NewJob{
			ID: id, DedupeKey: id + "-key", RepositoryID: repo.ID, Kind: "implementation",
			Role: domain.RoleDev, Subject: id, Revision: id + "-rev", AvailableAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	owned, ok, err := store.AcquireAgentRun(ctx, "worker-1", now, time.Minute)
	if err != nil || !ok {
		t.Fatalf("initial acquire: ok=%v err=%v", ok, err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE runs SET status = 'succeeded', finished_at = $2 WHERE id = $1`, owned.Run.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := store.AcquireAgentRun(ctx, "worker-1", now.Add(2*time.Second), time.Minute); err == nil || ok {
		t.Fatalf("mismatched running ownership must fail closed: ok=%v err=%v", ok, err)
	}
	next, err := store.GetJob(ctx, "job-next")
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != domain.JobQueued || next.LeaseOwner != "" {
		t.Fatalf("second job was claimed despite unresolved running ownership: %#v", next)
	}
}
