package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) PutReconcileState(ctx context.Context, state ReconcileState) (ReconcileState, error) {
	if state.RepositoryID == "" {
		return ReconcileState{}, fmt.Errorf("repository id is required")
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO reconcile_state (
    repository_id, last_incremental_at, last_full_reconcile_at, watermark
) VALUES ($1, $2, $3, $4)
ON CONFLICT (repository_id) DO UPDATE SET
    last_incremental_at = EXCLUDED.last_incremental_at,
    last_full_reconcile_at = EXCLUDED.last_full_reconcile_at,
    watermark = EXCLUDED.watermark,
    updated_at = NOW()
RETURNING repository_id, last_incremental_at, last_full_reconcile_at, watermark, updated_at`,
		state.RepositoryID,
		state.LastIncrementalAt,
		state.LastFullReconcileAt,
		jsonOrEmpty(state.Watermark),
	)
	return scanReconcileState(row)
}

func (s *Store) GetReconcileState(ctx context.Context, repositoryID string) (ReconcileState, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT repository_id, last_incremental_at, last_full_reconcile_at, watermark, updated_at
FROM reconcile_state
WHERE repository_id = $1`, repositoryID)
	state, err := scanReconcileState(row)
	if err == sql.ErrNoRows {
		return ReconcileState{}, ErrNotFound
	}
	return state, err
}

func scanReconcileState(row rowScanner) (ReconcileState, error) {
	var state ReconcileState
	if err := row.Scan(
		&state.RepositoryID,
		&state.LastIncrementalAt,
		&state.LastFullReconcileAt,
		&state.Watermark,
		&state.UpdatedAt,
	); err != nil {
		return ReconcileState{}, err
	}
	return state, nil
}
