package agentpostgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/agent"
	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type fakeStore struct {
	heartbeat postgres.Worker
	active    postgres.AgentRun
	hasActive bool
	acquired  postgres.AgentRun
	renewed   int
}

func (f *fakeStore) HeartbeatAgentWorker(_ context.Context, worker postgres.Worker, _ time.Time) (postgres.Worker, error) {
	f.heartbeat = worker
	return worker, nil
}

func (f *fakeStore) ActiveAgentRun(context.Context, string, time.Time) (postgres.AgentRun, bool, error) {
	return f.active, f.hasActive, nil
}

func (f *fakeStore) AcquireAgentRun(context.Context, string, time.Time, time.Duration) (postgres.AgentRun, bool, error) {
	if f.acquired.Run.ID == "" {
		return postgres.AgentRun{}, false, nil
	}
	return f.acquired, true, nil
}

func (f *fakeStore) RenewLease(context.Context, string, string, time.Time, time.Duration) (domain.Job, error) {
	f.renewed++
	return f.active.Job, nil
}

func TestHeartbeatPersistsOnlyBoundedNormalizedCapabilitiesAndRenewsActiveLease(t *testing.T) {
	now := time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	value := ownedRun("worker-1")
	store := &fakeStore{active: value, hasActive: true}
	backend := Backend{Store: store, LeaseDuration: time.Minute, Now: func() time.Time { return now }}

	err := backend.Heartbeat(context.Background(), agent.Session{WorkerID: "worker-1"}, agent.Heartbeat{
		WorkerID:          "worker-1",
		CapabilityVersion: domain.WorkerCapabilityVersion,
		Capabilities: domain.WorkerCapabilities{
			Version:      domain.WorkerCapabilityVersion,
			OS:           " Linux ",
			Architecture: " AMD64 ",
			Runtimes:     []string{"Codex", "codex"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]domain.WorkerCapabilities
	if err := json.Unmarshal(store.heartbeat.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	c := metadata["capabilities"]
	if c.OS != "linux" || c.Architecture != "amd64" || len(c.Runtimes) != 1 || c.Runtimes[0] != "codex" {
		t.Fatalf("capabilities were not normalized: %#v", c)
	}
	if store.heartbeat.ID != "worker-1" || store.heartbeat.Capacity != 1 || store.renewed != 1 {
		t.Fatalf("unexpected heartbeat: worker=%#v renewed=%d", store.heartbeat, store.renewed)
	}
}

func TestHeartbeatRejectsUnsupportedOrOversizedCapabilities(t *testing.T) {
	store := &fakeStore{}
	backend := Backend{Store: store}
	session := agent.Session{WorkerID: "worker-1"}

	err := backend.Heartbeat(context.Background(), session, agent.Heartbeat{
		WorkerID:          "worker-1",
		CapabilityVersion: 99,
		Capabilities:      domain.WorkerCapabilities{Version: 99},
	})
	if err == nil {
		t.Fatal("unsupported capability version must fail closed")
	}

	err = backend.Heartbeat(context.Background(), session, agent.Heartbeat{
		WorkerID:          "worker-1",
		CapabilityVersion: domain.WorkerCapabilityVersion,
		Capabilities: domain.WorkerCapabilities{
			Version: domain.WorkerCapabilityVersion,
			Labels:  []string{string(make([]byte, maxCapabilityValue+1))},
		},
	})
	if err == nil {
		t.Fatal("oversized capability value must fail closed")
	}

	duplicates := make([]string, maxCapabilityItems+1)
	for i := range duplicates {
		duplicates[i] = "same"
	}
	err = backend.Heartbeat(context.Background(), session, agent.Heartbeat{
		WorkerID:          "worker-1",
		CapabilityVersion: domain.WorkerCapabilityVersion,
		Capabilities: domain.WorkerCapabilities{
			Version:  domain.WorkerCapabilityVersion,
			Runtimes: duplicates,
		},
	})
	if err == nil {
		t.Fatal("oversized raw capability list must fail closed before normalization")
	}
}

func TestLeaseDeliveryAndAckUseExactDurableIdentityWithoutCompletionMutation(t *testing.T) {
	value := ownedRun("worker-1")
	store := &fakeStore{active: value, hasActive: true, acquired: value}
	backend := Backend{Store: store}
	session := agent.Session{WorkerID: "worker-1"}

	delivery, ok, err := backend.NextLease(context.Background(), session)
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	if delivery.Identity.RunID != value.Run.ID || delivery.Identity.JobID != value.Job.ID || delivery.Identity.Revision != value.Job.Revision {
		t.Fatalf("wrong durable identity: %#v", delivery.Identity)
	}
	if err := backend.LeaseAcked(context.Background(), session, agent.LeaseAck{
		DeliveryID: delivery.DeliveryID,
		Identity:   delivery.Identity,
	}, true); err != nil {
		t.Fatal(err)
	}

	wrong := delivery.Identity
	wrong.RunID = "other"
	if err := backend.Status(context.Background(), session, agent.Status{Identity: wrong, State: "running"}); !errors.Is(err, ErrLeaseIdentityMismatch) {
		t.Fatalf("stale status error = %v", err)
	}
}

func ownedRun(workerID string) postgres.AgentRun {
	until := time.Now().UTC().Add(time.Hour)
	return postgres.AgentRun{
		Job: domain.Job{
			ID:         "job-1",
			Revision:   "abc123",
			Status:     domain.JobRunning,
			Attempt:    1,
			LeaseOwner: workerID,
			LeaseUntil: &until,
		},
		Run: postgres.Run{
			ID:      "run-1",
			JobID:   "job-1",
			Attempt: 1,
			Runtime: "agent",
			Status:  "running",
		},
	}
}
