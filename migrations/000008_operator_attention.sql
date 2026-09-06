CREATE TABLE IF NOT EXISTS operator_attention (
    id TEXT PRIMARY KEY,
    dedupe_key TEXT NOT NULL UNIQUE,
    repository_id TEXT NOT NULL DEFAULT '',
    workflow_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    severity TEXT NOT NULL,
    state TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    assigned_to TEXT NOT NULL DEFAULT '',
    snoozed_until TIMESTAMPTZ,
    acknowledged_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS operator_attention_active_idx
    ON operator_attention (state, severity, updated_at DESC);

CREATE INDEX IF NOT EXISTS operator_attention_repository_idx
    ON operator_attention (repository_id, state, updated_at DESC);
