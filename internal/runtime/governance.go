package runtime

import (
	"context"
	"errors"
	"math"
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
	ErrBudgetExhausted         = errors.New("runtime budget exhausted")
	ErrBudgetApprovalRequired  = errors.New("runtime budget approval required")
	ErrBudgetPolicyUnavailable = errors.New("runtime budget policy unavailable")
)

type BudgetRequest struct {
	Repository       string
	WorkflowID       string
	TaskID           string
	RunID            string
	Role             string
	Runtime          string
	Provider         string
	Model            string
	InputTokenLimit  int64
	OutputTokenLimit int64
}

type BudgetDecision struct {
	Outcome BudgetOutcome
	Reason  string
}

type BudgetGate interface {
	Evaluate(ctx context.Context, request BudgetRequest) (BudgetDecision, error)
}

func budgetRequest(request Request, runtimeName, provider, model string, runtimeCfg RuntimeConfig) BudgetRequest {
	return BudgetRequest{
		Repository:       strings.TrimSpace(request.Repository),
		WorkflowID:       strings.TrimSpace(request.Metadata["workflow_id"]),
		TaskID:           strings.TrimSpace(request.Metadata["task_id"]),
		RunID:            budgetRunID(request),
		Role:             strings.TrimSpace(request.Role),
		Runtime:          strings.TrimSpace(runtimeName),
		Provider:         strings.TrimSpace(provider),
		Model:            strings.TrimSpace(model),
		InputTokenLimit:  conservativeBudgetTokenLimit(runtimeCfg.BudgetInputTokenLimit),
		OutputTokenLimit: conservativeBudgetTokenLimit(runtimeCfg.BudgetOutputTokenLimit),
	}
}

// A zero config value means the operator has not supplied a trustworthy upper
// bound. Keep ordinary, unbudgeted routes backward-compatible, but make any
// priced budget projection conservative: a zero-rate pricing dimension still
// contributes zero, while a positive rate either overflows projection or yields
// a deliberately huge reservation instead of silently under-reserving spend.
func conservativeBudgetTokenLimit(configured int64) int64 {
	if configured > 0 {
		return configured
	}
	return math.MaxInt64
}

func budgetRunID(request Request) string {
	runID := strings.TrimSpace(request.RunID)
	attempt := strings.TrimSpace(request.Metadata["job_attempt"])
	jobID := strings.TrimSpace(request.Metadata["job_id"])
	if runID == "" || attempt == "" || jobID == "" {
		return runID
	}
	// Registry attempt scoping has already appended the runtime sequence to RunID.
	// Add the durable job attempt so retries of the same job cannot collide with
	// an unresolved reservation from an earlier execution attempt.
	return runID + ".attempt-" + attempt
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
