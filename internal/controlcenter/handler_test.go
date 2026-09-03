package controlcenter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type testStore struct {
	repositories []postgres.Repository
}

func (s testStore) OperationalStats(context.Context, time.Time, time.Time) (postgres.OperationalStats, error) {
	return postgres.OperationalStats{}, nil
}
func (s testStore) ListRepositories(context.Context) ([]postgres.Repository, error) {
	return s.repositories, nil
}
func (s testStore) GetRepository(_ context.Context, id string) (postgres.Repository, error) {
	for _, item := range s.repositories {
		if item.ID == id {
			return item, nil
		}
	}
	return postgres.Repository{}, postgres.ErrNotFound
}
func (testStore) ListJobs(context.Context, postgres.JobFilter) ([]domain.Job, error) { return nil, nil }
func (testStore) GetJob(context.Context, string) (domain.Job, error) {
	return domain.Job{}, postgres.ErrNotFound
}
func (testStore) ListWorkflows(context.Context, postgres.WorkflowFilter) ([]workflow.Instance, error) {
	return nil, nil
}
func (testStore) GetWorkflow(context.Context, string) (workflow.Instance, error) {
	return workflow.Instance{}, postgres.ErrNotFound
}
func (testStore) ListWorkflowActions(context.Context, string) ([]postgres.WorkflowActionRecord, error) {
	return nil, nil
}
func (testStore) ListWorkflowHistory(context.Context, string) ([]postgres.WorkflowHistoryRecord, error) {
	return nil, nil
}
func (testStore) ListRuns(context.Context, postgres.RunFilter) ([]postgres.Run, error) { return nil, nil }
func (testStore) GetRun(context.Context, string) (postgres.Run, error) {
	return postgres.Run{}, postgres.ErrNotFound
}
func (testStore) ListEvidence(context.Context, string) ([]postgres.Evidence, error) { return nil, nil }
func (testStore) ListWorkers(context.Context) ([]postgres.Worker, error)             { return nil, nil }

func TestOperatorAPIDisabledWithoutToken(t *testing.T) {
	mux := http.NewServeMux()
	Handler{Store: testStore{}}.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), "operator_api_disabled") {
		t.Fatalf("unexpected response: code=%d body=%s", res.Code, res.Body.String())
	}
}

func TestOperatorAPIRejectsInvalidBearerToken(t *testing.T) {
	mux := http.NewServeMux()
	Handler{Store: testStore{}, Token: "correct-secret"}.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil)
	req.Header.Set("Authorization", "Bearer wrong-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestRepositoryResponseDoesNotExposeRepositoryConfig(t *testing.T) {
	mux := http.NewServeMux()
	store := testStore{repositories: []postgres.Repository{{
		ID: "github:1", Provider: "github", FullName: "owner/repo", DefaultBranch: "develop", Enabled: true,
		Config: json.RawMessage(`{"clone_url":"https://secret.invalid/repo","api_key":"must-not-leak"}`),
		UpdatedAt: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
	}}}
	Handler{Store: store, Token: "operator-secret"}.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if strings.Contains(body, "must-not-leak") || strings.Contains(body, "clone_url") || strings.Contains(body, "Config") {
		t.Fatalf("repository config leaked: %s", body)
	}
	if !strings.Contains(body, "owner/repo") || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected safe response: %s", body)
	}
}

func TestPaginationIsBounded(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?limit=9999&offset=-3", nil)
	page := readPage(req)
	if page.Limit != 200 || page.Offset != 0 {
		t.Fatalf("unexpected page: %+v", page)
	}
}
