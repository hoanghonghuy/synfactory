package attention

import (
	"context"
	"fmt"

	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type WorkflowStore interface {
	GetWorkflow(context.Context, string) (workflow.Instance, error)
}

type WorkflowRevalidator struct {
	Store WorkflowStore
}

func (r WorkflowRevalidator) UnderlyingResolved(ctx context.Context, item Item) (bool, error) {
	if r.Store == nil {
		return false, fmt.Errorf("workflow store is required")
	}
	if item.WorkflowID == "" {
		return false, nil
	}
	instance, err := r.Store.GetWorkflow(ctx, item.WorkflowID)
	if err != nil {
		return false, fmt.Errorf("load workflow %s: %w", item.WorkflowID, err)
	}

	switch item.Kind {
	case KindRepairExhausted:
		ciExhausted := instance.CIRepairLimit > 0 && instance.CIRepairAttempts >= instance.CIRepairLimit
		reviewExhausted := instance.ReviewRepairLimit > 0 && instance.ReviewRepairAttempts >= instance.ReviewRepairLimit
		return instance.State != workflow.StateParked || (!ciExhausted && !reviewExhausted), nil
	case KindProductDecision:
		return instance.State != workflow.StateBlocked && instance.State != workflow.StateParked, nil
	case KindReleaseBlocker, KindSecurityBlocker:
		return instance.State == workflow.StateCompleted, nil
	default:
		// Credential and fleet signals require their own authoritative sources.
		// A workflow transition alone cannot prove those blockers cleared.
		return false, nil
	}
}
