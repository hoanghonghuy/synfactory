package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	runtimefactory "github.com/hoanghonghuy/synfactory/internal/runtime"
)

type fakeStore struct {
	mu         sync.Mutex
	job        domain.Job
	repository postgres.Repository
	runs       []postgres.Run
	finished   []string
	evidence   []postgres.Evidence
	succeeded  bool
	failed     bool
	renewErr   error
}

func (s *fakeStore) ClaimJob(context.Context, string, time.Time, time.Duration) (domain.Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.ID == "" {
		return domain.Job{}, false, nil
	}
	job := s.job
	s.job.ID = ""
	job.Status = domain.JobLeased
	return job, true, nil
}
func (s *fakeStore) StartJob(_ context.Context, jobID, workerID string, now time.Time) (domain.Job, error) {
	job := domain.Job{ID: jobID, RepositoryID: "repo-1", Role: "developer", Status: domain.JobRunning, Attempt: 1, MaxAttempts: 3, LeaseOwner: workerID}
	until := now.Add(time.Minute)
	job.LeaseUntil = &until
	return job, nil
}
func (s *fakeStore) RenewLease(context.Context, string, string, time.Time, time.Duration) (domain.Job, error) {
	return domain.Job{}, s.renewErr
}
func (s *fakeStore) SucceedJob(context.Context, string, string, time.Time) (domain.Job, error) {
	s.succeeded = true
	return domain.Job{Status: domain.JobSucceeded}, nil
}
func (s *fakeStore) FailJob(context.Context, string, string, time.Time, time.Time, string) (domain.Job, error) {
	s.failed = true
	return domain.Job{Status: domain.JobRetryWait}, nil
}
func (s *fakeStore) GetRepository(context.Context, string) (postgres.Repository, error) {
	return s.repository, nil
}
func (s *fakeStore) CreateRun(_ context.Context, run postgres.Run) (postgres.Run, error) {
	s.runs = append(s.runs, run)
	return run, nil
}
func (s *fakeStore) FinishRun(_ context.Context, runID, status string, _ time.Time, _ *int, _ string, _ string) (postgres.Run, error) {
	s.finished = append(s.finished, runID+":"+status)
	return postgres.Run{ID: runID, Status: status}, nil
}
func (s *fakeStore) AddEvidence(_ context.Context, evidence postgres.Evidence) (postgres.Evidence, error) {
	s.evidence = append(s.evidence, evidence)
	return evidence, nil
}
func (s *fakeStore) HeartbeatWorker(context.Context, postgres.Worker, time.Time) (postgres.Worker, error) {
	return postgres.Worker{}, nil
}

type fakeEngine struct {
	result   runtimefactory.Result
	attempts []runtimefactory.Attempt
	err      error
}

func (e fakeEngine) Execute(ctx context.Context, request runtimefactory.Request, observer runtimefactory.Observer) (runtimefactory.Result, []runtimefactory.Attempt, error) {
	for _, attempt := range e.attempts {
		if err := observer.AttemptStarted(ctx, attempt); err != nil {
			return runtimefactory.Result{}, nil, err
		}
		if err := observer.AttemptFinished(ctx, attempt); err != nil {
			return runtimefactory.Result{}, nil, err
		}
	}
	return e.result, e.attempts, e.err
}

type fakeBuilder struct{}

func (fakeBuilder) Build(context.Context, domain.Job, postgres.Repository) (runtimefactory.Request, error) {
	return runtimefactory.Request{Prompt: "do work", Workspace: "/tmp", Metadata: map[string]string{}}, nil
}

func TestRunOnePersistsRuntimeFallbackAttemptsAndSucceeds(t *testing.T) {
	store := &fakeStore{
		job:        domain.Job{ID: "job-1", RepositoryID: "repo-1", Role: "developer"},
		repository: postgres.Repository{ID: "repo-1", FullName: "owner/repo"},
	}
	attempts := []runtimefactory.Attempt{
		{Sequence: 1, Runtime: "codex", Model: "m1", FailureClass: runtimefactory.FailureUnavailable, Result: runtimefactory.Result{Outcome: runtimefactory.OutcomeUnavailable, ExitCode: -1}, Err: runtimefactory.Failure(runtimefactory.FailureUnavailable, runtimefactory.ErrRuntimeUnavailable)},
		{Sequence: 2, Runtime: "cursor", Model: "m2", Result: runtimefactory.Result{Outcome: runtimefactory.OutcomeSucceeded, ExitCode: 0, Summary: "done"}},
	}
	w := New(store, fakeEngine{result: attempts[1].Result, attempts: attempts}, fakeBuilder{}, Config{ID: "w1", LeaseDuration: time.Minute})
	worked, err := w.RunOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !worked || !store.succeeded || store.failed {
		t.Fatalf("unexpected job outcome worked=%v success=%v failed=%v", worked, store.succeeded, store.failed)
	}
	if len(store.runs) != 2 || store.runs[0].Sequence != 1 || store.runs[1].Sequence != 2 {
		t.Fatalf("unexpected persisted runs: %+v", store.runs)
	}
	if len(store.finished) != 2 || len(store.evidence) != 2 {
		t.Fatalf("expected finished/evidence per attempt: finished=%v evidence=%d", store.finished, len(store.evidence))
	}
}

func TestRunOneSchedulesRetryOnRuntimeFailure(t *testing.T) {
	store := &fakeStore{
		job:        domain.Job{ID: "job-2", RepositoryID: "repo-1", Role: "reviewer"},
		repository: postgres.Repository{ID: "repo-1", FullName: "owner/repo"},
	}
	failure := errors.New("provider failed")
	attempt := runtimefactory.Attempt{Sequence: 1, Runtime: "codex", Result: runtimefactory.Result{Outcome: runtimefactory.OutcomeFailed, ExitCode: 1}, Err: runtimefactory.Failure(runtimefactory.FailurePermanent, failure)}
	w := New(store, fakeEngine{result: attempt.Result, attempts: []runtimefactory.Attempt{attempt}, err: attempt.Err}, fakeBuilder{}, Config{ID: "w1", LeaseDuration: time.Minute})
	worked, err := w.RunOne(context.Background())
	if !worked || err == nil || !store.failed || store.succeeded {
		t.Fatalf("expected retry failure worked=%v err=%v failed=%v success=%v", worked, err, store.failed, store.succeeded)
	}
}
