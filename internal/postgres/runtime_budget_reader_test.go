package postgres

import (
	"math"
	"testing"
	"time"

	runtimepolicy "github.com/hoanghonghuy/synfactory/internal/runtime"
)

func TestRuntimeBudgetPolicyMatchesRequest(t *testing.T) {
	request := runtimepolicy.BudgetRequest{
		Repository: "owner/repo",
		WorkflowID: "wf-1",
		Role:       "developer",
		Provider:   "openai",
	}

	tests := []struct {
		name   string
		policy RuntimeBudgetPolicy
		want   bool
	}{
		{name: "repository day", policy: RuntimeBudgetPolicy{Repository: "owner/repo", Scope: RuntimeBudgetScopeRepositoryDay, Enabled: true}, want: true},
		{name: "role day", policy: RuntimeBudgetPolicy{Repository: "owner/repo", Scope: RuntimeBudgetScopeRoleDay, ScopeKey: "developer", Enabled: true}, want: true},
		{name: "provider day", policy: RuntimeBudgetPolicy{Repository: "owner/repo", Scope: RuntimeBudgetScopeProviderDay, ScopeKey: "openai", Enabled: true}, want: true},
		{name: "workflow max", policy: RuntimeBudgetPolicy{Repository: "owner/repo", Scope: RuntimeBudgetScopeWorkflowMax, ScopeKey: "wf-1", Enabled: true}, want: true},
		{name: "wrong provider", policy: RuntimeBudgetPolicy{Repository: "owner/repo", Scope: RuntimeBudgetScopeProviderDay, ScopeKey: "anthropic", Enabled: true}},
		{name: "wrong repository", policy: RuntimeBudgetPolicy{Repository: "other/repo", Scope: RuntimeBudgetScopeRepositoryDay, Enabled: true}},
		{name: "disabled", policy: RuntimeBudgetPolicy{Repository: "owner/repo", Scope: RuntimeBudgetScopeRepositoryDay, Enabled: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeBudgetPolicyMatchesRequest(tt.policy, request); got != tt.want {
				t.Fatalf("runtimeBudgetPolicyMatchesRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRuntimeBudgetOutcomePriorityUsesMostRestrictiveOutcome(t *testing.T) {
	if runtimeBudgetOutcomePriority(runtimepolicy.BudgetEscalate) <= runtimeBudgetOutcomePriority(runtimepolicy.BudgetPark) {
		t.Fatal("escalate must be more restrictive than park")
	}
	if runtimeBudgetOutcomePriority(runtimepolicy.BudgetPark) <= runtimeBudgetOutcomePriority(runtimepolicy.BudgetFallback) {
		t.Fatal("park must be more restrictive than fallback")
	}
}

func TestRuntimeBudgetCeilTokenCostRoundsUp(t *testing.T) {
	got, err := runtimeBudgetCeilTokenCost(1_500_000, 3)
	if err != nil {
		t.Fatalf("runtimeBudgetCeilTokenCost() error = %v", err)
	}
	if got != 5 {
		t.Fatalf("runtimeBudgetCeilTokenCost() = %d, want 5", got)
	}
}

func TestRuntimeBudgetCeilTokenCostRejectsOverflow(t *testing.T) {
	if _, err := runtimeBudgetCeilTokenCost(math.MaxInt64, 2); err == nil {
		t.Fatal("runtimeBudgetCeilTokenCost() expected overflow error")
	}
}

func TestRuntimeBudgetReservationIDIsAttemptScoped(t *testing.T) {
	base := runtimepolicy.BudgetRequest{Repository: "owner/repo", RunID: "job-1.1", Provider: "openai", Model: "gpt"}
	other := base
	other.RunID = "job-1.2"
	if runtimeBudgetReservationID(base) == runtimeBudgetReservationID(other) {
		t.Fatal("runtime budget reservation ids must differ across attempt-scoped run ids")
	}
}

func TestUTCDayStart(t *testing.T) {
	input := time.Date(2026, time.September, 6, 23, 45, 12, 123, time.FixedZone("UTC+7", 7*60*60))
	got := utcDayStart(input)
	want := time.Date(2026, time.September, 6, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("utcDayStart() = %s, want %s", got, want)
	}
}
