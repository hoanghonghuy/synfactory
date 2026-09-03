CREATE TABLE repositories (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    full_name TEXT NOT NULL,
    default_branch TEXT NOT NULL DEFAULT 'main',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, full_name)
);

CREATE TABLE event_inbox (
    id BIGSERIAL PRIMARY KEY,
    dedupe_key TEXT NOT NULL UNIQUE,
    provider TEXT NOT NULL,
    repository_id TEXT REFERENCES repositories(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    subject TEXT NOT NULL,
    revision TEXT NOT NULL DEFAULT '',
    delivery_id TEXT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    process_error TEXT
);

CREATE INDEX event_inbox_unprocessed_idx
    ON event_inbox (received_at, id)
    WHERE processed_at IS NULL;

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    dedupe_key TEXT UNIQUE,
    repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    source_event_id BIGINT REFERENCES event_inbox(id) ON DELETE SET NULL,
    kind TEXT NOT NULL,
    role TEXT NOT NULL,
    subject TEXT NOT NULL,
    revision TEXT NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 100,
    status TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner TEXT,
    lease_until TIMESTAMPTZ,
    last_error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (status IN ('queued', 'leased', 'running', 'retry_wait', 'succeeded', 'failed', 'cancelled'))
);

CREATE INDEX jobs_claim_idx
    ON jobs (priority DESC, available_at ASC, created_at ASC)
    WHERE status IN ('queued', 'retry_wait');

CREATE INDEX jobs_lease_expiry_idx
    ON jobs (lease_until)
    WHERE status IN ('leased', 'running') AND lease_until IS NOT NULL;

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    attempt INTEGER NOT NULL,
    runtime TEXT NOT NULL,
    model TEXT,
    session_id TEXT,
    status TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    exit_code INTEGER,
    summary TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE (job_id, attempt),
    CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out'))
);

CREATE TABLE evidence (
    id BIGSERIAL PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    uri TEXT,
    sha256 TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE reconcile_state (
    repository_id TEXT PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
    last_incremental_at TIMESTAMPTZ,
    last_full_reconcile_at TIMESTAMPTZ,
    watermark JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
