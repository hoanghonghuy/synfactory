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

// BudgetSnapshotLeaser optionally serializes hard-budget admission for the
// lifetime of one runtime attempt. The returned release function must be called
// after durable usage accounting has finished so another worker cannot evaluate
// the same hard cap against stale spend.
type BudgetSnapshotLeaser interface {
	AcquireBudgetSnapshot(ctx context.Context, request BudgetRequest) (BudgetSnapshot, func(), error)
}

// BudgetLeaseGate is implemented by gates that can keep hard-budget admission
// serialized until the caller releases the attempt lease.
type BudgetLeaseGate interface {
	Acquire(ctx context.Context, request BudgetRequest) (BudgetDecision, func(), error)
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
	return budgetDecisionFromSnapshot(snapshot)
}

func (g LedgerBudgetGate) Acquire(ctx context.Context, request BudgetRequest) (BudgetDecision, func(), error) {
	if g.Reader == nil {
		return BudgetDecision{}, nil, ErrBudgetPolicyUnavailable
	}
	leaser, ok := g.Reader.(BudgetSnapshotLeaser)
	if !ok {
		decision, err := g.Evaluate(ctx, request)
		return decision, func() {}, err
	}
	snapshot, release, err := leaser.AcquireBudgetSnapshot(ctx, request)
	if err != nil {
		return BudgetDecision{}, nil, err
	}
	if release == nil {
		release = func() {}
	}
	decision, err := budgetDecisionFromSnapshot(snapshot)
	if err != nil {
		release()
		return BudgetDecision{}, nil, err
	}
	return decision, release, nil
}

func budgetDecisionFromSnapshot(snapshot BudgetSnapshot) (BudgetDecision, error) {
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
