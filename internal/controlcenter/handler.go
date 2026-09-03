package controlcenter

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type Store interface {
	OperationalStats(ctx context.Context, now, workerStaleBefore time.Time) (postgres.OperationalStats, error)
	ListRepositories(ctx context.Context) ([]postgres.Repository, error)
	GetRepository(ctx context.Context, id string) (postgres.Repository, error)
	ListJobs(ctx context.Context, filter postgres.JobFilter) ([]domain.Job, error)
	GetJob(ctx context.Context, id string) (domain.Job, error)
	ListWorkflows(ctx context.Context, filter postgres.WorkflowFilter) ([]workflow.Instance, error)
	GetWorkflow(ctx context.Context, id string) (workflow.Instance, error)
	ListWorkflowActions(ctx context.Context, workflowID string) ([]postgres.WorkflowActionRecord, error)
	ListWorkflowHistory(ctx context.Context, workflowID string) ([]postgres.WorkflowHistoryRecord, error)
	ListRuns(ctx context.Context, filter postgres.RunFilter) ([]postgres.Run, error)
	GetRun(ctx context.Context, id string) (postgres.Run, error)
	ListEvidence(ctx context.Context, runID string) ([]postgres.Evidence, error)
	ListWorkers(ctx context.Context) ([]postgres.Worker, error)
}

type Handler struct {
	Store            Store
	Token            string
	WorkerStaleAfter time.Duration
	Now              func() time.Time
}

func (h Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/overview", h.authorize(http.HandlerFunc(h.overview)))
	mux.Handle("GET /api/v1/repositories", h.authorize(http.HandlerFunc(h.repositories)))
	mux.Handle("GET /api/v1/repositories/{id}", h.authorize(http.HandlerFunc(h.repository)))
	mux.Handle("GET /api/v1/jobs", h.authorize(http.HandlerFunc(h.jobs)))
	mux.Handle("GET /api/v1/jobs/{id}", h.authorize(http.HandlerFunc(h.job)))
	mux.Handle("GET /api/v1/workflows", h.authorize(http.HandlerFunc(h.workflows)))
	mux.Handle("GET /api/v1/workflows/{id}", h.authorize(http.HandlerFunc(h.workflow)))
	mux.Handle("GET /api/v1/runs", h.authorize(http.HandlerFunc(h.runs)))
	mux.Handle("GET /api/v1/runs/{id}", h.authorize(http.HandlerFunc(h.run)))
	mux.Handle("GET /api/v1/runs/{id}/evidence", h.authorize(http.HandlerFunc(h.evidence)))
	mux.Handle("GET /api/v1/workers", h.authorize(http.HandlerFunc(h.workers)))
}

func (h Handler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(h.Token)
		if expected == "" {
			writeError(w, http.StatusServiceUnavailable, "operator_api_disabled")
			return
		}
		authorization := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authorization, prefix) {
			writeError(w, http.StatusUnauthorized, "operator_auth_required")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeError(w, http.StatusUnauthorized, "operator_auth_invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type overviewResponse struct {
	GeneratedAt time.Time                   `json:"generated_at"`
	Stats       postgres.OperationalStats   `json:"stats"`
	Workers     []workerDTO                 `json:"workers"`
	Workflows   []workflowDTO               `json:"attention_workflows"`
}

func (h Handler) overview(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	staleAfter := h.WorkerStaleAfter
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	stats, err := h.Store.OperationalStats(r.Context(), now, now.Add(-staleAfter))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	workers, err := h.Store.ListWorkers(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	blocked, err := h.Store.ListWorkflows(r.Context(), postgres.WorkflowFilter{State: workflow.StateBlocked, Page: postgres.Page{Limit: 20}})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	parked, err := h.Store.ListWorkflows(r.Context(), postgres.WorkflowFilter{State: workflow.StateParked, Page: postgres.Page{Limit: 20}})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	attention := append(blocked, parked...)
	response := overviewResponse{GeneratedAt: now, Stats: stats}
	for _, item := range workers {
		response.Workers = append(response.Workers, toWorkerDTO(item, now, staleAfter))
	}
	for _, item := range attention {
		response.Workflows = append(response.Workflows, toWorkflowDTO(item))
	}
	writeJSON(w, http.StatusOK, response)
}

type repositoryDTO struct {
	ID            string    `json:"id"`
	Provider      string    `json:"provider"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
	Enabled       bool      `json:"enabled"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (h Handler) repositories(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListRepositories(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response := make([]repositoryDTO, 0, len(items))
	for _, item := range items {
		response = append(response, toRepositoryDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func (h Handler) repository(w http.ResponseWriter, r *http.Request) {
	item, err := h.Store.GetRepository(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRepositoryDTO(item))
}

func toRepositoryDTO(item postgres.Repository) repositoryDTO {
	return repositoryDTO{ID: item.ID, Provider: item.Provider, FullName: item.FullName, DefaultBranch: item.DefaultBranch, Enabled: item.Enabled, UpdatedAt: item.UpdatedAt}
}

type jobDTO struct {
	ID           string           `json:"id"`
	RepositoryID string           `json:"repository_id"`
	Kind         string           `json:"kind"`
	Role         domain.Role      `json:"role"`
	Subject      string           `json:"subject"`
	Revision     string           `json:"revision"`
	Priority     int              `json:"priority"`
	Status       domain.JobStatus `json:"status"`
	Attempt      int              `json:"attempt"`
	MaxAttempts  int              `json:"max_attempts"`
	AvailableAt  time.Time        `json:"available_at"`
	LeaseOwner   string           `json:"lease_owner,omitempty"`
	LeaseUntil   *time.Time       `json:"lease_until,omitempty"`
	LastError    string           `json:"last_error,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

func (h Handler) jobs(w http.ResponseWriter, r *http.Request) {
	page := readPage(r)
	items, err := h.Store.ListJobs(r.Context(), postgres.JobFilter{
		Status:       domain.JobStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		RepositoryID: strings.TrimSpace(r.URL.Query().Get("repository_id")),
		Page:         page,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response := make([]jobDTO, 0, len(items))
	for _, item := range items {
		response = append(response, toJobDTO(item))
	}
	writeJSON(w, http.StatusOK, pageResponse[jobDTO]{Items: response, Limit: page.Limit, Offset: page.Offset})
}

func (h Handler) job(w http.ResponseWriter, r *http.Request) {
	item, err := h.Store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toJobDTO(item))
}

func toJobDTO(item domain.Job) jobDTO {
	return jobDTO{
		ID: item.ID, RepositoryID: item.RepositoryID, Kind: item.Kind, Role: item.Role,
		Subject: item.Subject, Revision: item.Revision, Priority: item.Priority, Status: item.Status,
		Attempt: item.Attempt, MaxAttempts: item.MaxAttempts, AvailableAt: item.AvailableAt,
		LeaseOwner: item.LeaseOwner, LeaseUntil: item.LeaseUntil, LastError: item.LastError,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

type workflowDTO struct {
	ID                   string         `json:"id"`
	RepositoryID         string         `json:"repository_id"`
	Kind                 workflow.Kind  `json:"kind"`
	Subject              string         `json:"subject"`
	Revision             string         `json:"revision"`
	State                workflow.State `json:"state"`
	Priority             int            `json:"priority"`
	BlockedReason        string         `json:"blocked_reason,omitempty"`
	CIRepairAttempts     int            `json:"ci_repair_attempts"`
	CIRepairLimit        int            `json:"ci_repair_limit"`
	ReviewRepairAttempts int            `json:"review_repair_attempts"`
	ReviewRepairLimit    int            `json:"review_repair_limit"`
	LastDispatchedAt     *time.Time     `json:"last_dispatched_at,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

func (h Handler) workflows(w http.ResponseWriter, r *http.Request) {
	page := readPage(r)
	items, err := h.Store.ListWorkflows(r.Context(), postgres.WorkflowFilter{
		State:        workflow.State(strings.TrimSpace(r.URL.Query().Get("state"))),
		RepositoryID: strings.TrimSpace(r.URL.Query().Get("repository_id")),
		Page:         page,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response := make([]workflowDTO, 0, len(items))
	for _, item := range items {
		response = append(response, toWorkflowDTO(item))
	}
	writeJSON(w, http.StatusOK, pageResponse[workflowDTO]{Items: response, Limit: page.Limit, Offset: page.Offset})
}

type workflowDetailDTO struct {
	Workflow workflowDTO                      `json:"workflow"`
	Actions  []postgres.WorkflowActionRecord  `json:"actions"`
	History  []postgres.WorkflowHistoryRecord `json:"history"`
}

func (h Handler) workflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, err := h.Store.GetWorkflow(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	actions, err := h.Store.ListWorkflowActions(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	history, err := h.Store.ListWorkflowHistory(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowDetailDTO{Workflow: toWorkflowDTO(item), Actions: actions, History: history})
}

func toWorkflowDTO(item workflow.Instance) workflowDTO {
	return workflowDTO{
		ID: item.ID, RepositoryID: item.RepositoryID, Kind: item.Kind, Subject: item.Subject,
		Revision: item.Revision, State: item.State, Priority: item.Priority, BlockedReason: item.BlockedReason,
		CIRepairAttempts: item.CIRepairAttempts, CIRepairLimit: item.CIRepairLimit,
		ReviewRepairAttempts: item.ReviewRepairAttempts, ReviewRepairLimit: item.ReviewRepairLimit,
		LastDispatchedAt: item.LastDispatchedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

type runDTO struct {
	ID         string     `json:"id"`
	JobID      string     `json:"job_id"`
	Attempt    int        `json:"attempt"`
	Sequence   int        `json:"sequence"`
	Runtime    string     `json:"runtime"`
	Model      string     `json:"model,omitempty"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	Summary    string     `json:"summary,omitempty"`
}

func (h Handler) runs(w http.ResponseWriter, r *http.Request) {
	page := readPage(r)
	items, err := h.Store.ListRuns(r.Context(), postgres.RunFilter{JobID: strings.TrimSpace(r.URL.Query().Get("job_id")), Page: page})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response := make([]runDTO, 0, len(items))
	for _, item := range items {
		response = append(response, toRunDTO(item))
	}
	writeJSON(w, http.StatusOK, pageResponse[runDTO]{Items: response, Limit: page.Limit, Offset: page.Offset})
}

func (h Handler) run(w http.ResponseWriter, r *http.Request) {
	item, err := h.Store.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRunDTO(item))
}

func toRunDTO(item postgres.Run) runDTO {
	return runDTO{ID: item.ID, JobID: item.JobID, Attempt: item.Attempt, Sequence: item.Sequence, Runtime: item.Runtime, Model: item.Model, Status: item.Status, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, ExitCode: item.ExitCode, Summary: item.Summary}
}

type evidenceDTO struct {
	ID        int64     `json:"id"`
	RunID     string    `json:"run_id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	URI       string    `json:"uri,omitempty"`
	SHA256    string    `json:"sha256,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (h Handler) evidence(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListEvidence(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response := make([]evidenceDTO, 0, len(items))
	for _, item := range items {
		response = append(response, evidenceDTO{ID: item.ID, RunID: item.RunID, Kind: item.Kind, Name: item.Name, URI: item.URI, SHA256: item.SHA256, CreatedAt: item.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

type workerDTO struct {
	ID            string    `json:"id"`
	Host          string    `json:"host"`
	Capacity      int       `json:"capacity"`
	Draining      bool      `json:"draining"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	StartedAt     time.Time `json:"started_at"`
	Healthy       bool      `json:"healthy"`
}

func (h Handler) workers(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListWorkers(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	staleAfter := h.WorkerStaleAfter
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	response := make([]workerDTO, 0, len(items))
	for _, item := range items {
		response = append(response, toWorkerDTO(item, now, staleAfter))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func toWorkerDTO(item postgres.Worker, now time.Time, staleAfter time.Duration) workerDTO {
	return workerDTO{ID: item.ID, Host: item.Host, Capacity: item.Capacity, Draining: item.Draining, LastHeartbeat: item.LastHeartbeat, StartedAt: item.StartedAt, Healthy: !item.Draining && item.LastHeartbeat.After(now.Add(-staleAfter))}
}

type pageResponse[T any] struct {
	Items  []T `json:"items"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func readPage(r *http.Request) postgres.Page {
	return postgres.Page{Limit: readBoundedInt(r.URL.Query().Get("limit"), 50, 1, 200), Offset: readBoundedInt(r.URL.Query().Get("offset"), 0, 0, 1_000_000)}
}

func readBoundedInt(raw string, fallback, min, max int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "data_unavailable")
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
