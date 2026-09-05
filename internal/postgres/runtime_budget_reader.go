package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	runtimepolicy "github.com/hoanghonghuy/synfactory/internal/runtime"
)

// BudgetSnapshot evaluates persisted runtime budget policies against the durable
// usage ledger. A hard-budget override is considered authorized only when every
// hard-exceeded policy has an explicit audit record for the exact run identity.
func (s *Store) BudgetSnapshot(ctx context.Context, request runtimepolicy.BudgetRequest) (runtimepolicy.BudgetSnapshot, error) {
	repository := strings.TrimSpace(request.Repository)
	if repository == "" {
		return runtimepolicy.BudgetSnapshot{}, fmt.Errorf("runtime budget repository is required")
	}

	policies, err := s.RuntimeBudgetPolicies(ctx, repository)
	if err != nil {
		return runtimepolicy.BudgetSnapshot{}, err
	}

	var snapshot runtimepolicy.BudgetSnapshot
	var hardExceededPolicies []string
	for _, policy := range policies {
		if !runtimeBudgetPolicyMatchesRequest(policy, request) {
			continue
		}

		spent, err := s.runtimeBudgetSpentMicroUSD(ctx, policy, request, time.Now().UTC())
		if err != nil {
			return runtimepolicy.BudgetSnapshot{}, err
		}

		if policy.HardLimitMicroUSD > 0 && spent >= policy.HardLimitMicroUSD {
			snapshot.HardExceeded = true
			hardExceededPolicies = append(hardExceededPolicies, policy.ID)
			snapshot.Reason = runtimeBudgetReason(policy, spent, "hard")
			continue
		}
		if policy.SoftLimitMicroUSD > 0 && spent >= policy.SoftLimitMicroUSD {
			snapshot.SoftExceeded = true
			outcome := runtimeBudgetSoftOutcome(policy.SoftOutcome)
			if runtimeBudgetOutcomePriority(outcome) > runtimeBudgetOutcomePriority(snapshot.SoftOutcome) {
				snapshot.SoftOutcome = outcome
				snapshot.Reason = runtimeBudgetReason(policy, spent, "soft")
			}
		}
	}

	if len(hardExceededPolicies) == 0 {
		return snapshot, nil
	}

	for _, policyID := range hardExceededPolicies {
		authorized, err := s.runtimeBudgetOverrideAuthorized(ctx, policyID, request)
		if err != nil {
			return runtimepolicy.BudgetSnapshot{}, err
		}
		if !authorized {
			snapshot.OverrideAuthorized = false
			return snapshot, nil
		}
	}
	snapshot.OverrideAuthorized = true
	return snapshot, nil
}

func runtimeBudgetPolicyMatchesRequest(policy RuntimeBudgetPolicy, request runtimepolicy.BudgetRequest) bool {
	if strings.TrimSpace(policy.Repository) != strings.TrimSpace(request.Repository) || !policy.Enabled {
		return false
	}
	switch policy.Scope {
	case RuntimeBudgetScopeRepositoryDay:
		return true
	case RuntimeBudgetScopeRoleDay:
		return strings.TrimSpace(policy.ScopeKey) == strings.TrimSpace(request.Role)
	case RuntimeBudgetScopeProviderDay:
		return strings.TrimSpace(policy.ScopeKey) == strings.TrimSpace(request.Provider)
	case RuntimeBudgetScopeWorkflowMax:
		return strings.TrimSpace(policy.ScopeKey) == strings.TrimSpace(request.WorkflowID)
	default:
		return false
	}
}

func (s *Store) runtimeBudgetSpentMicroUSD(ctx context.Context, policy RuntimeBudgetPolicy, request runtimepolicy.BudgetRequest, now time.Time) (int64, error) {
	var query string
	var args []any
	repository := strings.TrimSpace(request.Repository)

	switch policy.Scope {
	case RuntimeBudgetScopeRepositoryDay:
		query = `SELECT COALESCE(SUM(cost_microusd), 0) FROM runtime_usage_ledger WHERE repository = $1 AND recorded_at >= $2`
		args = []any{repository, utcDayStart(now)}
	case RuntimeBudgetScopeRoleDay:
		query = `SELECT COALESCE(SUM(cost_microusd), 0) FROM runtime_usage_ledger WHERE repository = $1 AND role = $2 AND recorded_at >= $3`
		args = []any{repository, strings.TrimSpace(request.Role), utcDayStart(now)}
	case RuntimeBudgetScopeProviderDay:
		query = `SELECT COALESCE(SUM(cost_microusd), 0) FROM runtime_usage_ledger WHERE repository = $1 AND provider = $2 AND recorded_at >= $3`
		args = []any{repository, strings.TrimSpace(request.Provider), utcDayStart(now)}
	case RuntimeBudgetScopeWorkflowMax:
		query = `SELECT COALESCE(SUM(cost_microusd), 0) FROM runtime_usage_ledger WHERE repository = $1 AND workflow_id = $2`
		args = []any{repository, strings.TrimSpace(request.WorkflowID)}
	default:
		return 0, fmt.Errorf("unsupported runtime budget policy scope %q", policy.Scope)
	}

	var spent int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&spent); err != nil {
		return 0, fmt.Errorf("query runtime budget usage: %w", err)
	}
	return spent, nil
}

func (s *Store) runtimeBudgetOverrideAuthorized(ctx context.Context, policyID string, request runtimepolicy.BudgetRequest) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
      FROM runtime_budget_override_audit
     WHERE policy_id = $1
       AND repository = $2
       AND workflow_id = $3
       AND task_id = $4
       AND run_id = $5
)`, strings.TrimSpace(policyID), strings.TrimSpace(request.Repository), strings.TrimSpace(request.WorkflowID), strings.TrimSpace(request.TaskID), strings.TrimSpace(request.RunID)).Scan(&exists); err != nil {
		return false, fmt.Errorf("query runtime budget override: %w", err)
	}
	return exists, nil
}

func utcDayStart(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func runtimeBudgetSoftOutcome(outcome string) runtimepolicy.BudgetOutcome {
	switch strings.TrimSpace(outcome) {
	case string(runtimepolicy.BudgetPark):
		return runtimepolicy.BudgetPark
	case string(runtimepolicy.BudgetEscalate):
		return runtimepolicy.BudgetEscalate
	default:
		return runtimepolicy.BudgetFallback
	}
}

func runtimeBudgetOutcomePriority(outcome runtimepolicy.BudgetOutcome) int {
	switch outcome {
	case runtimepolicy.BudgetEscalate:
		return 3
	case runtimepolicy.BudgetPark:
		return 2
	case runtimepolicy.BudgetFallback:
		return 1
	default:
		return 0
	}
}

func runtimeBudgetReason(policy RuntimeBudgetPolicy, spent int64, level string) string {
	return fmt.Sprintf("%s budget policy %s exceeded: spent=%d microusd", level, policy.ID, spent)
}
