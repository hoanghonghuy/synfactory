package postgres

import (
	"context"
	"fmt"
	"time"
)

func (s *Store) HeartbeatWorker(ctx context.Context, worker Worker, at time.Time) (Worker, error) {
	if worker.ID == "" {
		return Worker{}, fmt.Errorf("worker id is required")
	}
	if worker.Capacity <= 0 {
		worker.Capacity = 1
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO workers (id, host, capacity, draining, last_heartbeat, metadata)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    host = EXCLUDED.host,
    capacity = EXCLUDED.capacity,
    draining = EXCLUDED.draining,
    last_heartbeat = EXCLUDED.last_heartbeat,
    metadata = EXCLUDED.metadata
RETURNING id, host, capacity, draining, last_heartbeat, started_at, metadata`,
		worker.ID,
		worker.Host,
		worker.Capacity,
		worker.Draining,
		at,
		jsonOrEmpty(worker.Metadata),
	)
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
		return Worker{}, fmt.Errorf("heartbeat worker: %w", err)
	}
	return heartbeat, nil
}

func (s *Store) SetWorkerDraining(ctx context.Context, workerID string, draining bool, at time.Time) error {
	if workerID == "" {
		return fmt.Errorf("worker id is required")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE workers
SET draining = $2, last_heartbeat = $3
WHERE id = $1`, workerID, draining, at)
	if err != nil {
		return fmt.Errorf("set worker draining: %w", err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("count worker draining update: %w", err)
	}
	return nil
}
