package runtime

import (
	"context"
	"errors"
	"testing"
)

type fakeAdapter struct {
	name      string
	probeErr  error
	result    Result
	runErr    error
	runCount  int
	resumeCnt int
}

func (f *fakeAdapter) Name() string                { return f.name }
func (f *fakeAdapter) Probe(context.Context) error { return f.probeErr }
func (f *fakeAdapter) Run(context.Context, Request) (Result, error) {
	f.runCount++
	return f.result, f.runErr
}
func (f *fakeAdapter) Resume(context.Context, string, Request) (Result, error) {
	f.resumeCnt++
	return f.result, f.runErr
}
func (f *fakeAdapter) Cancel(context.Context, string) error { return nil }

type captureObserver struct {
	started  []Attempt
	finished []Attempt
}

func (o *captureObserver) AttemptStarted(_ context.Context, attempt Attempt) error {
	o.started = append(o.started, attempt)
	return nil
}
func (o *captureObserver) AttemptFinished(_ context.Context, attempt Attempt) error {
	o.finished = append(o.finished, attempt)
	return nil
}

func TestRegistryFallsBackOnUnavailable(t *testing.T) {
	first := &fakeAdapter{name: "first", probeErr: Failure(FailureUnavailable, ErrRuntimeUnavailable)}
	second := &fakeAdapter{name: "second", result: Result{Outcome: OutcomeSucceeded, Summary: "ok"}}
	registry := &Registry{
		adapters: map[string]Adapter{"first": first, "second": second},
		config: Config{
			Runtimes: map[string]RuntimeConfig{"first": {Kind: ProviderCodex}, "second": {Kind: ProviderClaude}},
			Roles:    map[string]RoleConfig{"developer": {Chain: []CandidateConfig{{Runtime: "first"}, {Runtime: "second"}}}},
		},
	}
	observer := &captureObserver{}
	result, attempts, err := registry.Execute(context.Background(), Request{RunID: "job-1", Role: "developer", Metadata: map[string]string{}}, observer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "ok" || len(attempts) != 2 || second.runCount != 1 {
		t.Fatalf("unexpected fallback result=%+v attempts=%+v", result, attempts)
	}
	if len(observer.started) != 2 || len(observer.finished) != 2 {
		t.Fatalf("observer mismatch: started=%d finished=%d", len(observer.started), len(observer.finished))
	}
}

func TestRegistryDoesNotFallbackOnPermanentFailure(t *testing.T) {
	first := &fakeAdapter{name: "first", result: Result{Outcome: OutcomeFailed}, runErr: Failure(FailurePermanent, errors.New("bad request"))}
	second := &fakeAdapter{name: "second", result: Result{Outcome: OutcomeSucceeded}}
	registry := &Registry{
		adapters: map[string]Adapter{"first": first, "second": second},
		config: Config{
			Runtimes: map[string]RuntimeConfig{"first": {Kind: ProviderCodex}, "second": {Kind: ProviderClaude}},
			Roles:    map[string]RoleConfig{"reviewer": {Chain: []CandidateConfig{{Runtime: "first"}, {Runtime: "second"}}}},
		},
	}
	_, attempts, err := registry.Execute(context.Background(), Request{RunID: "job-2", Role: "reviewer", Metadata: map[string]string{}}, nil)
	if err == nil || len(attempts) != 1 || second.runCount != 0 {
		t.Fatalf("expected permanent stop, err=%v attempts=%d secondRuns=%d", err, len(attempts), second.runCount)
	}
}

func TestRegistryUsesResumeSessionMetadata(t *testing.T) {
	adapter := &fakeAdapter{name: "only", result: Result{Outcome: OutcomeSucceeded, SessionID: "next"}}
	registry := &Registry{
		adapters: map[string]Adapter{"only": adapter},
		config: Config{
			Runtimes: map[string]RuntimeConfig{"only": {Kind: ProviderAntigravity}},
			Roles:    map[string]RoleConfig{"pm": {Chain: []CandidateConfig{{Runtime: "only"}}}},
		},
	}
	_, _, err := registry.Execute(context.Background(), Request{RunID: "job-3", Role: "pm", Metadata: map[string]string{"resume_session_id": "old"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.resumeCnt != 1 || adapter.runCount != 0 {
		t.Fatalf("expected resume, resume=%d run=%d", adapter.resumeCnt, adapter.runCount)
	}
}
