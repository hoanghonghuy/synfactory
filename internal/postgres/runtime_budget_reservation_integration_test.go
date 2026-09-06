package postgres

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeBudgetReservationDuplicateIdentityFailsClosed(t *testing.T) {
	store := openTestStore(t)
	now := time.Now().UTC()
	reservation := RuntimeBudgetReservation{
		ID:                   "budget-duplicate-identity",
		Repository:           "owner/repo",
		RunID:                "job-1.1.attempt-1",
		Role:                 "developer",
		Provider:             "openai",
		Model:                "model",
		ReservedCostMicroUSD: 10,
		CreatedAt:            now,
		ExpiresAt:            now.Add(time.Hour),
	}
	if err := store.CreateRuntimeBudgetReservation(context.Background(), reservation); err != nil {
		t.Fatalf("first CreateRuntimeBudgetReservation() error = %v", err)
	}
	if err := store.CreateRuntimeBudgetReservation(context.Background(), reservation); err == nil {
		t.Fatal("duplicate CreateRuntimeBudgetReservation() unexpectedly succeeded")
	}
}
