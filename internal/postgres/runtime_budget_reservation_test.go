package postgres

import (
	"testing"
	"time"
)

func TestValidateRuntimeBudgetReservation(t *testing.T) {
	valid := RuntimeBudgetReservation{
		ID:                   "reservation-1",
		Repository:           "owner/repo",
		RunID:                "job-1",
		Role:                 "developer",
		Provider:             "openai",
		Model:                "gpt-test",
		ReservedCostMicroUSD: 1000,
		ExpiresAt:            time.Now().UTC().Add(time.Hour),
	}
	if err := validateRuntimeBudgetReservation(valid); err != nil {
		t.Fatalf("validateRuntimeBudgetReservation(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*RuntimeBudgetReservation)
	}{
		{name: "missing id", mutate: func(value *RuntimeBudgetReservation) { value.ID = "" }},
		{name: "missing repository", mutate: func(value *RuntimeBudgetReservation) { value.Repository = "" }},
		{name: "missing run", mutate: func(value *RuntimeBudgetReservation) { value.RunID = "" }},
		{name: "missing role", mutate: func(value *RuntimeBudgetReservation) { value.Role = "" }},
		{name: "missing provider", mutate: func(value *RuntimeBudgetReservation) { value.Provider = "" }},
		{name: "missing model", mutate: func(value *RuntimeBudgetReservation) { value.Model = "" }},
		{name: "missing reserved cost", mutate: func(value *RuntimeBudgetReservation) { value.ReservedCostMicroUSD = 0 }},
		{name: "negative reserved cost", mutate: func(value *RuntimeBudgetReservation) { value.ReservedCostMicroUSD = -1 }},
		{name: "missing expiry", mutate: func(value *RuntimeBudgetReservation) { value.ExpiresAt = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := valid
			tt.mutate(&value)
			if err := validateRuntimeBudgetReservation(value); err == nil {
				t.Fatal("validateRuntimeBudgetReservation() error = nil, want validation error")
			}
		})
	}
}
