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
		QueuedJobs: 3, StaleJobLeases: 1, BlockedWorkflows: 2, LiveWorkers: 4,
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
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q: %s", expected, body)
		}
	}
}
