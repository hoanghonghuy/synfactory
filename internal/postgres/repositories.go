package postgres

import (
	"context"
	"database/sql"
	"fmt"
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
RETURNING id, provider, full_name, default_branch, enabled, config, created_at, updated_at`,
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
SELECT id, provider, full_name, default_branch, enabled, config, created_at, updated_at
FROM repositories
WHERE id = $1`, id)
	repository, err := scanRepository(row)
	if err == sql.ErrNoRows {
		return Repository{}, ErrNotFound
	}
	return repository, err
}

func (s *Store) ListRepositories(ctx context.Context) ([]Repository, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, provider, full_name, default_branch, enabled, config, created_at, updated_at
FROM repositories
WHERE enabled = TRUE
ORDER BY provider, full_name`)
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
		&repository.CreatedAt,
		&repository.UpdatedAt,
	); err != nil {
		return Repository{}, err
	}
	return repository, nil
}
