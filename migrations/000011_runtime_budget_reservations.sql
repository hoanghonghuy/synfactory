CREATE TABLE runtime_budget_reservations (
    id TEXT PRIMARY KEY,
    repository TEXT NOT NULL,
    workflow_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL,
    role TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    reserved_cost_microusd BIGINT NOT NULL CHECK (reserved_cost_microusd > 0),
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'accounted', 'released')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    CHECK (expires_at > created_at),
    CHECK ((state = 'active' AND resolved_at IS NULL) OR (state <> 'active' AND resolved_at IS NOT NULL))
);

CREATE INDEX runtime_budget_reservation_repo_active_idx
    ON runtime_budget_reservations(repository, expires_at)
    WHERE state = 'active';

CREATE INDEX runtime_budget_reservation_identity_idx
    ON runtime_budget_reservations(repository, workflow_id, task_id, run_id);

CREATE UNIQUE INDEX runtime_budget_reservation_active_run_idx
    ON runtime_budget_reservations(repository, run_id, provider, model)
    WHERE state = 'active';
