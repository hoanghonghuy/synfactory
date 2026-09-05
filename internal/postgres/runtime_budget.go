package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	RuntimeBudgetScopeRepositoryDay = "repository_day"
	RuntimeBudgetScopeRoleDay       = "role_day"
	RuntimeBudgetScopeProviderDay   = "provider_day"
	RuntimeBudgetScopeWorkflowMax   = "workflow_max"
)

type RuntimeBudgetPolicy struct {
	ID               string
	Repository       string
	Scope            string
	ScopeKey         string
	SoftLimitMicroUSD int64
	HardLimitMicroUSD int64
	SoftOutcome      string
	Enabled          bool
	CreatedBy        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type RuntimeBudgetOverrideAudit struct {
	ID         string
	PolicyID   string
	Repository string
	WorkflowID string
	TaskID     string
	RunID      string
	Actor      string
	Reason     string
	CreatedAt  time.Time
}

func validateRuntimeBudgetPolicy(policy RuntimeBudgetPolicy) error {
	policy.ID = strings.TrimSpace(policy.ID)
	policy.Repository = strings.TrimSpace(policy.Repository)
	policy.Scope = strings.TrimSpace(policy.Scope)
	policy.ScopeKey = strings.TrimSpace(policy.ScopeKey)
	policy.SoftOutcome = strings.TrimSpace(policy.SoftOutcome)
	policy.CreatedBy = strings.TrimSpace(policy.CreatedBy)
	if policy.ID == "" || policy.Repository == "" || policy.Scope == "" || policy.CreatedBy == "" {
		return errors.New("runtime budget policy identity fields are required")
	}
	switch policy.Scope {
	case RuntimeBudgetScopeRepositoryDay:
		if policy.ScopeKey != "" {
			return errors.New("repository/day budget scope_key must be empty")
		}
	case RuntimeBudgetScopeRoleDay, RuntimeBudgetScopeProviderDay, RuntimeBudgetScopeWorkflowMax:
		if policy.ScopeKey == "" {
			return errors.New("runtime budget policy scope_key is required for scoped policy")
		}
	default:
		return errors.New("unsupported runtime budget policy scope")
	}
	if policy.SoftLimitMicroUSD < 0 || policy.HardLimitMicroUSD < 0 {
		return errors.New("runtime budget limits must be non-negative")
	}
	if policy.HardLimitMicroUSD > 0 && policy.SoftLimitMicroUSD > policy.HardLimitMicroUSD {
		return errors.New("runtime soft budget cannot exceed hard budget")
	}
	if policy.SoftOutcome == "" {
		policy.SoftOutcome = "fallback"
	}
	switch policy.SoftOutcome {
	case "fallback", "park", "escalate":
	default:
		return errors.New("runtime budget soft outcome must be fallback, park, or escalate")
	}
	return nil
}

func (s *Store) PutRuntimeBudgetPolicy(ctx context.Context, policy RuntimeBudgetPolicy) error {
	policy.ID = strings.TrimSpace(policy.ID)
	policy.Repository = strings.TrimSpace(policy.Repository)
	policy.Scope = strings.TrimSpace(policy.Scope)
	policy.ScopeKey = strings.TrimSpace(policy.ScopeKey)
	policy.SoftOutcome = strings.TrimSpace(policy.SoftOutcome)
	policy.CreatedBy = strings.TrimSpace(policy.CreatedBy)
	if policy.SoftOutcome == "" {
		policy.SoftOutcome = "fallback"
	}
	if err := validateRuntimeBudgetPolicy(policy); err != nil {
		return err
	}
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = time.Now().UTC()
	}
	if policy.UpdatedAt.IsZero() {
		policy.UpdatedAt = policy.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO runtime_budget_policies (
    id, repository, scope, scope_key, soft_limit_microusd, hard_limit_microusd,
    soft_outcome, enabled, created_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (id) DO UPDATE SET
    repository = EXCLUDED.repository,
    scope = EXCLUDED.scope,
    scope_key = EXCLUDED.scope_key,
    soft_limit_microusd = EXCLUDED.soft_limit_microusd,
    hard_limit_microusd = EXCLUDED.hard_limit_microusd,
    soft_outcome = EXCLUDED.soft_outcome,
    enabled = EXCLUDED.enabled,
    updated_at = EXCLUDED.updated_at`,
		policy.ID, policy.Repository, policy.Scope, policy.ScopeKey, policy.SoftLimitMicroUSD,
		policy.HardLimitMicroUSD, policy.SoftOutcome, policy.Enabled, policy.CreatedBy,
		policy.CreatedAt, policy.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("put runtime budget policy: %w", err)
	}
	return nil
}

func (s *Store) RuntimeBudgetPolicies(ctx context.Context, repository string) ([]RuntimeBudgetPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, repository, scope, scope_key, soft_limit_microusd, hard_limit_microusd,
       soft_outcome, enabled, created_by, created_at, updated_at
  FROM runtime_budget_policies
 WHERE repository = $1 AND enabled = TRUE
 ORDER BY scope, scope_key, id`, strings.TrimSpace(repository))
	if err != nil {
		return nil, fmt.Errorf("query runtime budget policies: %w", err)
	}
	defer rows.Close()

	var policies []RuntimeBudgetPolicy
	for rows.Next() {
		var policy RuntimeBudgetPolicy
		if err := rows.Scan(
			&policy.ID, &policy.Repository, &policy.Scope, &policy.ScopeKey,
			&policy.SoftLimitMicroUSD, &policy.HardLimitMicroUSD, &policy.SoftOutcome,
			&policy.Enabled, &policy.CreatedBy, &policy.CreatedAt, &policy.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan runtime budget policy: %w", err)
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime budget policies: %w", err)
	}
	return policies, nil
}

func (s *Store) RecordRuntimeBudgetOverride(ctx context.Context, audit RuntimeBudgetOverrideAudit) error {
	audit.ID = strings.TrimSpace(audit.ID)
	audit.PolicyID = strings.TrimSpace(audit.PolicyID)
	audit.Repository = strings.TrimSpace(audit.Repository)
	audit.WorkflowID = strings.TrimSpace(audit.WorkflowID)
	audit.TaskID = strings.TrimSpace(audit.TaskID)
	audit.RunID = strings.TrimSpace(audit.RunID)
	audit.Actor = strings.TrimSpace(audit.Actor)
	audit.Reason = strings.TrimSpace(audit.Reason)
	if audit.ID == "" || audit.PolicyID == "" || audit.Repository == "" || audit.RunID == "" || audit.Actor == "" || audit.Reason == "" {
		return errors.New("runtime budget override audit identity, actor, and reason are required")
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO runtime_budget_override_audit (
    id, policy_id, repository, workflow_id, task_id, run_id, actor, reason, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		audit.ID, audit.PolicyID, audit.Repository, audit.WorkflowID, audit.TaskID,
		audit.RunID, audit.Actor, audit.Reason, audit.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record runtime budget override audit: %w", err)
	}
	return nil
}
