package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

const workflowColumns = `
id, dedupe_key, repository_id, kind, subject, revision, state, priority,
COALESCE(blocked_reason, ''), ci_repair_attempts, ci_repair_limit,
review_repair_attempts, review_repair_limit, last_dispatched_at,
metadata, created_at, updated_at`

func (s *Store) UpsertWorkflow(ctx context.Context, instance workflow.Instance) (workflow.Instance, error) {
	if instance.ID == "" || instance.DedupeKey == "" || instance.RepositoryID == "" || instance.Kind == "" || instance.Subject == "" {
		return workflow.Instance{}, fmt.Errorf("workflow id, dedupe key, repository, kind and subject are required")
	}
	if instance.State == "" {
		instance.State = workflow.StateDiscovered
	}
	if instance.Priority == 0 {
		instance.Priority = 100
	}
	if instance.CIRepairLimit <= 0 {
		instance.CIRepairLimit = 2
	}
	if instance.ReviewRepairLimit <= 0 {
		instance.ReviewRepairLimit = 2
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO workflow_instances (
    id, dedupe_key, repository_id, kind, subject, revision, state, priority,
    ci_repair_limit, review_repair_limit, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (dedupe_key) DO UPDATE SET
    revision = EXCLUDED.revision,
    priority = EXCLUDED.priority,
    state = CASE
        WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision
             AND workflow_instances.kind = 'repository' THEN 'discovered'
        WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision
             AND workflow_instances.state = 'blocked' THEN 'discovered'
        ELSE workflow_instances.state
    END,
    blocked_reason = CASE
        WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision THEN NULL
        ELSE workflow_instances.blocked_reason
    END,
    ci_repair_attempts = CASE
        WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision THEN 0
        ELSE workflow_instances.ci_repair_attempts
    END,
    review_repair_attempts = CASE
        WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision THEN 0
        ELSE workflow_instances.review_repair_attempts
    END,
    metadata = workflow_instances.metadata || EXCLUDED.metadata,
    updated_at = NOW()
RETURNING `+workflowColumns,
		instance.ID, instance.DedupeKey, instance.RepositoryID, instance.Kind, instance.Subject,
		instance.Revision, instance.State, instance.Priority, instance.CIRepairLimit,
		instance.ReviewRepairLimit, jsonOrEmpty(instance.Metadata),
	)
	created, err := scanWorkflow(row)
	if err != nil {
		return workflow.Instance{}, fmt.Errorf("upsert workflow: %w", err)
	}
	return created, nil
}

func (s *Store) GetWorkflow(ctx context.Context, id string) (workflow.Instance, error) {
	item, err := scanWorkflow(s.db.QueryRowContext(ctx, `SELECT `+workflowColumns+` FROM workflow_instances WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return workflow.Instance{}, ErrNotFound
	}
	if err != nil {
		return workflow.Instance{}, fmt.Errorf("get workflow: %w", err)
	}
	return item, nil
}

func (s *Store) AddWorkflowDependency(ctx context.Context, workflowID, dependsOnID string) error {
	if workflowID == "" || dependsOnID == "" || workflowID == dependsOnID {
		return fmt.Errorf("valid distinct workflow ids are required")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO workflow_dependencies (workflow_id, depends_on_id)
VALUES ($1, $2)
ON CONFLICT (workflow_id, depends_on_id) DO NOTHING`, workflowID, dependsOnID)
	if err != nil {
		return fmt.Errorf("add workflow dependency: %w", err)
	}
	return nil
}

func (s *Store) DependenciesSatisfied(ctx context.Context, workflowID string) (bool, error) {
	var satisfied bool
	err := s.db.QueryRowContext(ctx, `
SELECT NOT EXISTS (
    SELECT 1
    FROM workflow_dependencies d
    JOIN workflow_instances dependency ON dependency.id = d.depends_on_id
    WHERE d.workflow_id = $1
      AND dependency.state <> d.required_state
)`, workflowID).Scan(&satisfied)
	if err != nil {
		return false, fmt.Errorf("check workflow dependencies: %w", err)
	}
	return satisfied, nil
}

func (s *Store) ActionSucceeded(ctx context.Context, workflowID string, kind workflow.ActionKind, revision string) (bool, error) {
	var succeeded bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM workflow_actions a
    JOIN jobs j ON j.id = a.job_id
    WHERE a.workflow_id = $1
      AND a.kind = $2
      AND a.revision = $3
      AND j.status = 'succeeded'
)`, workflowID, kind, revision).Scan(&succeeded)
	if err != nil {
		return false, fmt.Errorf("check workflow action outcome: %w", err)
	}
	return succeeded, nil
}

func (s *Store) LatestActionStatus(ctx context.Context, workflowID string, kind workflow.ActionKind, revision string) (domain.JobStatus, bool, error) {
	var status domain.JobStatus
	err := s.db.QueryRowContext(ctx, `
SELECT j.status
FROM workflow_actions a
JOIN jobs j ON j.id = a.job_id
WHERE a.workflow_id = $1
  AND a.kind = $2
  AND a.revision = $3
ORDER BY a.created_at DESC
LIMIT 1`, workflowID, kind, revision).Scan(&status)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load latest workflow action status: %w", err)
	}
	return status, true, nil
}

func (s *Store) ActiveRoleCount(ctx context.Context, role domain.Role) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM jobs
WHERE role = $1
  AND status IN ('queued', 'leased', 'running', 'retry_wait')`, role).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active role jobs: %w", err)
	}
	return count, nil
}

func (s *Store) ApplyDecision(ctx context.Context, workflowID string, decision workflow.Decision, actor domain.Role, now time.Time) (workflow.Instance, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workflow.Instance{}, fmt.Errorf("begin workflow decision: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	current, err := lockWorkflow(ctx, tx, workflowID)
	if err != nil {
		return workflow.Instance{}, err
	}
	if !workflow.CanTransition(actor, current.State, decision.TargetState) {
		return workflow.Instance{}, workflow.ErrUnauthorizedActor
	}
	updated, err := updateWorkflowState(ctx, tx, current, decision, actor, now, false)
	if err != nil {
		return workflow.Instance{}, err
	}
	if err := tx.Commit(); err != nil {
		return workflow.Instance{}, fmt.Errorf("commit workflow decision: %w", err)
	}
	return updated, nil
}

func (s *Store) DispatchAction(ctx context.Context, workflowID string, decision workflow.Decision, job workflow.JobSpec, actor domain.Role, now time.Time) (workflow.Instance, workflow.DispatchResult, error) {
	if decision.Action == nil || decision.Action.Mode != workflow.ActionJob {
		return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("job workflow action is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("begin workflow dispatch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	current, err := lockWorkflow(ctx, tx, workflowID)
	if err != nil {
		return workflow.Instance{}, workflow.DispatchResult{}, err
	}
	if !workflow.CanTransition(actor, current.State, decision.TargetState) {
		return workflow.Instance{}, workflow.DispatchResult{}, workflow.ErrUnauthorizedActor
	}

	actionID := deterministicID("wfa", decision.Action.Key)
	var existingJobID sql.NullString
	err = tx.QueryRowContext(ctx, `
INSERT INTO workflow_actions (id, workflow_id, action_key, kind, role, mode, target_state, revision, budget_kind, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10)
ON CONFLICT (action_key) DO NOTHING
RETURNING job_id`, actionID, workflowID, decision.Action.Key, decision.Action.Kind, decision.Action.Role,
		decision.Action.Mode, decision.Action.TargetState, current.Revision, decision.Action.Budget, jsonOrEmptyStringMap(decision.Action.Metadata)).Scan(&existingJobID)
	if err == sql.ErrNoRows {
		var jobID sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT job_id FROM workflow_actions WHERE action_key = $1`, decision.Action.Key).Scan(&jobID); err != nil {
			return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("load existing workflow action: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("commit duplicate workflow dispatch: %w", err)
		}
		return current, workflow.DispatchResult{JobID: jobID.String, Dispatched: false}, nil
	}
	if err != nil {
		return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("create workflow action: %w", err)
	}

	if job.ID == "" || job.DedupeKey == "" || job.RepositoryID == "" || job.Role == "" || job.Subject == "" {
		return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("workflow job id, dedupe key, repository, role and subject are required")
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 1
	}
	if job.AvailableAt.IsZero() {
		job.AvailableAt = now
	}
	if job.Priority == 0 {
		job.Priority = 100
	}
	var jobID string
	err = tx.QueryRowContext(ctx, `
INSERT INTO jobs (id, dedupe_key, repository_id, kind, role, subject, revision, priority, status, max_attempts, available_at, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'queued',$9,$10,$11)
ON CONFLICT (dedupe_key) DO UPDATE SET dedupe_key = EXCLUDED.dedupe_key
RETURNING id`, job.ID, job.DedupeKey, job.RepositoryID, job.Kind, job.Role, job.Subject,
		job.Revision, job.Priority, job.MaxAttempts, job.AvailableAt, jsonOrEmpty(job.Metadata)).Scan(&jobID)
	if err != nil {
		return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("create workflow job: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE workflow_actions SET status = 'dispatched', job_id = $2 WHERE action_key = $1`, decision.Action.Key, jobID); err != nil {
		return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("link workflow action job: %w", err)
	}
	updated, err := updateWorkflowState(ctx, tx, current, decision, actor, now, true)
	if err != nil {
		return workflow.Instance{}, workflow.DispatchResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return workflow.Instance{}, workflow.DispatchResult{}, fmt.Errorf("commit workflow dispatch: %w", err)
	}
	return updated, workflow.DispatchResult{JobID: jobID, Dispatched: true}, nil
}

func (s *Store) RegisterTask(ctx context.Context, repositoryID, fingerprint string, issueNumber int64, state string, seenAt time.Time) (bool, error) {
	if repositoryID == "" || fingerprint == "" || issueNumber <= 0 || state == "" {
		return false, fmt.Errorf("repository, fingerprint, issue number and state are required")
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO task_registry (repository_id, fingerprint, issue_number, state, first_seen_at, last_seen_at)
VALUES ($1,$2,$3,$4,$5,$5)
ON CONFLICT (repository_id, fingerprint) DO NOTHING`, repositoryID, fingerprint, issueNumber, state, seenAt)
	if err != nil {
		return false, fmt.Errorf("register task fingerprint: %w", err)
	}
	created, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count task fingerprint insert: %w", err)
	}
	if created == 0 {
		if _, err := s.db.ExecContext(ctx, `
UPDATE task_registry SET issue_number = $3, state = $4, last_seen_at = $5
WHERE repository_id = $1 AND fingerprint = $2`, repositoryID, fingerprint, issueNumber, state, seenAt); err != nil {
			return false, fmt.Errorf("refresh task fingerprint: %w", err)
		}
	}
	return created == 1, nil
}

func lockWorkflow(ctx context.Context, tx *sql.Tx, id string) (workflow.Instance, error) {
	item, err := scanWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowColumns+` FROM workflow_instances WHERE id = $1 FOR UPDATE`, id))
	if err == sql.ErrNoRows {
		return workflow.Instance{}, ErrNotFound
	}
	if err != nil {
		return workflow.Instance{}, fmt.Errorf("lock workflow: %w", err)
	}
	return item, nil
}

func updateWorkflowState(ctx context.Context, tx *sql.Tx, current workflow.Instance, decision workflow.Decision, actor domain.Role, now time.Time, dispatched bool) (workflow.Instance, error) {
	ciIncrement := 0
	reviewIncrement := 0
	if dispatched && decision.Action != nil {
		switch decision.Action.Budget {
		case workflow.BudgetCIRepair:
			ciIncrement = 1
		case workflow.BudgetReviewRepair:
			reviewIncrement = 1
		}
	}
	row := tx.QueryRowContext(ctx, `
UPDATE workflow_instances
SET state = $2,
    blocked_reason = NULLIF($3, ''),
    ci_repair_attempts = ci_repair_attempts + $4,
    review_repair_attempts = review_repair_attempts + $5,
    last_dispatched_at = CASE WHEN $6 THEN $7 ELSE last_dispatched_at END,
    updated_at = $7
WHERE id = $1
RETURNING `+workflowColumns, current.ID, decision.TargetState, decision.BlockedReason,
		ciIncrement, reviewIncrement, dispatched, now)
	updated, err := scanWorkflow(row)
	if err != nil {
		return workflow.Instance{}, fmt.Errorf("update workflow state: %w", err)
	}
	if current.State != decision.TargetState || decision.Reason != "" {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO workflow_history (workflow_id, from_state, to_state, actor_role, reason)
VALUES ($1,$2,$3,$4,NULLIF($5,''))`, current.ID, current.State, decision.TargetState, actor, decision.Reason); err != nil {
			return workflow.Instance{}, fmt.Errorf("record workflow history: %w", err)
		}
	}
	return updated, nil
}

func scanWorkflow(row rowScanner) (workflow.Instance, error) {
	var item workflow.Instance
	if err := row.Scan(
		&item.ID,
		&item.DedupeKey,
		&item.RepositoryID,
		&item.Kind,
		&item.Subject,
		&item.Revision,
		&item.State,
		&item.Priority,
		&item.BlockedReason,
		&item.CIRepairAttempts,
		&item.CIRepairLimit,
		&item.ReviewRepairAttempts,
		&item.ReviewRepairLimit,
		&item.LastDispatchedAt,
		&item.Metadata,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return workflow.Instance{}, err
	}
	return item, nil
}

func deterministicID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + "_" + hex.EncodeToString(sum[:12])
}

func jsonOrEmptyStringMap(value map[string]string) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func (s *Store) ReserveTask(ctx context.Context, repositoryID, fingerprint, owner string, now time.Time, ttl time.Duration) (bool, error) {
	if repositoryID == "" || fingerprint == "" || owner == "" || ttl <= 0 {
		return false, fmt.Errorf("repository, fingerprint, owner and positive ttl are required")
	}
	until := now.Add(ttl)
	result, err := s.db.ExecContext(ctx, `
INSERT INTO task_registry (
    repository_id, fingerprint, state, reservation_owner, reservation_until, first_seen_at, last_seen_at
) VALUES ($1,$2,'reserved',$3,$4,$5,$5)
ON CONFLICT (repository_id, fingerprint) DO UPDATE SET
    reservation_owner = EXCLUDED.reservation_owner,
    reservation_until = EXCLUDED.reservation_until,
    state = 'reserved',
    last_seen_at = EXCLUDED.last_seen_at
WHERE task_registry.issue_number IS NULL
  AND (task_registry.reservation_owner = EXCLUDED.reservation_owner
       OR task_registry.reservation_until IS NULL
       OR task_registry.reservation_until <= $5)`,
		repositoryID, fingerprint, owner, until, now)
	if err != nil {
		return false, fmt.Errorf("reserve task fingerprint: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count task reservation: %w", err)
	}
	return rows == 1, nil
}

func (s *Store) BindTask(ctx context.Context, repositoryID, fingerprint, owner string, issueNumber int64, state string, seenAt time.Time) error {
	if repositoryID == "" || fingerprint == "" || owner == "" || issueNumber <= 0 || state == "" {
		return fmt.Errorf("repository, fingerprint, owner, issue number and state are required")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE task_registry
SET issue_number = $4,
    state = $5,
    reservation_owner = NULL,
    reservation_until = NULL,
    last_seen_at = $6
WHERE repository_id = $1
  AND fingerprint = $2
  AND issue_number IS NULL
  AND reservation_owner = $3
  AND reservation_until > $6`, repositoryID, fingerprint, owner, issueNumber, state, seenAt)
	if err != nil {
		return fmt.Errorf("bind task fingerprint: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count task bind: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("task reservation is missing, expired or owned by another actor")
	}
	return nil
}

func (s *Store) ListActiveWorkflows(ctx context.Context) ([]workflow.Instance, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+workflowColumns+`
FROM workflow_instances
WHERE state NOT IN ('completed', 'parked')
ORDER BY priority DESC, last_dispatched_at NULLS FIRST, created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list active workflows: %w", err)
	}
	defer rows.Close()
	var items []workflow.Instance
	for rows.Next() {
		item, err := scanWorkflow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan active workflow: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active workflows: %w", err)
	}
	return items, nil
}

func (s *Store) LatestActionDecision(ctx context.Context, workflowID string, kind workflow.ActionKind, revision string) (string, bool, error) {
	var decision string
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(a.decision, '')
FROM workflow_actions a
JOIN jobs j ON j.id = a.job_id
WHERE a.workflow_id = $1
  AND a.kind = $2
  AND a.revision = $3
  AND j.status = 'succeeded'
  AND a.decision IS NOT NULL
ORDER BY a.completed_at DESC NULLS LAST, a.created_at DESC
LIMIT 1`, workflowID, kind, revision).Scan(&decision)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load latest workflow action decision: %w", err)
	}
	return decision, decision != "", nil
}

func (s *Store) RecordWorkflowHandoff(ctx context.Context, jobID, decision string, metadata json.RawMessage, completedAt time.Time) error {
	if jobID == "" || decision == "" {
		return fmt.Errorf("job id and handoff decision are required")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE workflow_actions
SET decision = $2,
    status = 'completed',
    metadata = metadata || $3,
    completed_at = COALESCE(completed_at, $4)
WHERE job_id = $1`, jobID, decision, jsonOrEmpty(metadata), completedAt)
	if err != nil {
		return fmt.Errorf("record workflow handoff: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count workflow handoff update: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("workflow action for job %s not found", jobID)
	}
	return nil
}
