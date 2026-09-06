package runtime

import (
	"context"
	"errors"
	"testing"
)

type budgetPolicyMatcherReaderStub struct {
	matched    bool
	matchErr   error
	readCalled bool
}

func (s *budgetPolicyMatcherReaderStub) HasBudgetPolicy(_ context.Context, _ BudgetRequest) (bool, error) {
	return s.matched, s.matchErr
}

func (s *budgetPolicyMatcherReaderStub) BudgetSnapshot(_ context.Context, _ BudgetRequest) (BudgetSnapshot, error) {
	s.readCalled = true
	return BudgetSnapshot{}, errors.New("snapshot should not be read for an unbudgeted route")
}

func TestLedgerBudgetGateSkipsSnapshotForUnbudgetedRoute(t *testing.T) {
	reader := &budgetPolicyMatcherReaderStub{matched: false}
	gate := LedgerBudgetGate{Reader: reader}
	decision, err := gate.Evaluate(context.Background(), BudgetRequest{Repository: "owner/repo"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Outcome != BudgetContinue {
		t.Fatalf("Evaluate() outcome = %q, want %q", decision.Outcome, BudgetContinue)
	}
	if reader.readCalled {
		t.Fatal("BudgetSnapshot() was called for an unbudgeted route")
	}
}

func TestLedgerBudgetGateFailsClosedWhenPolicyMatchLookupFails(t *testing.T) {
	reader := &budgetPolicyMatcherReaderStub{matchErr: errors.New("policy store unavailable")}
	gate := LedgerBudgetGate{Reader: reader}
	if _, err := gate.Evaluate(context.Background(), BudgetRequest{Repository: "owner/repo"}); err == nil {
		t.Fatal("Evaluate() expected policy matcher error")
	}
}
