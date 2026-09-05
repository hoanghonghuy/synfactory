package postgres

import "testing"

func TestValidateRuntimeBudgetPolicyScopes(t *testing.T) {
	tests := []struct {
		name    string
		policy  RuntimeBudgetPolicy
		wantErr bool
	}{
		{
			name: "repository day",
			policy: RuntimeBudgetPolicy{
				ID: "repo", Repository: "owner/repo", Scope: RuntimeBudgetScopeRepositoryDay,
				SoftLimitMicroUSD: 100, HardLimitMicroUSD: 200, SoftOutcome: "fallback", CreatedBy: "operator",
			},
		},
		{
			name: "role day",
			policy: RuntimeBudgetPolicy{
				ID: "role", Repository: "owner/repo", Scope: RuntimeBudgetScopeRoleDay, ScopeKey: "developer",
				SoftLimitMicroUSD: 100, HardLimitMicroUSD: 200, SoftOutcome: "park", CreatedBy: "operator",
			},
		},
		{
			name: "provider day requires key",
			policy: RuntimeBudgetPolicy{
				ID: "provider", Repository: "owner/repo", Scope: RuntimeBudgetScopeProviderDay,
				SoftLimitMicroUSD: 100, HardLimitMicroUSD: 200, SoftOutcome: "escalate", CreatedBy: "operator",
			},
			wantErr: true,
		},
		{
			name: "repository scope rejects key",
			policy: RuntimeBudgetPolicy{
				ID: "repo-key", Repository: "owner/repo", Scope: RuntimeBudgetScopeRepositoryDay, ScopeKey: "unexpected",
				CreatedBy: "operator",
			},
			wantErr: true,
		},
		{
			name: "soft cannot exceed hard",
			policy: RuntimeBudgetPolicy{
				ID: "limits", Repository: "owner/repo", Scope: RuntimeBudgetScopeWorkflowMax, ScopeKey: "wf-1",
				SoftLimitMicroUSD: 201, HardLimitMicroUSD: 200, CreatedBy: "operator",
			},
			wantErr: true,
		},
		{
			name: "invalid soft outcome",
			policy: RuntimeBudgetPolicy{
				ID: "outcome", Repository: "owner/repo", Scope: RuntimeBudgetScopeWorkflowMax, ScopeKey: "wf-1",
				SoftOutcome: "continue", CreatedBy: "operator",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuntimeBudgetPolicy(tt.policy)
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
