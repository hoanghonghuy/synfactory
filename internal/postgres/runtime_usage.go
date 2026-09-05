package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrPricingVersionConflict = errors.New("pricing version already exists with different values")

type RuntimePricing struct {
	Version                   string
	Provider                  string
	Model                     string
	InputMicroUSDPerMillion   int64
	OutputMicroUSDPerMillion  int64
	RequestMicroUSD           int64
	EffectiveAt               time.Time
}

type RuntimeUsage struct {
	ID             string
	Repository     string
	WorkflowID     string
	TaskID         string
	RunID          string
	Role           string
	Runtime        string
	Provider       string
	Model          string
	PricingVersion string
	RequestCount   int64
	InputTokens    int64
	OutputTokens   int64
	RuntimeMS      int64
	CostMicroUSD   int64
	RecordedAt     time.Time
}

func (s *Store) PutRuntimePricing(ctx context.Context, pricing RuntimePricing) error {
	pricing.Version = strings.TrimSpace(pricing.Version)
	pricing.Provider = strings.TrimSpace(pricing.Provider)
	pricing.Model = strings.TrimSpace(pricing.Model)
	if pricing.Version == "" || pricing.Provider == "" || pricing.Model == "" {
		return errors.New("runtime pricing version, provider, and model are required")
	}
	if pricing.InputMicroUSDPerMillion < 0 || pricing.OutputMicroUSDPerMillion < 0 || pricing.RequestMicroUSD < 0 {
		return errors.New("runtime pricing values must be non-negative")
	}
	if pricing.EffectiveAt.IsZero() {
		return errors.New("runtime pricing effective_at is required")
	}

	result, err := s.db.ExecContext(ctx, `
INSERT INTO runtime_pricing_versions (
    version, provider, model, input_microusd_per_million,
    output_microusd_per_million, request_microusd, effective_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (version) DO NOTHING`,
		pricing.Version, pricing.Provider, pricing.Model, pricing.InputMicroUSDPerMillion,
		pricing.OutputMicroUSDPerMillion, pricing.RequestMicroUSD, pricing.EffectiveAt,
	)
	if err != nil {
		return fmt.Errorf("insert runtime pricing: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 1 {
		return nil
	}

	var existing RuntimePricing
	if err := s.db.QueryRowContext(ctx, `
SELECT version, provider, model, input_microusd_per_million,
       output_microusd_per_million, request_microusd, effective_at
  FROM runtime_pricing_versions
 WHERE version = $1`, pricing.Version).Scan(
		&existing.Version, &existing.Provider, &existing.Model, &existing.InputMicroUSDPerMillion,
		&existing.OutputMicroUSDPerMillion, &existing.RequestMicroUSD, &existing.EffectiveAt,
	); err != nil {
		return fmt.Errorf("read existing runtime pricing: %w", err)
	}
	if existing.Provider != pricing.Provider || existing.Model != pricing.Model ||
		existing.InputMicroUSDPerMillion != pricing.InputMicroUSDPerMillion ||
		existing.OutputMicroUSDPerMillion != pricing.OutputMicroUSDPerMillion ||
		existing.RequestMicroUSD != pricing.RequestMicroUSD || !existing.EffectiveAt.Equal(pricing.EffectiveAt) {
		return ErrPricingVersionConflict
	}
	return nil
}

func EstimateRuntimeCostMicroUSD(pricing RuntimePricing, requestCount, inputTokens, outputTokens int64) (int64, error) {
	if requestCount < 0 || inputTokens < 0 || outputTokens < 0 {
		return 0, errors.New("runtime usage values must be non-negative")
	}
	return pricing.RequestMicroUSD*requestCount +
		(pricing.InputMicroUSDPerMillion*inputTokens)/1_000_000 +
		(pricing.OutputMicroUSDPerMillion*outputTokens)/1_000_000, nil
}

func (s *Store) RecordRuntimeUsage(ctx context.Context, usage RuntimeUsage) error {
	usage.ID = strings.TrimSpace(usage.ID)
	usage.Repository = strings.TrimSpace(usage.Repository)
	usage.WorkflowID = strings.TrimSpace(usage.WorkflowID)
	usage.TaskID = strings.TrimSpace(usage.TaskID)
	usage.RunID = strings.TrimSpace(usage.RunID)
	usage.Role = strings.TrimSpace(usage.Role)
	usage.Runtime = strings.TrimSpace(usage.Runtime)
	usage.Provider = strings.TrimSpace(usage.Provider)
	usage.Model = strings.TrimSpace(usage.Model)
	usage.PricingVersion = strings.TrimSpace(usage.PricingVersion)
	if usage.ID == "" || usage.Repository == "" || usage.RunID == "" || usage.Role == "" || usage.Runtime == "" || usage.Provider == "" || usage.Model == "" || usage.PricingVersion == "" {
		return errors.New("runtime usage identity fields are required")
	}
	if usage.RequestCount < 0 || usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.RuntimeMS < 0 || usage.CostMicroUSD < 0 {
		return errors.New("runtime usage values must be non-negative")
	}
	if usage.RecordedAt.IsZero() {
		usage.RecordedAt = time.Now().UTC()
	}

	var provider, model string
	if err := s.db.QueryRowContext(ctx, `SELECT provider, model FROM runtime_pricing_versions WHERE version = $1`, usage.PricingVersion).Scan(&provider, &model); err != nil {
		return fmt.Errorf("resolve runtime pricing version: %w", err)
	}
	if provider != usage.Provider || model != usage.Model {
		return errors.New("runtime usage provider/model does not match pricing version")
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO runtime_usage_ledger (
    id, repository, workflow_id, task_id, run_id, role, runtime, provider, model,
    pricing_version, request_count, input_tokens, output_tokens, runtime_ms,
    cost_microusd, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		usage.ID, usage.Repository, usage.WorkflowID, usage.TaskID, usage.RunID, usage.Role, usage.Runtime,
		usage.Provider, usage.Model, usage.PricingVersion, usage.RequestCount, usage.InputTokens,
		usage.OutputTokens, usage.RuntimeMS, usage.CostMicroUSD, usage.RecordedAt,
	)
	if err != nil {
		return fmt.Errorf("record runtime usage: %w", err)
	}
	return nil
}

func (s *Store) RuntimeUsageForRun(ctx context.Context, repository, workflowID, taskID, runID string) ([]RuntimeUsage, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, repository, workflow_id, task_id, run_id, role, runtime, provider, model,
       pricing_version, request_count, input_tokens, output_tokens, runtime_ms,
       cost_microusd, recorded_at
  FROM runtime_usage_ledger
 WHERE repository = $1 AND workflow_id = $2 AND task_id = $3 AND run_id = $4
 ORDER BY recorded_at, id`, strings.TrimSpace(repository), strings.TrimSpace(workflowID), strings.TrimSpace(taskID), strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("query runtime usage: %w", err)
	}
	defer rows.Close()

	var usages []RuntimeUsage
	for rows.Next() {
		var usage RuntimeUsage
		if err := rows.Scan(
			&usage.ID, &usage.Repository, &usage.WorkflowID, &usage.TaskID, &usage.RunID,
			&usage.Role, &usage.Runtime, &usage.Provider, &usage.Model, &usage.PricingVersion,
			&usage.RequestCount, &usage.InputTokens, &usage.OutputTokens, &usage.RuntimeMS,
			&usage.CostMicroUSD, &usage.RecordedAt,
		); err != nil {
			return nil, fmt.Errorf("scan runtime usage: %w", err)
		}
		usages = append(usages, usage)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime usage: %w", err)
	}
	return usages, nil
}
