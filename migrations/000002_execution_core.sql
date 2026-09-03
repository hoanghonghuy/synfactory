CREATE TABLE workers (
    id TEXT PRIMARY KEY,
    host TEXT NOT NULL DEFAULT '',
    capacity INTEGER NOT NULL DEFAULT 1 CHECK (capacity > 0),
    draining BOOLEAN NOT NULL DEFAULT FALSE,
    last_heartbeat TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX workers_heartbeat_idx ON workers (last_heartbeat);

CREATE INDEX jobs_repository_active_idx
    ON jobs (repository_id, status, priority DESC, available_at ASC)
    WHERE status IN ('queued', 'retry_wait', 'leased', 'running');

CREATE INDEX runs_active_idx
    ON runs (job_id, attempt)
    WHERE status IN ('pending', 'running');
