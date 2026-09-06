package runtime

import (
	"context"
	"errors"
	"testing"
)

type nonExecutionBudgetGateStub struct {
	released []BudgetRequest
}

func (g *nonExecutionBudgetGateStub) Evaluate(context.Context, BudgetRequest) (BudgetDecision, error) {
	return BudgetDecision{Outcome: BudgetContinue}, nil
}

func (g *nonExecutionBudgetGateStub) Acquire(_ context.Context, _ BudgetRequest) (BudgetDecision, func(), error) {
	return BudgetDecision{Outcome: BudgetContinue}, func() {}, nil
}

func (g *nonExecutionBudgetGateStub) ReleaseNonExecuted(_ context.Context, request BudgetRequest) error {
	g.released = append(g.released, request)
	return nil
}

func TestRegistryReleasesReservationAfterProbeFailure(t *testing.T) {
	first := &fakeAdapter{name: "first", probeErr: Failure(FailureUnavailable, ErrRuntimeUnavailable)}
	second := &fakeAdapter{name: "second", result: Result{Outcome: OutcomeSucceeded}}
	gate := &nonExecutionBudgetGateStub{}
	registry := (&Registry{
		adapters: map[string]Adapter{"first": first, "second": second},
		config: Config{
			Runtimes: map[string]RuntimeConfig{
				"first":  {Kind: ProviderOpenAI, Model: "first-model"},
				"second": {Kind: ProviderOpenAI, Model: "second-model"},
			},
			Roles: map[string]RoleConfig{"developer": {Chain: []CandidateConfig{{Runtime: "first"}, {Runtime: "second"}}}},
		},
	}).WithBudgetGate(gate)

	_, _, err := registry.Execute(context.Background(), Request{Repository: "owner/repo", RunID: "job-1", Role: "developer", Metadata: map[string]string{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(gate.released) != 1 {
		t.Fatalf("released reservations = %d, want 1", len(gate.released))
	}
	if got := gate.released[0]; got.Runtime != "first" || got.RunID != "job-1.1" {
		t.Fatalf("released wrong reservation identity: %+v", got)
	}
}

func TestRegistryDoesNotReleaseReservationAfterProviderFailure(t *testing.T) {
	adapter := &fakeAdapter{name: "only", result: Result{Outcome: OutcomeFailed}, runErr: Failure(FailurePermanent, errors.New("provider failed"))}
	gate := &nonExecutionBudgetGateStub{}
	registry := (&Registry{
		adapters: map[string]Adapter{"only": adapter},
		config: Config{
			Runtimes: map[string]RuntimeConfig{"only": {Kind: ProviderOpenAI, Model: "model"}},
			Roles:    map[string]RoleConfig{"developer": {Chain: []CandidateConfig{{Runtime: "only"}}}},
		},
	}).WithBudgetGate(gate)

	_, _, err := registry.Execute(context.Background(), Request{Repository: "owner/repo", RunID: "job-2", Role: "developer", Metadata: map[string]string{}}, nil)
	if err == nil {
		t.Fatal("expected provider failure")
	}
	if len(gate.released) != 0 {
		t.Fatalf("post-provider failure released %d reservations, want 0", len(gate.released))
	}
}
