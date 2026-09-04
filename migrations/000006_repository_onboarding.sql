ALTER TABLE repositories
    ADD COLUMN config_version BIGINT NOT NULL DEFAULT 1;

CREATE TABLE repository_config_audit (
    id BIGSERIAL PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    config_version BIGINT NOT NULL,
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    previous_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    new_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (action IN ('register', 'update', 'enable', 'disable'))
);

CREATE UNIQUE INDEX repository_config_audit_version_idx
    ON repository_config_audit (repository_id, config_version);
