package postgres

import (
	"context"
	"fmt"
	"time"
)

type OperationalStats struct {
	QueuedJobs       int64 `json:"queued_jobs"`
	ActiveJobs       int64 `json:"active_jobs"`
	FailedJobs       int64 `json:"failed_jobs"`
	StaleJobLeases   int64 `json:"stale_job_leases"`
	PendingEvents    int64 `json:"pending_events"`
	BlockedWorkflows int64 `json:"blocked_workflows"`
	ParkedWorkflows  int64 `json:"parked_workflows"`
	LiveWorkers      int64 `json:"live_workers"`
	StaleWorkers     int64 `json:"stale_workers"`
}

func (s *Store) OperationalStats(ctx context.Context, now, workerStaleBefore time.Time) (OperationalStats, error) {
	var stats OperationalStats
	err := s.db.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM jobs WHERE status IN ('queued', 'retry_wait')),
    (SELECT COUNT(*) FROM jobs WHERE status IN ('leased', 'running')),
    (SELECT COUNT(*) FROM jobs WHERE status = 'failed'),
    (SELECT COUNT(*) FROM jobs WHERE status IN ('leased', 'running') AND lease_until IS NOT NULL AND lease_until <= $1),
    (SELECT COUNT(*) FROM event_inbox WHERE processed_at IS NULL),
    (SELECT COUNT(*) FROM workflow_instances WHERE state = 'blocked'),
    (SELECT COUNT(*) FROM workflow_instances WHERE state = 'parked'),
    (SELECT COUNT(*) FROM workers WHERE last_heartbeat > $2 AND draining = FALSE),
    (SELECT COUNT(*) FROM workers WHERE last_heartbeat <= $2 OR draining = TRUE)`, now, workerStaleBefore).Scan(
		&stats.QueuedJobs,
		&stats.ActiveJobs,
		&stats.FailedJobs,
		&stats.StaleJobLeases,
		&stats.PendingEvents,
		&stats.BlockedWorkflows,
		&stats.ParkedWorkflows,
		&stats.LiveWorkers,
		&stats.StaleWorkers,
	)
	if err != nil {
		return OperationalStats{}, fmt.Errorf("load operational stats: %w", err)
	}
	return stats, nil
}
