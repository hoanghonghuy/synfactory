package attention

import (
	"context"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type workflowRevalidatorStore struct {
	instance workflow.Instance
}

func (s workflowRevalidatorStore) GetWorkflow(context.Context, string) (workflow.Instance, error) {
	return s.instance, nil
}

func TestWorkflowRevalidatorRepairExhaustionClearsWhenWorkflowLeavesParkedState(t *testing.T) {
	r := WorkflowRevalidator{Store: workflowRevalidatorStore{instance: workflow.Instance{
		State:            workflow.StateReviewing,
		CIRepairAttempts: 2,
		CIRepairLimit:    2,
	}}}
	resolved, err := r.UnderlyingResolved(context.Background(), Item{WorkflowID: "wf-1", Kind: KindRepairExhausted})
	if err != nil {
		t.Fatal(err)
	}
	if !resolved {
		t.Fatal("repair exhaustion should clear after workflow leaves parked state")
	}
}

func TestWorkflowRevalidatorRepairExhaustionRemainsWhileParkedAndBudgetExhausted(t *testing.T) {
	r := WorkflowRevalidator{Store: workflowRevalidatorStore{instance: workflow.Instance{
		State:                workflow.StateParked,
		ReviewRepairAttempts: 2,
		ReviewRepairLimit:    2,
	}}}
	resolved, err := r.UnderlyingResolved(context.Background(), Item{WorkflowID: "wf-1", Kind: KindRepairExhausted})
	if err != nil {
		t.Fatal(err)
	}
	if resolved {
		t.Fatal("parked exhausted workflow must remain unresolved")
	}
}

func TestWorkflowRevalidatorFailsClosedForNonWorkflowSignals(t *testing.T) {
	r := WorkflowRevalidator{Store: workflowRevalidatorStore{instance: workflow.Instance{State: workflow.StateCompleted}}}
	resolved, err := r.UnderlyingResolved(context.Background(), Item{WorkflowID: "wf-1", Kind: KindCredential})
	if err != nil {
		t.Fatal(err)
	}
	if resolved {
		t.Fatal("credential blocker cannot be cleared from workflow state alone")
	}
}

func TestWorkflowRevalidatorReleaseBlockerRequiresCompletion(t *testing.T) {
	r := WorkflowRevalidator{Store: workflowRevalidatorStore{instance: workflow.Instance{State: workflow.StateMergeReady}}}
	resolved, err := r.UnderlyingResolved(context.Background(), Item{WorkflowID: "wf-1", Kind: KindReleaseBlocker})
	if err != nil {
		t.Fatal(err)
	}
	if resolved {
		t.Fatal("release blocker must remain until workflow is completed")
	}

	r.Store = workflowRevalidatorStore{instance: workflow.Instance{State: workflow.StateCompleted}}
	resolved, err = r.UnderlyingResolved(context.Background(), Item{WorkflowID: "wf-1", Kind: KindReleaseBlocker})
	if err != nil {
		t.Fatal(err)
	}
	if !resolved {
		t.Fatal("completed workflow should clear release blocker")
	}
}
