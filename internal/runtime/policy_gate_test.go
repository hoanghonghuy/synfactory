package runtime

import (
	"context"
	"errors"
	"testing"
)

type budgetSnapshotReaderStub struct {
	snapshot BudgetSnapshot
	err      error
}

func (s budgetSnapshotReaderStub) BudgetSnapshot(_ context.Context, _ BudgetRequest) (BudgetSnapshot, error) {
	return s.snapshot, s.err
}

func TestLedgerBudgetGateBlocksHardBudgetWithoutAuthorizedOverride(t *testing.T) {
	gate := LedgerBudgetGate{Reader: budgetSnapshotReaderStub{snapshot: BudgetSnapshot{
		HardExceeded: true,
		Reason:       "repository daily hard budget",
	}}}
	decision, err := gate.Evaluate(context.Background(), BudgetRequest{Repository: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != BudgetEscalate || decision.Reason != "repository daily hard budget" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestLedgerBudgetGateAllowsExplicitAuthorizedHardBudgetOverride(t *testing.T) {
	gate := LedgerBudgetGate{Reader: budgetSnapshotReaderStub{snapshot: BudgetSnapshot{
		HardExceeded:       true,
		OverrideAuthorized: true,
	}}}
	decision, err := gate.Evaluate(context.Background(), BudgetRequest{Repository: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != BudgetContinue {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestLedgerBudgetGateSoftBudgetDefaultsToFallback(t *testing.T) {
	gate := LedgerBudgetGate{Reader: budgetSnapshotReaderStub{snapshot: BudgetSnapshot{SoftExceeded: true}}}
	decision, err := gate.Evaluate(context.Background(), BudgetRequest{Repository: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != BudgetFallback || decision.Reason != "soft budget exceeded" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestLedgerBudgetGateRejectsInvalidSoftOutcome(t *testing.T) {
	gate := LedgerBudgetGate{Reader: budgetSnapshotReaderStub{snapshot: BudgetSnapshot{
		SoftExceeded: true,
		SoftOutcome:  BudgetContinue,
	}}}
	if _, err := gate.Evaluate(context.Background(), BudgetRequest{}); err == nil {
		t.Fatal("expected invalid soft outcome to fail")
	}
}

func TestLedgerBudgetGateFailsClosedWhenReaderUnavailable(t *testing.T) {
	gate := LedgerBudgetGate{}
	if _, err := gate.Evaluate(context.Background(), BudgetRequest{}); !errors.Is(err, ErrBudgetPolicyUnavailable) {
		t.Fatalf("err = %v, want ErrBudgetPolicyUnavailable", err)
	}
}
