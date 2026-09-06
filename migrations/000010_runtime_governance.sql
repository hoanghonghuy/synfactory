CREATE TABLE runtime_pricing_versions (
    version TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    input_microusd_per_million BIGINT NOT NULL CHECK (input_microusd_per_million >= 0),
    output_microusd_per_million BIGINT NOT NULL CHECK (output_microusd_per_million >= 0),
    request_microusd BIGINT NOT NULL DEFAULT 0 CHECK (request_microusd >= 0),
    effective_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, model, version)
);

CREATE TABLE runtime_usage_ledger (
    id TEXT PRIMARY KEY,
    repository TEXT NOT NULL,
    workflow_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL,
    role TEXT NOT NULL,
    runtime TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    pricing_version TEXT NOT NULL REFERENCES runtime_pricing_versions(version),
    request_count BIGINT NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    runtime_ms BIGINT NOT NULL DEFAULT 0 CHECK (runtime_ms >= 0),
    cost_microusd BIGINT NOT NULL DEFAULT 0 CHECK (cost_microusd >= 0),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX runtime_usage_repo_time_idx ON runtime_usage_ledger(repository, recorded_at DESC);
CREATE INDEX runtime_usage_run_idx ON runtime_usage_ledger(repository, workflow_id, task_id, run_id);
CREATE INDEX runtime_usage_provider_time_idx ON runtime_usage_ledger(provider, recorded_at DESC);

CREATE TABLE runtime_budget_policies (
    id TEXT PRIMARY KEY,
    repository TEXT NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('repository_day', 'role_day', 'provider_day', 'workflow_max')),
    scope_key TEXT NOT NULL DEFAULT '',
    soft_limit_microusd BIGINT NOT NULL DEFAULT 0 CHECK (soft_limit_microusd >= 0),
    hard_limit_microusd BIGINT NOT NULL DEFAULT 0 CHECK (hard_limit_microusd >= 0),
    soft_outcome TEXT NOT NULL DEFAULT 'fallback' CHECK (soft_outcome IN ('fallback', 'park', 'escalate')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (hard_limit_microusd = 0 OR soft_limit_microusd <= hard_limit_microusd),
    CHECK ((scope = 'repository_day' AND scope_key = '') OR (scope <> 'repository_day' AND scope_key <> '')),
    UNIQUE (repository, scope, scope_key)
);

CREATE INDEX runtime_budget_policy_repo_idx ON runtime_budget_policies(repository, enabled, scope, scope_key);

CREATE TABLE runtime_budget_override_audit (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES runtime_budget_policies(id),
    repository TEXT NOT NULL,
    workflow_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX runtime_budget_override_repo_time_idx ON runtime_budget_override_audit(repository, created_at DESC);
CREATE INDEX runtime_budget_override_run_idx ON runtime_budget_override_audit(repository, workflow_id, task_id, run_id);
