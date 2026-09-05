package runtime

import (
	"context"
	"errors"
	"strings"
)

type BudgetOutcome string

const (
	BudgetContinue BudgetOutcome = "continue"
	BudgetFallback BudgetOutcome = "fallback"
	BudgetPark     BudgetOutcome = "park"
	BudgetEscalate BudgetOutcome = "escalate"
)

var (
	ErrBudgetExhausted        = errors.New("runtime budget exhausted")
	ErrBudgetApprovalRequired = errors.New("runtime budget approval required")
	ErrBudgetPolicyUnavailable = errors.New("runtime budget policy unavailable")
)

type BudgetRequest struct {
	Repository string
	WorkflowID string
	TaskID     string
	RunID      string
	Role       string
	Runtime    string
	Model      string
}

type BudgetDecision struct {
	Outcome BudgetOutcome
	Reason  string
}

type BudgetGate interface {
	Evaluate(ctx context.Context, request BudgetRequest) (BudgetDecision, error)
}

func budgetRequest(request Request, runtimeName, model string) BudgetRequest {
	return BudgetRequest{
		Repository: strings.TrimSpace(request.Repository),
		WorkflowID: strings.TrimSpace(request.Metadata["workflow_id"]),
		TaskID:     strings.TrimSpace(request.Metadata["task_id"]),
		RunID:      strings.TrimSpace(request.RunID),
		Role:       strings.TrimSpace(request.Role),
		Runtime:    strings.TrimSpace(runtimeName),
		Model:      strings.TrimSpace(model),
	}
}

func normalizeBudgetDecision(decision BudgetDecision) BudgetDecision {
	decision.Reason = strings.TrimSpace(decision.Reason)
	switch decision.Outcome {
	case BudgetContinue, BudgetFallback, BudgetPark, BudgetEscalate:
		return decision
	default:
		return BudgetDecision{Outcome: BudgetEscalate, Reason: "invalid budget decision"}
	}
}
