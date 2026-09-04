package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
)

func (s *Store) UpsertRepository(ctx context.Context, repository Repository) (Repository, error) {
	if repository.ID == "" || repository.Provider == "" || repository.FullName == "" {
		return Repository{}, fmt.Errorf("repository id, provider and full name are required")
	}
	if repository.DefaultBranch == "" {
		repository.DefaultBranch = "main"
	}

	row := s.db.QueryRowContext(ctx, `
INSERT INTO repositories (id, provider, full_name, default_branch, enabled, config)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (provider, full_name) DO UPDATE SET
    default_branch = EXCLUDED.default_branch,
    enabled = EXCLUDED.enabled,
    config = EXCLUDED.config,
    updated_at = NOW()
RETURNING id, provider, full_name, default_branch, enabled, config, config_version, created_at, updated_at`,
		repository.ID,
		repository.Provider,
		repository.FullName,
		repository.DefaultBranch,
		repository.Enabled,
		jsonOrEmpty(repository.Config),
	)
	return scanRepository(row)
}

func (s *Store) GetRepository(ctx context.Context, id string) (Repository, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, provider, full_name, default_branch, enabled, config, config_version, created_at, updated_at
FROM repositories
WHERE id = $1`, id)
	repository, err := scanRepository(row)
	if err == sql.ErrNoRows {
		return Repository{}, ErrNotFound
	}
	return repository, err
}

func (s *Store) ListRepositories(ctx context.Context) ([]Repository, error) {
	return s.listRepositories(ctx, true)
}

func (s *Store) ListAllRepositories(ctx context.Context) ([]Repository, error) {
	return s.listRepositories(ctx, false)
}

func (s *Store) listRepositories(ctx context.Context, enabledOnly bool) ([]Repository, error) {
	query := `
SELECT id, provider, full_name, default_branch, enabled, config, config_version, created_at, updated_at
FROM repositories`
	if enabledOnly {
		query += ` WHERE enabled = TRUE`
	}
	query += ` ORDER BY provider, full_name`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()

	var repositories []Repository
	for rows.Next() {
		repository, err := scanRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repository: %w", err)
		}
		repositories = append(repositories, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repositories: %w", err)
	}
	return repositories, nil
}

func (s *Store) MutateRepository(ctx context.Context, repository Repository, action, actor string) (Repository, error) {
	if repository.ID == "" || repository.Provider == "" || repository.FullName == "" {
		return Repository{}, fmt.Errorf("repository id, provider and full name are required")
	}
	if action != "register" && action != "update" && action != "enable" && action != "disable" {
		return Repository{}, fmt.Errorf("unsupported repository action %q", action)
	}
	if actor == "" {
		actor = "operator"
	}
	if repository.DefaultBranch == "" {
		repository.DefaultBranch = "main"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Repository{}, fmt.Errorf("begin repository mutation: %w", err)
	}
	defer tx.Rollback()

	lockKey := repository.Provider + ":" + repository.FullName
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return Repository{}, fmt.Errorf("lock repository identity: %w", err)
	}

	var existing Repository
	err = tx.QueryRowContext(ctx, `
SELECT id, provider, full_name, default_branch, enabled, config, config_version, created_at, updated_at
FROM repositories
WHERE provider = $1 AND full_name = $2
FOR UPDATE`, repository.Provider, repository.FullName).Scan(
		&existing.ID, &existing.Provider, &existing.FullName, &existing.DefaultBranch, &existing.Enabled,
		&existing.Config, &existing.ConfigVersion, &existing.CreatedAt, &existing.UpdatedAt,
	)
	if err != nil && err != sql.ErrNoRows {
		return Repository{}, fmt.Errorf("lock repository: %w", err)
	}

	if err == nil &&
		existing.DefaultBranch == repository.DefaultBranch &&
		existing.Enabled == repository.Enabled &&
		jsonEquivalent(existing.Config, repository.Config) {
		if err := tx.Commit(); err != nil {
			return Repository{}, fmt.Errorf("commit idempotent repository mutation: %w", err)
		}
		return existing, nil
	}

	previous := json.RawMessage(`{}`)
	if err == nil {
		previous = existing.Config
	}

	var saved Repository
	err = tx.QueryRowContext(ctx, `
INSERT INTO repositories (id, provider, full_name, default_branch, enabled, config, config_version)
VALUES ($1, $2, $3, $4, $5, $6, 1)
ON CONFLICT (provider, full_name) DO UPDATE SET
    default_branch = EXCLUDED.default_branch,
    enabled = EXCLUDED.enabled,
    config = EXCLUDED.config,
    config_version = repositories.config_version + 1,
    updated_at = NOW()
RETURNING id, provider, full_name, default_branch, enabled, config, config_version, created_at, updated_at`,
		repository.ID, repository.Provider, repository.FullName, repository.DefaultBranch,
		repository.Enabled, jsonOrEmpty(repository.Config),
	).Scan(&saved.ID, &saved.Provider, &saved.FullName, &saved.DefaultBranch, &saved.Enabled, &saved.Config, &saved.ConfigVersion, &saved.CreatedAt, &saved.UpdatedAt)
	if err != nil {
		return Repository{}, fmt.Errorf("save repository: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO repository_config_audit (repository_id, config_version, action, actor, previous_config, new_config)
VALUES ($1, $2, $3, $4, $5, $6)`, saved.ID, saved.ConfigVersion, action, actor, jsonOrEmpty(previous), jsonOrEmpty(saved.Config)); err != nil {
		return Repository{}, fmt.Errorf("audit repository mutation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Repository{}, fmt.Errorf("commit repository mutation: %w", err)
	}
	return saved, nil
}

func jsonEquivalent(left, right json.RawMessage) bool {
	var leftValue any
	if err := json.Unmarshal(jsonOrEmpty(left), &leftValue); err != nil {
		return false
	}
	var rightValue any
	if err := json.Unmarshal(jsonOrEmpty(right), &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func (s *Store) ListRepositoryConfigAudit(ctx context.Context, repositoryID string) ([]RepositoryConfigAudit, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, repository_id, config_version, action, actor, previous_config, new_config, created_at
FROM repository_config_audit
WHERE repository_id = $1
ORDER BY config_version DESC`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository audit: %w", err)
	}
	defer rows.Close()

	var result []RepositoryConfigAudit
	for rows.Next() {
		var item RepositoryConfigAudit
		if err := rows.Scan(&item.ID, &item.RepositoryID, &item.ConfigVersion, &item.Action, &item.Actor, &item.PreviousConfig, &item.NewConfig, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan repository audit: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRepository(row rowScanner) (Repository, error) {
	var repository Repository
	if err := row.Scan(
		&repository.ID,
		&repository.Provider,
		&repository.FullName,
		&repository.DefaultBranch,
		&repository.Enabled,
		&repository.Config,
		&repository.ConfigVersion,
		&repository.CreatedAt,
		&repository.UpdatedAt,
	); err != nil {
		return Repository{}, err
	}
	return repository, nil
}
