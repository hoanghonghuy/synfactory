package postgres

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

func TestCapabilityPlacementKeepsIncompatibleJobQueued(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	heartbeatCapabilityWorker(t, store, "worker-linux", domain.WorkerCapabilities{
		OS: "linux", Architecture: "amd64", CPUClass: "8-cpu", MemoryClass: "16-gib", Docker: true,
		Runtimes: []string{"codex"}, Providers: []string{"openai_compatible"},
	}, now)
	_, _, err := store.CreateJob(ctx, NewJob{
		ID: "job-gpu", DedupeKey: "job-gpu-key", RepositoryID: repo.ID, Kind: "implementation", Role: domain.RoleDev,
		Subject: "gpu", AvailableAt: now, Requirements: domain.JobRequirements{OS: "linux", Architecture: "amd64", GPU: "nvidia"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok, err := store.ClaimJob(ctx, "worker-linux", now, time.Minute); err != nil || ok {
		t.Fatalf("incompatible claim: ok=%v err=%v", ok, err)
	}
	job, err := store.GetJob(ctx, "job-gpu")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobQueued || job.LeaseOwner != "" {
		t.Fatalf("incompatible job changed state: status=%s owner=%q", job.Status, job.LeaseOwner)
	}

	heartbeatCapabilityWorker(t, store, "worker-gpu", domain.WorkerCapabilities{
		OS: "linux", Architecture: "amd64", CPUClass: "8-cpu", MemoryClass: "16-gib", Docker: true,
		Runtimes: []string{"codex"}, Providers: []string{"openai_compatible"}, GPU: "nvidia",
	}, now)
	claimed, ok, err := store.ClaimJob(ctx, "worker-gpu", now, time.Minute)
	if err != nil || !ok || claimed.ID != "job-gpu" {
		t.Fatalf("compatible claim: job=%s ok=%v err=%v", claimed.ID, ok, err)
	}
}

func TestCapabilityHeartbeatUpdateDoesNotRewriteLease(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	heartbeatCapabilityWorker(t, store, "worker-1", domain.WorkerCapabilities{OS: "linux", Architecture: "amd64", Runtimes: []string{"codex"}}, now)
	_, _, err := store.CreateJob(ctx, NewJob{
		ID: "job-1", DedupeKey: "job-1-key", RepositoryID: repo.ID, Kind: "implementation", Role: domain.RoleDev,
		Subject: "1", AvailableAt: now, Requirements: domain.JobRequirements{Runtimes: []string{"codex"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimJob(ctx, "worker-1", now, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	heartbeatCapabilityWorker(t, store, "worker-1", domain.WorkerCapabilities{OS: "linux", Architecture: "amd64", Runtimes: []string{"opencode"}}, now.Add(time.Second))
	persisted, err := store.GetJob(ctx, claimed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.JobLeased || persisted.LeaseOwner != "worker-1" {
		t.Fatalf("heartbeat rewrote lease: status=%s owner=%q", persisted.Status, persisted.LeaseOwner)
	}
}

func TestConcurrentCompatibleWorkersStillLeaseJobOnce(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"worker-a", "worker-b"} {
		heartbeatCapabilityWorker(t, store, id, domain.WorkerCapabilities{OS: "linux", Architecture: "amd64", Docker: true}, now)
	}
	_, _, err := store.CreateJob(ctx, NewJob{
		ID: "job-docker", DedupeKey: "job-docker-key", RepositoryID: repo.ID, Kind: "implementation", Role: domain.RoleDev,
		Subject: "docker", AvailableAt: now, Requirements: domain.JobRequirements{Docker: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	claimed := make(chan string, 2)
	for _, id := range []string{"worker-a", "worker-b"} {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, ok, err := store.ClaimJob(context.Background(), id, now, time.Minute)
			if err != nil {
				t.Errorf("claim %s: %v", id, err)
				return
			}
			if ok {
				claimed <- job.ID
			}
		}()
	}
	wg.Wait()
	close(claimed)
	count := 0
	for range claimed {
		count++
	}
	if count != 1 {
		t.Fatalf("expected one compatible lease, got %d", count)
	}
}

func heartbeatCapabilityWorker(t *testing.T, store *Store, id string, capabilities domain.WorkerCapabilities, at time.Time) {
	t.Helper()
	metadata, err := json.Marshal(map[string]any{"capabilities": capabilities.Normalized()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.HeartbeatWorker(context.Background(), Worker{ID: id, Host: "test-host", Capacity: 1, Metadata: metadata}, at); err != nil {
		t.Fatal(err)
	}
}
