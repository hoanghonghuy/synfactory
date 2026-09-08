package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

// AgentRun is the PostgreSQL-authoritative execution identity delivered to an
// authenticated outbound worker. The job lease and run row are committed
// together before this value may be returned to the transport.
type AgentRun struct {
	Job domain.Job
	Run Run
}

// HeartbeatAgentWorker refreshes agent-owned capability metadata without
// clearing an operator-controlled draining flag on reconnect.
func (s *Store) HeartbeatAgentWorker(ctx context.Context, worker Worker, at time.Time) (Worker, error) {
	if worker.ID == "" {
		return Worker{}, fmt.Errorf("worker id is required")
	}
	if worker.Capacity <= 0 {
		worker.Capacity = 1
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO workers (id, host, capacity, draining, last_heartbeat, metadata)
VALUES ($1, $2, $3, FALSE, $4, $5)
ON CONFLICT (id) DO UPDATE SET
    host = EXCLUDED.host,
    capacity = EXCLUDED.capacity,
    last_heartbeat = EXCLUDED.last_heartbeat,
    metadata = EXCLUDED.metadata
RETURNING id, host, capacity, draining, last_heartbeat, started_at, metadata`,
		worker.ID, worker.Host, worker.Capacity, at, jsonOrEmpty(worker.Metadata))
	var heartbeat Worker
	if err := row.Scan(
		&heartbeat.ID,
		&heartbeat.Host,
		&heartbeat.Capacity,
		&heartbeat.Draining,
		&heartbeat.LastHeartbeat,
		&heartbeat.StartedAt,
		&heartbeat.Metadata,
	); err != nil {
		return Worker{}, fmt.Errorf("heartbeat agent worker: %w", err)
	}
	return heartbeat, nil
}

// ActiveAgentRun returns the current unexpired running job/run owned by worker.
// It never repairs or advances workflow state.
func (s *Store) ActiveAgentRun(ctx context.Context, workerID string, now time.Time) (AgentRun, bool, error) {
	if workerID == "" {
		return AgentRun{}, false, fmt.Errorf("worker id is required")
	}
	row := s.db.QueryRowContext(ctx, activeAgentRunSQL, workerID, now)
	value, err := scanAgentRun(row)
	if err == sql.ErrNoRows {
		return AgentRun{}, false, nil
	}
	if err != nil {
		return AgentRun{}, false, fmt.Errorf("load active agent run: %w", err)
	}
	return value, true, nil
}

// AcquireAgentRun serializes acquisition for one worker, reuses an existing
// unexpired run on reconnect, otherwise claims a compatible job, starts it and
// creates the durable run in one transaction. No transport acknowledgement can
// enter this transaction or complete the job.
func (s *Store) AcquireAgentRun(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) (AgentRun, bool, error) {
	if workerID == "" || leaseDuration <= 0 {
		return AgentRun{}, false, fmt.Errorf("worker id and positive lease duration are required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentRun{}, false, fmt.Errorf("begin agent run acquisition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Session sequencing is in-memory and intentionally short lived. This
	// database lock closes the cross-request race where two accepted polls for
	// the same worker could otherwise claim two different jobs concurrently.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, workerID); err != nil {
		return AgentRun{}, false, fmt.Errorf("lock agent worker acquisition: %w", err)
	}

	active, err := scanAgentRun(tx.QueryRowContext(ctx, activeAgentRunSQL, workerID, now))
	if err == nil {
		if err := tx.Commit(); err != nil {
			return AgentRun{}, false, fmt.Errorf("commit active agent run lookup: %w", err)
		}
		return active, true, nil
	}
	if err != sql.ErrNoRows {
		return AgentRun{}, false, fmt.Errorf("load active agent run: %w", err)
	}

	leaseUntil := now.Add(leaseDuration)
	claimed, err := scanJob(tx.QueryRowContext(ctx, `
WITH candidate AS (
    SELECT j.id
    FROM jobs AS j
    JOIN workers AS w ON w.id = $2
    WHERE j.status IN ('queued', 'retry_wait')
      AND j.available_at <= $1
      AND j.attempt < j.max_attempts
      AND w.draining = FALSE
      AND btrim(j.revision) <> ''
      AND (
          COALESCE(j.metadata->'requirements', '{}'::jsonb) = '{}'::jsonb
          OR COALESCE(w.metadata->'capabilities', '{}'::jsonb) @> COALESCE(j.metadata->'requirements', '{}'::jsonb)
      )
    ORDER BY j.priority DESC, j.available_at ASC, j.created_at ASC
    FOR UPDATE OF j SKIP LOCKED
    LIMIT 1
)
UPDATE jobs AS j
SET status = 'leased', lease_owner = $2, lease_until = $3, updated_at = $1
FROM candidate AS c
WHERE j.id = c.id
RETURNING `+qualifiedJobColumns, now, workerID, leaseUntil))
	if err == sql.ErrNoRows {
		if err := tx.Commit(); err != nil {
			return AgentRun{}, false, fmt.Errorf("commit empty agent acquisition: %w", err)
		}
		return AgentRun{}, false, nil
	}
	if err != nil {
		return AgentRun{}, false, fmt.Errorf("claim agent job: %w", err)
	}

	running, err := scanJob(tx.QueryRowContext(ctx, `
UPDATE jobs
SET status = 'running', attempt = attempt + 1, updated_at = $3
WHERE id = $1
  AND status = 'leased'
  AND lease_owner = $2
  AND lease_until > $3
  AND attempt < max_attempts
RETURNING `+jobColumns, claimed.ID, workerID, now))
	if err != nil {
		return AgentRun{}, false, fmt.Errorf("start agent job: %w", err)
	}

	runID := deterministicAgentRunID(running.ID, running.Attempt)
	run, err := scanRun(tx.QueryRowContext(ctx, `
INSERT INTO runs (id, job_id, attempt, sequence, runtime, status, metadata)
VALUES ($1, $2, $3, 1, 'agent', 'running', '{}'::jsonb)
RETURNING id, job_id, attempt, sequence, runtime, COALESCE(model, ''), COALESCE(session_id, ''), status,
          started_at, finished_at, exit_code, COALESCE(summary, ''), metadata`,
		runID, running.ID, running.Attempt))
	if err != nil {
		return AgentRun{}, false, fmt.Errorf("create durable agent run: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return AgentRun{}, false, fmt.Errorf("commit agent run acquisition: %w", err)
	}
	return AgentRun{Job: running, Run: run}, true, nil
}

const activeAgentRunSQL = `
SELECT ` + qualifiedJobColumns + `,
       r.id, r.job_id, r.attempt, r.sequence, r.runtime, COALESCE(r.model, ''), COALESCE(r.session_id, ''), r.status,
       r.started_at, r.finished_at, r.exit_code, COALESCE(r.summary, ''), r.metadata
FROM jobs AS j
JOIN runs AS r ON r.job_id = j.id AND r.attempt = j.attempt
WHERE j.status = 'running'
  AND j.lease_owner = $1
  AND j.lease_until > $2
  AND r.status = 'running'
ORDER BY r.started_at ASC, r.id ASC
LIMIT 1`

func scanAgentRun(row rowScanner) (AgentRun, error) {
	job, run, err := scanJobAndRun(row)
	if err != nil {
		return AgentRun{}, err
	}
	return AgentRun{Job: job, Run: run}, nil
}

func scanJobAndRun(row rowScanner) (domain.Job, Run, error) {
	var job domain.Job
	var run Run
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
		&job.Metadata,
		&job.CreatedAt,
		&job.UpdatedAt,
		&run.ID,
		&run.JobID,
		&run.Attempt,
		&run.Sequence,
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
		return domain.Job{}, Run{}, err
	}
	return job, run, nil
}

func deterministicAgentRunID(jobID string, attempt int) string {
	sum := sha256.Sum256([]byte(jobID + "\x00" + strconv.Itoa(attempt)))
	return "agent-" + hex.EncodeToString(sum[:])
}
