package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	runtimepolicy "github.com/hoanghonghuy/synfactory/internal/runtime"
)

const runtimeBudgetReservationTTL = 24 * time.Hour

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
	projected, err := s.runtimeBudgetProjectedCostMicroUSD(ctx, request, time.Now().UTC())
	if err != nil {
		return runtimepolicy.BudgetSnapshot{}, err
	}
	return s.budgetSnapshotWithPolicies(ctx, request, policies, projected)
}

// AcquireBudgetSnapshot serializes attempts that are subject to at least one
// matching hard budget. While the repository lock is held, the store resolves
// immutable server-side pricing, evaluates durable spend plus the projected
// attempt cost, and persists a fail-closed reservation before returning an
// admissible snapshot. The reservation therefore survives process death even
// though PostgreSQL releases the advisory lock with the dead connection.
func (s *Store) AcquireBudgetSnapshot(ctx context.Context, request runtimepolicy.BudgetRequest) (runtimepolicy.BudgetSnapshot, func(), error) {
	repository := strings.TrimSpace(request.Repository)
	if repository == "" {
		return runtimepolicy.BudgetSnapshot{}, nil, fmt.Errorf("runtime budget repository is required")
	}

	policies, err := s.RuntimeBudgetPolicies(ctx, repository)
	if err != nil {
		return runtimepolicy.BudgetSnapshot{}, nil, err
	}
	if !runtimeBudgetHasMatchingHardPolicy(policies, request) {
		projected, err := s.runtimeBudgetProjectedCostMicroUSD(ctx, request, time.Now().UTC())
		if err != nil {
			return runtimepolicy.BudgetSnapshot{}, nil, err
		}
		snapshot, err := s.budgetSnapshotWithPolicies(ctx, request, policies, projected)
		return snapshot, func() {}, err
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return runtimepolicy.BudgetSnapshot{}, nil, fmt.Errorf("reserve runtime budget connection: %w", err)
	}
	lockKey := "runtime-budget:" + repository
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		_ = conn.Close()
		return runtimepolicy.BudgetSnapshot{}, nil, fmt.Errorf("acquire runtime budget lock: %w", err)
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockKey)
			_ = conn.Close()
		})
	}

	// A contender may have waited behind another attempt. Reload policy state and
	// spend after acquiring the lock so the decision observes that attempt's
	// accounting or durable reservation before admitting more work.
	policies, err = s.RuntimeBudgetPolicies(ctx, repository)
	if err != nil {
		release()
		return runtimepolicy.BudgetSnapshot{}, nil, err
	}
	now := time.Now().UTC()
	projected, err := s.runtimeBudgetProjectedCostMicroUSD(ctx, request, now)
	if err != nil {
		release()
		return runtimepolicy.BudgetSnapshot{}, nil, err
	}
	snapshot, err := s.budgetSnapshotWithPolicies(ctx, request, policies, projected)
	if err != nil {
		release()
		return runtimepolicy.BudgetSnapshot{}, nil, err
	}

	// Soft exhaustion always routes away/parks/escalates. Hard exhaustion may
	// continue only with an exact-run authorized override. Persist the reservation
	// before the serialized admission window can be released.
	admitted := !snapshot.SoftExceeded && (!snapshot.HardExceeded || snapshot.OverrideAuthorized)
	if admitted && projected > 0 {
		reservation := RuntimeBudgetReservation{
			ID:                   runtimeBudgetReservationID(request),
			Repository:           repository,
			WorkflowID:           strings.TrimSpace(request.WorkflowID),
			TaskID:               strings.TrimSpace(request.TaskID),
			RunID:                strings.TrimSpace(request.RunID),
			Role:                 strings.TrimSpace(request.Role),
			Provider:             strings.TrimSpace(request.Provider),
			Model:                strings.TrimSpace(request.Model),
			ReservedCostMicroUSD: projected,
			CreatedAt:            now,
			ExpiresAt:            now.Add(runtimeBudgetReservationTTL),
		}
		if err := s.CreateRuntimeBudgetReservation(ctx, reservation); err != nil {
			release()
			return runtimepolicy.BudgetSnapshot{}, nil, fmt.Errorf("persist runtime budget admission reservation: %w", err)
		}
	}
	return snapshot, release, nil
}

func runtimeBudgetHasMatchingHardPolicy(policies []RuntimeBudgetPolicy, request runtimepolicy.BudgetRequest) bool {
	for _, policy := range policies {
		if policy.HardLimitMicroUSD > 0 && runtimeBudgetPolicyMatchesRequest(policy, request) {
			return true
		}
	}
	return false
}

func (s *Store) budgetSnapshotWithPolicies(ctx context.Context, request runtimepolicy.BudgetRequest, policies []RuntimeBudgetPolicy, projected int64) (runtimepolicy.BudgetSnapshot, error) {
	var snapshot runtimepolicy.BudgetSnapshot
	var hardExceededPolicies []string
	now := time.Now().UTC()
	for _, policy := range policies {
		if !runtimeBudgetPolicyMatchesRequest(policy, request) {
			continue
		}

		spent, err := s.runtimeBudgetSpentMicroUSD(ctx, policy, request, now)
		if err != nil {
			return runtimepolicy.BudgetSnapshot{}, err
		}
		projectedSpend, err := addRuntimeBudgetCost(spent, projected)
		if err != nil {
			return runtimepolicy.BudgetSnapshot{}, err
		}

		if policy.HardLimitMicroUSD > 0 && projectedSpend > policy.HardLimitMicroUSD {
			snapshot.HardExceeded = true
			hardExceededPolicies = append(hardExceededPolicies, policy.ID)
			snapshot.Reason = runtimeBudgetReason(policy, projectedSpend, "hard")
			continue
		}
		if policy.SoftLimitMicroUSD > 0 && projectedSpend > policy.SoftLimitMicroUSD {
			snapshot.SoftExceeded = true
			outcome := runtimeBudgetSoftOutcome(policy.SoftOutcome)
			if runtimeBudgetOutcomePriority(outcome) > runtimeBudgetOutcomePriority(snapshot.SoftOutcome) {
				snapshot.SoftOutcome = outcome
				snapshot.Reason = runtimeBudgetReason(policy, projectedSpend, "soft")
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

func (s *Store) runtimeBudgetProjectedCostMicroUSD(ctx context.Context, request runtimepolicy.BudgetRequest, at time.Time) (int64, error) {
	if request.InputTokenLimit < 0 || request.OutputTokenLimit < 0 {
		return 0, fmt.Errorf("runtime budget token projections must be non-negative")
	}
	pricing, err := s.ResolveRuntimePricing(ctx, request.Provider, request.Model, at)
	if err != nil {
		return 0, fmt.Errorf("resolve runtime budget projection pricing: %w", err)
	}
	inputCost, err := runtimeBudgetCeilTokenCost(pricing.InputMicroUSDPerMillion, request.InputTokenLimit)
	if err != nil {
		return 0, err
	}
	outputCost, err := runtimeBudgetCeilTokenCost(pricing.OutputMicroUSDPerMillion, request.OutputTokenLimit)
	if err != nil {
		return 0, err
	}
	cost, err := addRuntimeBudgetCost(pricing.RequestMicroUSD, inputCost)
	if err != nil {
		return 0, err
	}
	return addRuntimeBudgetCost(cost, outputCost)
}

func runtimeBudgetCeilTokenCost(ratePerMillion, tokens int64) (int64, error) {
	if ratePerMillion < 0 || tokens < 0 {
		return 0, fmt.Errorf("runtime budget pricing inputs must be non-negative")
	}
	if ratePerMillion == 0 || tokens == 0 {
		return 0, nil
	}
	if ratePerMillion > math.MaxInt64/tokens {
		return 0, fmt.Errorf("runtime budget projected token cost overflow")
	}
	product := ratePerMillion * tokens
	cost := product / 1_000_000
	if product%1_000_000 != 0 {
		cost++
	}
	return cost, nil
}

func addRuntimeBudgetCost(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, fmt.Errorf("runtime budget projected cost overflow")
	}
	return left + right, nil
}

func runtimeBudgetReservationID(request runtimepolicy.BudgetRequest) string {
	identity := strings.Join([]string{
		strings.TrimSpace(request.Repository),
		strings.TrimSpace(request.RunID),
		strings.TrimSpace(request.Provider),
		strings.TrimSpace(request.Model),
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return "budget_" + hex.EncodeToString(sum[:12])
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

	var ledgerSpent int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&ledgerSpent); err != nil {
		return 0, fmt.Errorf("query runtime budget usage: %w", err)
	}
	reserved, err := s.runtimeBudgetReservedMicroUSD(ctx, policy, request)
	if err != nil {
		return 0, err
	}
	return addRuntimeBudgetCost(ledgerSpent, reserved)
}

func (s *Store) runtimeBudgetReservedMicroUSD(ctx context.Context, policy RuntimeBudgetPolicy, request runtimepolicy.BudgetRequest) (int64, error) {
	var query string
	var args []any
	repository := strings.TrimSpace(request.Repository)

	switch policy.Scope {
	case RuntimeBudgetScopeRepositoryDay:
		query = `SELECT COALESCE(SUM(reserved_cost_microusd), 0) FROM runtime_budget_reservations WHERE repository = $1 AND state = 'active'`
		args = []any{repository}
	case RuntimeBudgetScopeRoleDay:
		query = `SELECT COALESCE(SUM(reserved_cost_microusd), 0) FROM runtime_budget_reservations WHERE repository = $1 AND role = $2 AND state = 'active'`
		args = []any{repository, strings.TrimSpace(request.Role)}
	case RuntimeBudgetScopeProviderDay:
		query = `SELECT COALESCE(SUM(reserved_cost_microusd), 0) FROM runtime_budget_reservations WHERE repository = $1 AND provider = $2 AND state = 'active'`
		args = []any{repository, strings.TrimSpace(request.Provider)}
	case RuntimeBudgetScopeWorkflowMax:
		query = `SELECT COALESCE(SUM(reserved_cost_microusd), 0) FROM runtime_budget_reservations WHERE repository = $1 AND workflow_id = $2 AND state = 'active'`
		args = []any{repository, strings.TrimSpace(request.WorkflowID)}
	default:
		return 0, fmt.Errorf("unsupported runtime budget policy scope %q", policy.Scope)
	}

	var reserved int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&reserved); err != nil {
		return 0, fmt.Errorf("query active runtime budget reservations: %w", err)
	}
	return reserved, nil
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
	return fmt.Sprintf("%s budget policy %s exceeded: projected_spend=%d microusd", level, policy.ID, spent)
}
