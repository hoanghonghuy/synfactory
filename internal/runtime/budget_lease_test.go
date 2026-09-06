package runtime

import (
	"context"
	"testing"
)

type leasingBudgetGateStub struct {
	released bool
}

func (g *leasingBudgetGateStub) Evaluate(context.Context, BudgetRequest) (BudgetDecision, error) {
	return BudgetDecision{Outcome: BudgetContinue}, nil
}

func (g *leasingBudgetGateStub) Acquire(context.Context, BudgetRequest) (BudgetDecision, func(), error) {
	return BudgetDecision{Outcome: BudgetContinue}, func() { g.released = true }, nil
}

type leaseAwareObserver struct {
	t    *testing.T
	gate *leasingBudgetGateStub
}

func (o leaseAwareObserver) AttemptStarted(context.Context, Attempt) error {
	if o.gate.released {
		o.t.Fatal("budget lease released before runtime attempt started")
	}
	return nil
}

func (o leaseAwareObserver) AttemptFinished(context.Context, Attempt) error {
	if o.gate.released {
		o.t.Fatal("budget lease released before durable accounting finished")
	}
	return nil
}

func TestRegistryHoldsBudgetLeaseThroughObserverAccounting(t *testing.T) {
	adapter := &fakeAdapter{name: "premium", result: Result{Outcome: OutcomeSucceeded}}
	gate := &leasingBudgetGateStub{}
	registry := governedTestRegistry(adapter, &fakeAdapter{name: "economy"}, gate)

	_, _, err := registry.Execute(context.Background(), Request{
		Repository: "owner/repo",
		Role:       "developer",
		RunID:      "run-lease",
		Metadata:   map[string]string{"workflow_id": "wf-1", "task_id": "task-1"},
	}, leaseAwareObserver{t: t, gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	if !gate.released {
		t.Fatal("budget lease was not released after accounting")
	}
}
