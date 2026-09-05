package runtime

import (
	"context"
	"errors"
	"testing"
)

type budgetGateStub struct {
	decisions map[string]BudgetDecision
	err       error
	requests  []BudgetRequest
}

func (g *budgetGateStub) Evaluate(_ context.Context, request BudgetRequest) (BudgetDecision, error) {
	g.requests = append(g.requests, request)
	if g.err != nil {
		return BudgetDecision{}, g.err
	}
	if decision, ok := g.decisions[request.Runtime]; ok {
		return decision, nil
	}
	return BudgetDecision{Outcome: BudgetContinue}, nil
}

func governedTestRegistry(first, second *fakeAdapter, gate BudgetGate) *Registry {
	return (&Registry{
		adapters: map[string]Adapter{"premium": first, "economy": second},
		config: Config{
			Runtimes: map[string]RuntimeConfig{
				"premium": {Kind: ProviderOpenAI, Model: "premium-model"},
				"economy": {Kind: ProviderOpenAI, Model: "economy-model"},
			},
			Roles: map[string]RoleConfig{
				"developer": {Chain: []CandidateConfig{{Runtime: "premium"}, {Runtime: "economy"}}},
			},
		},
	}).WithBudgetGate(gate)
}

func TestRegistryBudgetFallbackSkipsDeniedRuntime(t *testing.T) {
	premium := &fakeAdapter{name: "premium", result: Result{Outcome: OutcomeSucceeded}}
	economy := &fakeAdapter{name: "economy", result: Result{Outcome: OutcomeSucceeded, Summary: "economy"}}
	gate := &budgetGateStub{decisions: map[string]BudgetDecision{
		"premium": {Outcome: BudgetFallback, Reason: "repository daily soft budget"},
	}}
	registry := governedTestRegistry(premium, economy, gate)
	result, attempts, err := registry.Execute(context.Background(), Request{
		Repository: "owner/repo",
		Role:       "developer",
		RunID:      "run-7",
		Metadata:   map[string]string{"workflow_id": "wf-2", "task_id": "task-9"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "economy" || premium.runCount != 0 || economy.runCount != 1 {
		t.Fatalf("unexpected routing result=%+v premium=%d economy=%d", result, premium.runCount, economy.runCount)
	}
	if len(attempts) != 2 || attempts[0].FailureClass != FailureBudget || !errors.Is(attempts[0].Err, ErrBudgetExhausted) {
		t.Fatalf("unexpected budget attempts: %+v", attempts)
	}
	if len(gate.requests) != 2 || gate.requests[0].Repository != "owner/repo" || gate.requests[0].WorkflowID != "wf-2" || gate.requests[0].TaskID != "task-9" {
		t.Fatalf("missing attribution in budget requests: %+v", gate.requests)
	}
}

func TestRegistryBudgetParkStopsWithoutExecutingAdapter(t *testing.T) {
	premium := &fakeAdapter{name: "premium", result: Result{Outcome: OutcomeSucceeded}}
	economy := &fakeAdapter{name: "economy", result: Result{Outcome: OutcomeSucceeded}}
	gate := &budgetGateStub{decisions: map[string]BudgetDecision{
		"premium": {Outcome: BudgetPark, Reason: "workflow hard budget exhausted"},
	}}
	registry := governedTestRegistry(premium, economy, gate)
	_, attempts, err := registry.Execute(context.Background(), Request{Role: "developer", RunID: "run-8", Metadata: map[string]string{}}, nil)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err=%v, want budget exhausted", err)
	}
	if premium.runCount != 0 || economy.runCount != 0 || len(attempts) != 1 || attempts[0].FailureClass != FailureBudget {
		t.Fatalf("budget park executed runtime: attempts=%+v premium=%d economy=%d", attempts, premium.runCount, economy.runCount)
	}
}

func TestRegistryBudgetGateFailureFailsClosed(t *testing.T) {
	premium := &fakeAdapter{name: "premium", result: Result{Outcome: OutcomeSucceeded}}
	economy := &fakeAdapter{name: "economy", result: Result{Outcome: OutcomeSucceeded}}
	registry := governedTestRegistry(premium, economy, &budgetGateStub{err: errors.New("ledger unavailable")})
	_, attempts, err := registry.Execute(context.Background(), Request{Role: "developer", RunID: "run-9", Metadata: map[string]string{}}, nil)
	if !errors.Is(err, ErrBudgetPolicyUnavailable) {
		t.Fatalf("err=%v, want budget policy unavailable", err)
	}
	if len(attempts) != 0 || premium.runCount != 0 || economy.runCount != 0 {
		t.Fatalf("policy failure must execute nothing: attempts=%+v premium=%d economy=%d", attempts, premium.runCount, economy.runCount)
	}
}
