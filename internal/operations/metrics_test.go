package operations

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type statsStoreStub struct {
	stats postgres.OperationalStats
}

func (s statsStoreStub) OperationalStats(context.Context, time.Time, time.Time) (postgres.OperationalStats, error) {
	return s.stats, nil
}

func TestPrometheusMetricsExposeOperationalState(t *testing.T) {
	handler := Handler{Store: statsStoreStub{stats: postgres.OperationalStats{
		QueuedJobs:             3,
		StaleJobLeases:         1,
		BlockedWorkflows:       2,
		LiveWorkers:            4,
		ActiveWorkflows:        7,
		StuckWorkflows:         1,
		RepairingWorkflows:     2,
		ExhaustedRepairBudgets: 1,
		CompletedWorkflows24h:  5,
		RecoveredWorkflows24h:  2,
		WorkflowActions24h:     10,
		CompletedActions24h:    8,
		UsefulWorkRatio24h:     0.8,
	}}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/metrics", nil)
	handler.Prometheus(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		"synfactory_jobs_queued 3",
		"synfactory_job_leases_stale 1",
		"synfactory_workflows_blocked 2",
		"synfactory_workers_live 4",
		"synfactory_autonomy_workflows_active 7",
		"synfactory_autonomy_workflows_stuck 1",
		"synfactory_autonomy_workflows_repairing 2",
		"synfactory_autonomy_repair_budgets_exhausted 1",
		"synfactory_autonomy_workflows_completed_24h 5",
		"synfactory_autonomy_workflows_recovered_24h 2",
		"synfactory_autonomy_actions_24h 10",
		"synfactory_autonomy_actions_completed_24h 8",
		"synfactory_autonomy_useful_work_ratio_24h 0.800000",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q: %s", expected, body)
		}
	}
}
