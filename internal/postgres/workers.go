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
