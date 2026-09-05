CREATE TABLE IF NOT EXISTS notification_deliveries (
    id TEXT PRIMARY KEY,
    attention_id TEXT NOT NULL REFERENCES operator_attention(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    state TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    delivered_at TIMESTAMPTZ,
    UNIQUE (attention_id, provider)
);

CREATE INDEX IF NOT EXISTS notification_deliveries_due_idx
    ON notification_deliveries (state, next_attempt_at)
    WHERE state IN ('pending', 'retrying');
