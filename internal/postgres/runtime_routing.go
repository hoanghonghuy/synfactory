package postgres

import (
	"context"
	"fmt"
	"strings"

	runtimepolicy "github.com/hoanghonghuy/synfactory/internal/runtime"
)

// RuntimeRoutingMetrics derives the reproducible scoreboard from durable runs and
// usage cost evidence. The fixed 30-day window is part of runtime-score-v1.
func (s *Store) RuntimeRoutingMetrics(ctx context.Context, request runtimepolicy.RoutingMetricsRequest) (runtimepolicy.RoutingMetrics, error) {
	repository := strings.TrimSpace(request.Repository)
	role := strings.TrimSpace(request.Role)
	runtimeName := strings.TrimSpace(request.Runtime)
	model := strings.TrimSpace(request.Model)
	if repository == "" || role == "" || runtimeName == "" {
		return runtimepolicy.RoutingMetrics{}, fmt.Errorf("runtime routing repository, role, and runtime are required")
	}

	var metrics runtimepolicy.RoutingMetrics
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*),
       COUNT(*) FILTER (WHERE r.status = 'succeeded'),
       COUNT(*) FILTER (WHERE r.status <> 'succeeded'),
       COUNT(*) FILTER (WHERE r.attempt > 1),
       COALESCE(AVG(EXTRACT(EPOCH FROM (r.finished_at - r.started_at)) * 1000)::BIGINT, 0),
       COALESCE(AVG(COALESCE((
           SELECT SUM(u.cost_microusd)
             FROM runtime_usage_ledger AS u
            WHERE u.run_id = r.id
              AND u.repository = repo.full_name
       ), 0))::BIGINT, 0)
  FROM runs AS r
  JOIN jobs AS j ON j.id = r.job_id
  JOIN repositories AS repo ON repo.id = j.repository_id
 WHERE repo.full_name = $1
   AND j.role = $2
   AND r.runtime = $3
   AND COALESCE(r.model, '') = $4
   AND r.finished_at IS NOT NULL
   AND r.started_at >= NOW() - INTERVAL '30 days'`, repository, role, runtimeName, model).Scan(
		&metrics.Attempts,
		&metrics.Successes,
		&metrics.Failures,
		&metrics.Rework,
		&metrics.AverageRuntimeMS,
		&metrics.AverageCostMicroUSD,
	); err != nil {
		return runtimepolicy.RoutingMetrics{}, fmt.Errorf("query runtime routing scoreboard: %w", err)
	}
	return metrics, nil
}
