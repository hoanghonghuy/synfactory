CREATE TABLE IF NOT EXISTS security_audit_events (
    id TEXT PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS security_audit_events_occurred_at_idx
    ON security_audit_events (occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS security_audit_events_actor_idx
    ON security_audit_events (actor_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS security_audit_events_action_idx
    ON security_audit_events (action, occurred_at DESC);
CREATE INDEX IF NOT EXISTS security_audit_events_resource_idx
    ON security_audit_events (resource_type, resource_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS security_audit_events_outcome_idx
    ON security_audit_events (outcome, occurred_at DESC);
