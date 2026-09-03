package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type Page struct {
	Limit  int
	Offset int
}

func normalizePage(page Page) Page {
	if page.Limit <= 0 {
		page.Limit = 50
	}
	if page.Limit > 200 {
		page.Limit = 200
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return page
}

type JobFilter struct {
	Status       domain.JobStatus
	RepositoryID string
	Page         Page
}

func (s *Store) ListJobs(ctx context.Context, filter JobFilter) ([]domain.Job, error) {
	page := normalizePage(filter.Page)
	rows, err := s.db.QueryContext(ctx, `
SELECT `+jobColumns+`
FROM jobs
WHERE ($1 = '' OR status = $1)
  AND ($2 = '' OR repository_id = $2)
ORDER BY created_at DESC, id DESC
LIMIT $3 OFFSET $4`, filter.Status, filter.RepositoryID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list control center jobs: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Job, 0, page.Limit)
	for rows.Next() {
		item, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan control center job: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate control center jobs: %w", err)
	}
	return items, nil
}

type WorkflowFilter struct {
	State        workflow.State
	RepositoryID string
	Page         Page
}

func (s *Store) ListWorkflows(ctx context.Context, filter WorkflowFilter) ([]workflow.Instance, error) {
	page := normalizePage(filter.Page)
	rows, err := s.db.QueryContext(ctx, `
SELECT `+workflowColumns+`
FROM workflow_instances
WHERE ($1 = '' OR state = $1)
  AND ($2 = '' OR repository_id = $2)
ORDER BY updated_at DESC, id DESC
LIMIT $3 OFFSET $4`, filter.State, filter.RepositoryID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list control center workflows: %w", err)
	}
	defer rows.Close()

	items := make([]workflow.Instance, 0, page.Limit)
	for rows.Next() {
		item, err := scanWorkflow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan control center workflow: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate control center workflows: %w", err)
	}
	return items, nil
}

type RunFilter struct {
	JobID string
	Page  Page
}

func (s *Store) ListRuns(ctx context.Context, filter RunFilter) ([]Run, error) {
	page := normalizePage(filter.Page)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, attempt, sequence, runtime, COALESCE(model, ''), COALESCE(session_id, ''), status,
       started_at, finished_at, exit_code, COALESCE(summary, ''), metadata
FROM runs
WHERE ($1 = '' OR job_id = $1)
ORDER BY started_at DESC, id DESC
LIMIT $2 OFFSET $3`, filter.JobID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list control center runs: %w", err)
	}
	defer rows.Close()

	items := make([]Run, 0, page.Limit)
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan control center run: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate control center runs: %w", err)
	}
	return items, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	run, err := scanRun(s.db.QueryRowContext(ctx, `
SELECT id, job_id, attempt, sequence, runtime, COALESCE(model, ''), COALESCE(session_id, ''), status,
       started_at, finished_at, exit_code, COALESCE(summary, ''), metadata
FROM runs WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get control center run: %w", err)
	}
	return run, nil
}

func (s *Store) ListEvidence(ctx context.Context, runID string) ([]Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, run_id, kind, name, COALESCE(uri, ''), COALESCE(sha256, ''), metadata, created_at
FROM evidence
WHERE run_id = $1
ORDER BY created_at ASC, id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list run evidence: %w", err)
	}
	defer rows.Close()

	var items []Evidence
	for rows.Next() {
		var item Evidence
		if err := rows.Scan(&item.ID, &item.RunID, &item.Kind, &item.Name, &item.URI, &item.SHA256, &item.Metadata, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan run evidence: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run evidence: %w", err)
	}
	return items, nil
}

func (s *Store) ListWorkers(ctx context.Context) ([]Worker, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, host, capacity, draining, last_heartbeat, started_at, metadata
FROM workers
ORDER BY last_heartbeat DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	defer rows.Close()

	var items []Worker
	for rows.Next() {
		var item Worker
		if err := rows.Scan(&item.ID, &item.Host, &item.Capacity, &item.Draining, &item.LastHeartbeat, &item.StartedAt, &item.Metadata); err != nil {
			return nil, fmt.Errorf("scan worker: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workers: %w", err)
	}
	return items, nil
}

type WorkflowActionRecord struct {
	ID          string
	Kind        workflow.ActionKind
	Role        domain.Role
	Mode        workflow.ActionMode
	TargetState workflow.State
	Revision    string
	BudgetKind  string
	Status      string
	JobID       string
	Decision    string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

func (s *Store) ListWorkflowActions(ctx context.Context, workflowID string) ([]WorkflowActionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, kind, role, mode, target_state, revision, COALESCE(budget_kind, ''), status,
       COALESCE(job_id, ''), COALESCE(decision, ''), created_at, completed_at
FROM workflow_actions
WHERE workflow_id = $1
ORDER BY created_at ASC, id ASC`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("list workflow actions: %w", err)
	}
	defer rows.Close()

	var items []WorkflowActionRecord
	for rows.Next() {
		var item WorkflowActionRecord
		if err := rows.Scan(&item.ID, &item.Kind, &item.Role, &item.Mode, &item.TargetState, &item.Revision, &item.BudgetKind, &item.Status, &item.JobID, &item.Decision, &item.CreatedAt, &item.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan workflow action: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workflow actions: %w", err)
	}
	return items, nil
}

type WorkflowHistoryRecord struct {
	ID        int64
	FromState workflow.State
	ToState   workflow.State
	ActorRole domain.Role
	Reason    string
	CreatedAt time.Time
}

func (s *Store) ListWorkflowHistory(ctx context.Context, workflowID string) ([]WorkflowHistoryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, from_state, to_state, actor_role, COALESCE(reason, ''), created_at
FROM workflow_history
WHERE workflow_id = $1
ORDER BY created_at ASC, id ASC`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("list workflow history: %w", err)
	}
	defer rows.Close()

	var items []WorkflowHistoryRecord
	for rows.Next() {
		var item WorkflowHistoryRecord
		if err := rows.Scan(&item.ID, &item.FromState, &item.ToState, &item.ActorRole, &item.Reason, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan workflow history: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workflow history: %w", err)
	}
	return items, nil
}
