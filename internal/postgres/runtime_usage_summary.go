package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type RuntimeUsageTotals struct {
	Runs         int64 `json:"runs"`
	Requests     int64 `json:"requests"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	RuntimeMS    int64 `json:"runtime_ms"`
	CostMicroUSD int64 `json:"cost_microusd"`
}

type RuntimeUsageAggregate struct {
	Repository   string `json:"repository"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Role         string `json:"role"`
	Runs         int64  `json:"runs"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	RuntimeMS    int64  `json:"runtime_ms"`
	CostMicroUSD int64  `json:"cost_microusd"`
}

func (s *Store) RuntimeUsageSummary(ctx context.Context, repository string, since time.Time, limit int) (RuntimeUsageTotals, []RuntimeUsageAggregate, error) {
	repository = strings.TrimSpace(repository)
	if since.IsZero() {
		since = time.Now().UTC().Add(-24 * time.Hour)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var totals RuntimeUsageTotals
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT run_id),
       COALESCE(SUM(request_count), 0),
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(runtime_ms), 0),
       COALESCE(SUM(cost_microusd), 0)
  FROM runtime_usage_ledger
 WHERE recorded_at >= $1
   AND ($2 = '' OR repository = $2)`, since.UTC(), repository).Scan(
		&totals.Runs, &totals.Requests, &totals.InputTokens, &totals.OutputTokens,
		&totals.RuntimeMS, &totals.CostMicroUSD,
	); err != nil {
		return RuntimeUsageTotals{}, nil, fmt.Errorf("query runtime usage totals: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT repository, provider, model, role,
       COUNT(DISTINCT run_id),
       COALESCE(SUM(request_count), 0),
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(runtime_ms), 0),
       COALESCE(SUM(cost_microusd), 0)
  FROM runtime_usage_ledger
 WHERE recorded_at >= $1
   AND ($2 = '' OR repository = $2)
 GROUP BY repository, provider, model, role
 ORDER BY COALESCE(SUM(cost_microusd), 0) DESC, COUNT(DISTINCT run_id) DESC,
          repository, provider, model, role
 LIMIT $3`, since.UTC(), repository, limit)
	if err != nil {
		return RuntimeUsageTotals{}, nil, fmt.Errorf("query runtime usage aggregates: %w", err)
	}
	defer rows.Close()

	items := make([]RuntimeUsageAggregate, 0, limit)
	for rows.Next() {
		var item RuntimeUsageAggregate
		if err := rows.Scan(
			&item.Repository, &item.Provider, &item.Model, &item.Role,
			&item.Runs, &item.Requests, &item.InputTokens, &item.OutputTokens,
			&item.RuntimeMS, &item.CostMicroUSD,
		); err != nil {
			return RuntimeUsageTotals{}, nil, fmt.Errorf("scan runtime usage aggregate: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RuntimeUsageTotals{}, nil, fmt.Errorf("iterate runtime usage aggregates: %w", err)
	}
	return totals, items, nil
}
