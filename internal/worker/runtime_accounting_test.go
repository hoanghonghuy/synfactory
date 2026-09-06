package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	runtimefactory "github.com/hoanghonghuy/synfactory/internal/runtime"
)

type accountingStore struct {
	fakeStore
	pricing    postgres.RuntimePricing
	pricingErr error
	recordErr  error
	usage      []postgres.RuntimeUsage
}

func (s *accountingStore) ResolveRuntimePricing(context.Context, string, string, time.Time) (postgres.RuntimePricing, error) {
	if s.pricingErr != nil {
		return postgres.RuntimePricing{}, s.pricingErr
	}
	return s.pricing, nil
}

func (s *accountingStore) RecordRuntimeUsage(_ context.Context, usage postgres.RuntimeUsage) error {
	if s.recordErr != nil {
		return s.recordErr
	}
	s.usage = append(s.usage, usage)
	return nil
}

func TestRunObserverRecordsAttributedUsageWithHistoricalPricing(t *testing.T) {
	finished := time.Date(2026, 9, 6, 5, 0, 0, 0, time.UTC)
	store := &accountingStore{pricing: postgres.RuntimePricing{
		Version: "openai-gpt-5-v1", Provider: "openai", Model: "gpt-5", EffectiveAt: finished.Add(-time.Hour),
	}}
	observer := &runObserver{
		store: store,
		job: domain.Job{
			ID: "job-35", Attempt: 2, Role: domain.RoleDev, Subject: "35",
			Metadata: json.RawMessage(`{"workflow_id":"workflow-35","task_id":"task-35"}`),
		},
		repository: postgres.Repository{FullName: "owner/repo"},
		now:        func() time.Time { return finished },
	}
	attempt := runtimefactory.Attempt{
		Sequence: 1, Runtime: "primary", Provider: "openai", Model: "gpt-5",
		Result: runtimefactory.Result{
			Outcome: runtimefactory.OutcomeSucceeded, ExitCode: 0, FinishedAt: finished,
			Usage: runtimefactory.Usage{RequestCount: 1, InputTokens: 1200, OutputTokens: 300, RuntimeMS: 2500},
		},
	}
	if err := observer.AttemptStarted(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := observer.AttemptFinished(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if len(store.usage) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(store.usage))
	}
	got := store.usage[0]
	if got.Repository != "owner/repo" || got.WorkflowID != "workflow-35" || got.TaskID != "task-35" {
		t.Fatalf("unexpected attribution: %+v", got)
	}
	if got.Provider != "openai" || got.Model != "gpt-5" || got.PricingVersion != "openai-gpt-5-v1" {
		t.Fatalf("unexpected provider/pricing attribution: %+v", got)
	}
	if got.RequestCount != 1 || got.InputTokens != 1200 || got.OutputTokens != 300 || got.RuntimeMS != 2500 {
		t.Fatalf("unexpected normalized usage: %+v", got)
	}
}

func TestRunObserverAccountingFailureDoesNotReplaySuccessfulAttempt(t *testing.T) {
	finished := time.Date(2026, 9, 6, 5, 0, 0, 0, time.UTC)
	store := &accountingStore{pricingErr: errors.New("pricing temporarily unavailable")}
	observer := &runObserver{
		store: store,
		job: domain.Job{ID: "job-35", Attempt: 1, Role: domain.RoleDev, Subject: "35"},
		repository: postgres.Repository{FullName: "owner/repo"},
		now:        func() time.Time { return finished },
	}
	attempt := runtimefactory.Attempt{
		Sequence: 1, Runtime: "primary", Provider: "openai", Model: "gpt-5",
		Result: runtimefactory.Result{
			Outcome: runtimefactory.OutcomeSucceeded, ExitCode: 0, FinishedAt: finished,
			Usage: runtimefactory.Usage{RequestCount: 1, RuntimeMS: 100},
		},
	}
	if err := observer.AttemptStarted(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := observer.AttemptFinished(context.Background(), attempt); err != nil {
		t.Fatalf("accounting failure propagated into runtime result: %v", err)
	}
	if len(store.finished) != 1 || len(store.evidence) != 1 {
		t.Fatalf("core run persistence was not completed: finished=%v evidence=%d", store.finished, len(store.evidence))
	}
	if len(store.usage) != 0 {
		t.Fatalf("usage unexpectedly recorded after pricing failure: %+v", store.usage)
	}
}
