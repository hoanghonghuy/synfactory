CREATE TABLE IF NOT EXISTS workflow_instances (
    id TEXT PRIMARY KEY,
    dedupe_key TEXT NOT NULL UNIQUE,
    repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    subject TEXT NOT NULL,
    revision TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 100,
    blocked_reason TEXT,
    ci_repair_attempts INTEGER NOT NULL DEFAULT 0 CHECK (ci_repair_attempts >= 0),
    ci_repair_limit INTEGER NOT NULL DEFAULT 2 CHECK (ci_repair_limit >= 0),
    review_repair_attempts INTEGER NOT NULL DEFAULT 0 CHECK (review_repair_attempts >= 0),
    review_repair_limit INTEGER NOT NULL DEFAULT 2 CHECK (review_repair_limit >= 0),
    last_dispatched_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repository_id, kind, subject)
);

CREATE INDEX IF NOT EXISTS workflow_runnable_idx
    ON workflow_instances (state, priority DESC, last_dispatched_at NULLS FIRST, created_at)
    WHERE state NOT IN ('completed', 'parked');

CREATE TABLE IF NOT EXISTS workflow_dependencies (
    workflow_id TEXT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    depends_on_id TEXT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    required_state TEXT NOT NULL DEFAULT 'completed',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workflow_id, depends_on_id),
    CHECK (workflow_id <> depends_on_id)
);

CREATE TABLE IF NOT EXISTS workflow_actions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    action_key TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    role TEXT NOT NULL,
    mode TEXT NOT NULL,
    target_state TEXT NOT NULL,
    revision TEXT NOT NULL DEFAULT '',
    budget_kind TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    job_id TEXT REFERENCES jobs(id) ON DELETE SET NULL,
    decision TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    UNIQUE (workflow_id, action_key)
);

CREATE INDEX IF NOT EXISTS workflow_actions_workflow_idx
    ON workflow_actions (workflow_id, created_at DESC);

CREATE TABLE IF NOT EXISTS workflow_history (
    id BIGSERIAL PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL,
    actor_role TEXT NOT NULL,
    reason TEXT,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS workflow_history_workflow_idx
    ON workflow_history (workflow_id, created_at DESC);

CREATE TABLE IF NOT EXISTS task_registry (
    repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    issue_number BIGINT,
    state TEXT NOT NULL,
    reservation_owner TEXT,
    reservation_until TIMESTAMPTZ,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repository_id, fingerprint)
);
