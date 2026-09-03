package github

import (
	"context"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type reconcileMemoryAPI struct{}

func (reconcileMemoryAPI) ListOpenIssues(context.Context, string, string) ([]Issue, error) {
	return []Issue{{Number: 7, UpdatedAt: "2026-09-03T08:00:00Z"}}, nil
}

func (reconcileMemoryAPI) ListOpenPulls(context.Context, string, string) ([]PullRequest, error) {
	var pull PullRequest
	pull.Number = 55
	pull.UpdatedAt = "2026-09-03T08:01:00Z"
	pull.Head.SHA = "abc123"
	return []PullRequest{pull}, nil
}

func (reconcileMemoryAPI) ListReviews(context.Context, string, string, int64) ([]Review, error) {
	return []Review{{ID: 3, State: "APPROVED", SubmittedAt: "2026-09-03T08:02:00Z"}}, nil
}

func (reconcileMemoryAPI) ListCheckRuns(context.Context, string, string, string) ([]CheckRun, error) {
	return []CheckRun{{ID: 9, Status: "completed", Conclusion: "success", HeadSHA: "abc123", UpdatedAt: "2026-09-03T08:03:00Z"}}, nil
}

func (reconcileMemoryAPI) GetBranch(context.Context, string, string, string) (Branch, error) {
	var branch Branch
	branch.Name = "develop"
	branch.Commit.SHA = "def456"
	return branch, nil
}

type reconcileMemoryStore struct {
	repositories []postgres.Repository
	events       []postgres.InboxEvent
	dedupe       map[string]struct{}
	state        postgres.ReconcileState
}

func (s *reconcileMemoryStore) ListRepositories(context.Context) ([]postgres.Repository, error) {
	return s.repositories, nil
}

func (s *reconcileMemoryStore) PutEvent(_ context.Context, event postgres.InboxEvent) (postgres.InboxEvent, bool, error) {
	if s.dedupe == nil {
		s.dedupe = map[string]struct{}{}
	}
	if _, exists := s.dedupe[event.DedupeKey]; exists {
		return event, false, nil
	}
	s.dedupe[event.DedupeKey] = struct{}{}
	s.events = append(s.events, event)
	return event, true, nil
}

func (s *reconcileMemoryStore) PutReconcileState(_ context.Context, state postgres.ReconcileState) (postgres.ReconcileState, error) {
	s.state = state
	return state, nil
}

func TestReconcilerEmitsCanonicalEventsWithoutDuplicatingSweep(t *testing.T) {
	store := &reconcileMemoryStore{repositories: []postgres.Repository{{
		ID:            "github:42",
		Provider:      "github",
		FullName:      "owner/repo",
		DefaultBranch: "develop",
		Enabled:       true,
	}}}
	wakes := 0
	reconciler := NewReconciler(reconcileMemoryAPI{}, store, time.Hour, func() { wakes++ })
	now := time.Unix(123, 0).UTC()
	reconciler.now = func() time.Time { return now }

	if err := reconciler.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 5 {
		t.Fatalf("expected issue, PR, review, check and branch events; got %d", len(store.events))
	}
	if wakes != 5 {
		t.Fatalf("expected five wakes, got %d", wakes)
	}

	if err := reconciler.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 5 || wakes != 5 {
		t.Fatalf("second sweep must dedupe: events=%d wakes=%d", len(store.events), wakes)
	}
	if store.state.LastFullReconcileAt == nil || !store.state.LastFullReconcileAt.Equal(now) {
		t.Fatalf("reconcile watermark not persisted: %+v", store.state)
	}
}
