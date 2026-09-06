package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type StatsStore interface {
	OperationalStats(ctx context.Context, now, workerStaleBefore time.Time) (postgres.OperationalStats, error)
}

type Handler struct {
	Store            StatsStore
	WorkerStaleAfter time.Duration
	Now              func() time.Time
}

func (h Handler) Stats(ctx context.Context) (postgres.OperationalStats, error) {
	if h.Store == nil {
		return postgres.OperationalStats{}, fmt.Errorf("operations stats store is required")
	}
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	staleAfter := h.WorkerStaleAfter
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	return h.Store.OperationalStats(ctx, now, now.Add(-staleAfter))
}

func (h Handler) JSON(w http.ResponseWriter, r *http.Request) {
	stats, err := h.Stats(r.Context())
	if err != nil {
		http.Error(w, "operational stats unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (h Handler) Prometheus(w http.ResponseWriter, r *http.Request) {
	stats, err := h.Stats(r.Context())
	if err != nil {
		http.Error(w, "operational metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writeGauge(w, "synfactory_jobs_queued", "Jobs waiting for execution or retry.", stats.QueuedJobs)
	writeGauge(w, "synfactory_jobs_active", "Jobs currently leased or running.", stats.ActiveJobs)
	writeGauge(w, "synfactory_jobs_failed", "Jobs in terminal failed state.", stats.FailedJobs)
	writeGauge(w, "synfactory_job_leases_stale", "Leased or running jobs whose lease has expired.", stats.StaleJobLeases)
	writeGauge(w, "synfactory_events_pending", "Durable inbox events not yet completed.", stats.PendingEvents)
	writeGauge(w, "synfactory_workflows_blocked", "Workflows currently blocked.", stats.BlockedWorkflows)
	writeGauge(w, "synfactory_workflows_parked", "Workflows parked after bounded recovery was exhausted.", stats.ParkedWorkflows)
	writeGauge(w, "synfactory_workers_live", "Workers with a fresh heartbeat and not draining.", stats.LiveWorkers)
	writeGauge(w, "synfactory_workers_stale", "Workers with stale heartbeat or draining state.", stats.StaleWorkers)
	writeGauge(w, "synfactory_autonomy_workflows_active", "Non-terminal workflows currently under autonomous control.", stats.ActiveWorkflows)
	writeGauge(w, "synfactory_autonomy_workflows_stuck", "Runnable workflows with no state update for at least 15 minutes.", stats.StuckWorkflows)
	writeGauge(w, "synfactory_autonomy_workflows_repairing", "Active workflows that have consumed at least one bounded CI or review repair attempt.", stats.RepairingWorkflows)
	writeGauge(w, "synfactory_autonomy_repair_budgets_exhausted", "Parked workflows whose configured CI or review repair budget is exhausted.", stats.ExhaustedRepairBudgets)
	writeGauge(w, "synfactory_autonomy_workflows_completed_24h", "Workflow completion transitions recorded in the last 24 hours.", stats.CompletedWorkflows24h)
	writeGauge(w, "synfactory_autonomy_workflows_recovered_24h", "Transitions out of blocked or parked state recorded in the last 24 hours.", stats.RecoveredWorkflows24h)
	writeGauge(w, "synfactory_autonomy_actions_24h", "Workflow actions created in the last 24 hours.", stats.WorkflowActions24h)
	writeGauge(w, "synfactory_autonomy_actions_completed_24h", "Workflow actions completed in the last 24-hour cohort.", stats.CompletedActions24h)
	writeFloatGauge(w, "synfactory_autonomy_useful_work_ratio_24h", "Fraction of the last 24-hour action cohort that completed.", stats.UsefulWorkRatio24h)
}

func writeGauge(w http.ResponseWriter, name, help string, value int64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, value)
}

func writeFloatGauge(w http.ResponseWriter, name, help string, value float64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %.6f\n", name, help, name, name, value)
}
