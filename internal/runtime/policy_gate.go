package runtime

import (
	"context"
	"errors"
	"strings"
)

// BudgetSnapshot is a server-owned view of the configured budget state for one
// candidate runtime. Callers may identify a run, but cannot self-authorize an
// override or supply spent/projected cost as policy truth.
type BudgetSnapshot struct {
	HardExceeded       bool
	SoftExceeded       bool
	OverrideAuthorized bool
	SoftOutcome        BudgetOutcome
	Reason             string
}

// BudgetSnapshotReader resolves scoped repository/role/provider/workflow budget
// state from trusted policy and usage data.
type BudgetSnapshotReader interface {
	BudgetSnapshot(ctx context.Context, request BudgetRequest) (BudgetSnapshot, error)
}

type LedgerBudgetGate struct {
	Reader BudgetSnapshotReader
}

func (g LedgerBudgetGate) Evaluate(ctx context.Context, request BudgetRequest) (BudgetDecision, error) {
	if g.Reader == nil {
		return BudgetDecision{}, ErrBudgetPolicyUnavailable
	}
	snapshot, err := g.Reader.BudgetSnapshot(ctx, request)
	if err != nil {
		return BudgetDecision{}, err
	}
	snapshot.Reason = strings.TrimSpace(snapshot.Reason)

	if snapshot.HardExceeded && !snapshot.OverrideAuthorized {
		return BudgetDecision{Outcome: BudgetEscalate, Reason: budgetReason(snapshot.Reason, "hard budget exceeded")}, nil
	}
	if snapshot.SoftExceeded {
		outcome := snapshot.SoftOutcome
		if outcome == "" {
			outcome = BudgetFallback
		}
		switch outcome {
		case BudgetFallback, BudgetPark, BudgetEscalate:
			return BudgetDecision{Outcome: outcome, Reason: budgetReason(snapshot.Reason, "soft budget exceeded")}, nil
		default:
			return BudgetDecision{}, errors.New("invalid soft budget outcome")
		}
	}
	return BudgetDecision{Outcome: BudgetContinue, Reason: snapshot.Reason}, nil
}

func budgetReason(reason, fallback string) string {
	if reason != "" {
		return reason
	}
	return fallback
}
