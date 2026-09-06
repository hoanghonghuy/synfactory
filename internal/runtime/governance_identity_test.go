package runtime

import "testing"

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
