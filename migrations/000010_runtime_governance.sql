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
