package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) CreateRun(ctx context.Context, run Run) (Run, error) {
	if run.ID == "" || run.JobID == "" || run.Attempt <= 0 || run.Runtime == "" {
		return Run{}, fmt.Errorf("run id, job id, positive attempt and runtime are required")
	}
	if run.Status == "" {
		run.Status = "running"
	}

	row := s.db.QueryRowContext(ctx, `
INSERT INTO runs (id, job_id, attempt, runtime, model, session_id, status, metadata)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)
RETURNING id, job_id, attempt, runtime, COALESCE(model, ''), COALESCE(session_id, ''), status,
          started_at, finished_at, exit_code, COALESCE(summary, ''), metadata`,
		run.ID,
		run.JobID,
		run.Attempt,
		run.Runtime,
		run.Model,
		run.SessionID,
		run.Status,
		jsonOrEmpty(run.Metadata),
	)
	created, err := scanRun(row)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}
	return created, nil
}

func (s *Store) FinishRun(ctx context.Context, runID, status string, finishedAt time.Time, exitCode *int, summary, sessionID string) (Run, error) {
	row := s.db.QueryRowContext(ctx, `
UPDATE runs
SET status = $2,
    finished_at = $3,
    exit_code = $4,
    summary = NULLIF($5, ''),
    session_id = COALESCE(NULLIF($6, ''), session_id)
WHERE id = $1
  AND status IN ('pending', 'running')
RETURNING id, job_id, attempt, runtime, COALESCE(model, ''), COALESCE(session_id, ''), status,
          started_at, finished_at, exit_code, COALESCE(summary, ''), metadata`,
		runID, status, finishedAt, exitCode, summary, sessionID,
	)
	run, err := scanRun(row)
	if err == sql.ErrNoRows {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("finish run: %w", err)
	}
	return run, nil
}

func (s *Store) AddEvidence(ctx context.Context, evidence Evidence) (Evidence, error) {
	if evidence.RunID == "" || evidence.Kind == "" || evidence.Name == "" {
		return Evidence{}, fmt.Errorf("evidence run id, kind and name are required")
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO evidence (run_id, kind, name, uri, sha256, metadata)
VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)
RETURNING id, run_id, kind, name, COALESCE(uri, ''), COALESCE(sha256, ''), metadata, created_at`,
		evidence.RunID,
		evidence.Kind,
		evidence.Name,
		evidence.URI,
		evidence.SHA256,
		jsonOrEmpty(evidence.Metadata),
	)
	var created Evidence
	if err := row.Scan(
		&created.ID,
		&created.RunID,
		&created.Kind,
		&created.Name,
		&created.URI,
		&created.SHA256,
		&created.Metadata,
		&created.CreatedAt,
	); err != nil {
		return Evidence{}, fmt.Errorf("add evidence: %w", err)
	}
	return created, nil
}

func scanRun(row rowScanner) (Run, error) {
	var run Run
	if err := row.Scan(
		&run.ID,
		&run.JobID,
		&run.Attempt,
		&run.Runtime,
		&run.Model,
		&run.SessionID,
		&run.Status,
		&run.StartedAt,
		&run.FinishedAt,
		&run.ExitCode,
		&run.Summary,
		&run.Metadata,
	); err != nil {
		return Run{}, err
	}
	return run, nil
}
