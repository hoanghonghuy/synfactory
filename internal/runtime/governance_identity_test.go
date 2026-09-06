package runtime

import (
	"math"
	"testing"
)

func TestBudgetRunIDIncludesDurableJobAttempt(t *testing.T) {
	request := Request{
		RunID: "job-7.2",
		Metadata: map[string]string{
			"job_id":      "job-7",
			"job_attempt": "3",
		},
	}
	if got, want := budgetRunID(request), "job-7.2.attempt-3"; got != want {
		t.Fatalf("budgetRunID() = %q, want %q", got, want)
	}
}

func TestBudgetRunIDPreservesNonWorkerCallerIdentity(t *testing.T) {
	request := Request{RunID: "manual-run.1", Metadata: map[string]string{}}
	if got, want := budgetRunID(request), "manual-run.1"; got != want {
		t.Fatalf("budgetRunID() = %q, want %q", got, want)
	}
}

func TestConservativeBudgetTokenLimitPreservesPositiveBound(t *testing.T) {
	if got, want := conservativeBudgetTokenLimit(4096), int64(4096); got != want {
		t.Fatalf("conservativeBudgetTokenLimit() = %d, want %d", got, want)
	}
}

func TestConservativeBudgetTokenLimitTreatsUnsetAsUnbounded(t *testing.T) {
	if got, want := conservativeBudgetTokenLimit(0), int64(math.MaxInt64); got != want {
		t.Fatalf("conservativeBudgetTokenLimit() = %d, want %d", got, want)
	}
}
