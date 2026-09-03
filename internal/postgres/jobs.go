package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

const jobColumns = `
id, repository_id, kind, role, subject, revision, priority, status,
attempt, max_attempts, available_at, COALESCE(lease_owner, ''), lease_until,
COALESCE(last_error, ''), created_at, updated_at`

const qualifiedJobColumns = `
j.id, j.repository_id, j.kind, j.role, j.subject, j.revision, j.priority, j.status,
j.attempt, j.max_attempts, j.available_at, COALESCE(j.lease_owner, ''), j.lease_until,
COALESCE(j.last_error, ''), j.created_at, j.updated_at`

func (s *Store) CreateJob(ctx context.Context, job NewJob) (domain.Job, bool, error) {
	if job.ID == "" || job.DedupeKey == "" || job.RepositoryID == "" || job.Kind == "" || job.Role == "" || job.Subject == "" {
		return domain.Job{}, false, fmt.Errorf("job id, dedupe key, repository, kind, role and subject are required")
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 3
	}
	if job.Priority == 0 {
		job.Priority = 100
	}
	if job.AvailableAt.IsZero() {
		job.AvailableAt = time.Now().UTC()
	}

	row := s.db.QueryRowContext(ctx, `
INSERT INTO jobs (
    id, dedupe_key, repository_id, source_event_id, kind, role, subject, revision,
    priority, status, max_attempts, available_at, metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'queued', $10, $11, $12)
ON CONFLICT (dedupe_key) DO NOTHING
RETURNING `+jobColumns,
		job.ID,
		job.DedupeKey,
		job.RepositoryID,
		job.SourceEventID,
		job.Kind,
		job.Role,
		job.Subject,
		job.Revision,
		job.Priority,
		job.MaxAttempts,
		job.AvailableAt,
		jsonOrEmpty(job.Metadata),
	)
	created, err := scanJob(row)
	if err == nil {
		return created, true, nil
	}
	if err != sql.ErrNoRows {
		return domain.Job{}, false, fmt.Errorf("create job: %w", err)
	}

	existingRow := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE dedupe_key = $1`, job.DedupeKey)
	existing, err := scanJob(existingRow)
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("load existing job: %w", err)
	}
	return existing, false, nil
}

func (s *Store) GetJob(ctx context.Context, id string) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return domain.Job{}, ErrNotFound
	}
	return job, err
}

func (s *Store) ClaimJob(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) (domain.Job, bool, error) {
	if workerID == "" || leaseDuration <= 0 {
		return domain.Job{}, false, fmt.Errorf("worker id and positive lease duration are required")
	}
	leaseUntil := now.Add(leaseDuration)

	row := s.db.QueryRowContext(ctx, `
WITH candidate AS (
    SELECT id
    FROM jobs
    WHERE status IN ('queued', 'retry_wait')
      AND available_at <= $1
    ORDER BY priority DESC, available_at ASC, created_at ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE jobs AS j
SET status = 'leased', lease_owner = $2, lease_until = $3, updated_at = $1
FROM candidate AS c
WHERE j.id = c.id
RETURNING `+qualifiedJobColumns,
		now, workerID, leaseUntil,
	)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return domain.Job{}, false, nil
	}
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("claim job: %w", err)
	}
	return job, true, nil
}

func (s *Store) StartJob(ctx context.Context, jobID, workerID string, now time.Time) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx, `
UPDATE jobs
SET status = 'running', attempt = attempt + 1, updated_at = $3
WHERE id = $1
  AND status = 'leased'
  AND lease_owner = $2
  AND lease_until > $3
  AND attempt < max_attempts
RETURNING `+jobColumns, jobID, workerID, now)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return domain.Job{}, ErrJobNotClaimable
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("start job: %w", err)
	}
	return job, nil
}

func (s *Store) RenewLease(ctx context.Context, jobID, workerID string, now time.Time, leaseDuration time.Duration) (domain.Job, error) {
	if leaseDuration <= 0 {
		return domain.Job{}, fmt.Errorf("positive lease duration is required")
	}
	leaseUntil := now.Add(leaseDuration)
	row := s.db.QueryRowContext(ctx, `
UPDATE jobs
SET lease_until = $4, updated_at = $3
WHERE id = $1
  AND status IN ('leased', 'running')
  AND lease_owner = $2
  AND lease_until > $3
RETURNING `+jobColumns, jobID, workerID, now, leaseUntil)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return domain.Job{}, ErrLeaseLost
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("renew lease: %w", err)
	}
	return job, nil
}

func (s *Store) SucceedJob(ctx context.Context, jobID, workerID string, now time.Time) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx, `
UPDATE jobs
SET status = 'succeeded', lease_owner = NULL, lease_until = NULL, last_error = NULL, updated_at = $3
WHERE id = $1
  AND status = 'running'
  AND lease_owner = $2
  AND lease_until > $3
RETURNING `+jobColumns, jobID, workerID, now)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return domain.Job{}, ErrLeaseLost
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("succeed job: %w", err)
	}
	return job, nil
}

func (s *Store) FailJob(ctx context.Context, jobID, workerID string, now, retryAt time.Time, message string) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx, `
UPDATE jobs
SET status = CASE WHEN attempt < max_attempts THEN 'retry_wait' ELSE 'failed' END,
    available_at = CASE WHEN attempt < max_attempts THEN $4 ELSE available_at END,
    lease_owner = NULL,
    lease_until = NULL,
    last_error = NULLIF($5, ''),
    updated_at = $3
WHERE id = $1
  AND status = 'running'
  AND lease_owner = $2
  AND lease_until > $3
RETURNING `+jobColumns, jobID, workerID, now, retryAt, message)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return domain.Job{}, ErrLeaseLost
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("fail job: %w", err)
	}
	return job, nil
}

func (s *Store) RecoverExpiredLeases(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin lease recovery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
UPDATE runs AS r
SET status = 'timed_out', finished_at = $1,
    summary = CASE WHEN COALESCE(summary, '') = '' THEN 'worker lease expired' ELSE summary END
FROM jobs AS j
WHERE r.job_id = j.id
  AND r.attempt = j.attempt
  AND r.status = 'running'
  AND j.status = 'running'
  AND j.lease_until IS NOT NULL
  AND j.lease_until <= $1`, now); err != nil {
		return 0, fmt.Errorf("expire abandoned runs: %w", err)
	}

	leasedResult, err := tx.ExecContext(ctx, `
UPDATE jobs
SET status = 'queued', lease_owner = NULL, lease_until = NULL,
    last_error = COALESCE(NULLIF(last_error, ''), 'lease expired before run started'),
    updated_at = $1
WHERE status = 'leased'
  AND lease_until IS NOT NULL
  AND lease_until <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("recover leased jobs: %w", err)
	}
	leasedCount, err := leasedResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count recovered leased jobs: %w", err)
	}

	runningResult, err := tx.ExecContext(ctx, `
UPDATE jobs
SET status = CASE WHEN attempt < max_attempts THEN 'retry_wait' ELSE 'failed' END,
    available_at = CASE WHEN attempt < max_attempts THEN $1 ELSE available_at END,
    lease_owner = NULL,
    lease_until = NULL,
    last_error = COALESCE(NULLIF(last_error, ''), 'worker lease expired'),
    updated_at = $1
WHERE status = 'running'
  AND lease_until IS NOT NULL
  AND lease_until <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("recover running jobs: %w", err)
	}
	runningCount, err := runningResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count recovered running jobs: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit lease recovery: %w", err)
	}
	return leasedCount + runningCount, nil
}

func scanJob(row rowScanner) (domain.Job, error) {
	var job domain.Job
	if err := row.Scan(
		&job.ID,
		&job.RepositoryID,
		&job.Kind,
		&job.Role,
		&job.Subject,
		&job.Revision,
		&job.Priority,
		&job.Status,
		&job.Attempt,
		&job.MaxAttempts,
		&job.AvailableAt,
		&job.LeaseOwner,
		&job.LeaseUntil,
		&job.LastError,
		&job.CreatedAt,
		&job.UpdatedAt,
	); err != nil {
		return domain.Job{}, err
	}
	return job, nil
}
