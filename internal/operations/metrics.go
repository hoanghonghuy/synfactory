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
}

func writeGauge(w http.ResponseWriter, name, help string, value int64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, value)
}
