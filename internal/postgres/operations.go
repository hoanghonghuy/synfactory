package postgres

import (
	"context"
	"fmt"
	"time"
)

type OperationalStats struct {
	QueuedJobs             int64   `json:"queued_jobs"`
	ActiveJobs             int64   `json:"active_jobs"`
	FailedJobs             int64   `json:"failed_jobs"`
	StaleJobLeases         int64   `json:"stale_job_leases"`
	PendingEvents          int64   `json:"pending_events"`
	BlockedWorkflows       int64   `json:"blocked_workflows"`
	ParkedWorkflows        int64   `json:"parked_workflows"`
	LiveWorkers            int64   `json:"live_workers"`
	StaleWorkers           int64   `json:"stale_workers"`
	ActiveWorkflows        int64   `json:"active_workflows"`
	StuckWorkflows         int64   `json:"stuck_workflows"`
	RepairingWorkflows     int64   `json:"repairing_workflows"`
	ExhaustedRepairBudgets int64   `json:"exhausted_repair_budgets"`
	CompletedWorkflows24h  int64   `json:"completed_workflows_24h"`
	RecoveredWorkflows24h  int64   `json:"recovered_workflows_24h"`
	WorkflowActions24h     int64   `json:"workflow_actions_24h"`
	CompletedActions24h    int64   `json:"completed_actions_24h"`
	UsefulWorkRatio24h     float64 `json:"useful_work_ratio_24h"`
}

func (s *Store) OperationalStats(ctx context.Context, now, workerStaleBefore time.Time) (OperationalStats, error) {
	var stats OperationalStats
	windowStart := now.Add(-24 * time.Hour)
	stuckBefore := now.Add(-15 * time.Minute)
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
    (SELECT COUNT(*) FROM workers WHERE last_heartbeat <= $2 OR draining = TRUE),
    (SELECT COUNT(*) FROM workflow_instances WHERE state NOT IN ('completed', 'parked')),
    (SELECT COUNT(*) FROM workflow_instances WHERE state NOT IN ('completed', 'parked', 'blocked') AND updated_at <= $3),
    (SELECT COUNT(*) FROM workflow_instances
      WHERE state NOT IN ('completed', 'parked')
        AND (ci_repair_attempts > 0 OR review_repair_attempts > 0)),
    (SELECT COUNT(*) FROM workflow_instances
      WHERE state = 'parked'
        AND ((ci_repair_limit > 0 AND ci_repair_attempts >= ci_repair_limit)
          OR (review_repair_limit > 0 AND review_repair_attempts >= review_repair_limit))),
    (SELECT COUNT(*) FROM workflow_history WHERE to_state = 'completed' AND created_at >= $4),
    (SELECT COUNT(*) FROM workflow_history
      WHERE from_state IN ('blocked', 'parked')
        AND to_state NOT IN ('blocked', 'parked')
        AND created_at >= $4),
    (SELECT COUNT(*) FROM workflow_actions WHERE created_at >= $4),
    (SELECT COUNT(*) FROM workflow_actions WHERE completed_at IS NOT NULL AND created_at >= $4)`,
		now, workerStaleBefore, stuckBefore, windowStart).Scan(
		&stats.QueuedJobs,
		&stats.ActiveJobs,
		&stats.FailedJobs,
		&stats.StaleJobLeases,
		&stats.PendingEvents,
		&stats.BlockedWorkflows,
		&stats.ParkedWorkflows,
		&stats.LiveWorkers,
		&stats.StaleWorkers,
		&stats.ActiveWorkflows,
		&stats.StuckWorkflows,
		&stats.RepairingWorkflows,
		&stats.ExhaustedRepairBudgets,
		&stats.CompletedWorkflows24h,
		&stats.RecoveredWorkflows24h,
		&stats.WorkflowActions24h,
		&stats.CompletedActions24h,
	)
	if err != nil {
		return OperationalStats{}, fmt.Errorf("load operational stats: %w", err)
	}
	if stats.WorkflowActions24h > 0 {
		stats.UsefulWorkRatio24h = float64(stats.CompletedActions24h) / float64(stats.WorkflowActions24h)
	}
	return stats, nil
}
