package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
)

func (s *Store) UpsertAttention(ctx context.Context, item attention.Item) (attention.Item, error) {
	if item.ID == "" || item.DedupeKey == "" {
		return attention.Item{}, fmt.Errorf("attention id and dedupe key are required")
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO operator_attention (
    id, dedupe_key, repository_id, workflow_id, kind, severity, state,
    title, summary, assigned_to, snoozed_until, acknowledged_at, resolved_at,
    created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (dedupe_key) DO UPDATE SET
    repository_id = EXCLUDED.repository_id,
    workflow_id = EXCLUDED.workflow_id,
    kind = EXCLUDED.kind,
    severity = EXCLUDED.severity,
    state = EXCLUDED.state,
    title = EXCLUDED.title,
    summary = EXCLUDED.summary,
    assigned_to = EXCLUDED.assigned_to,
    snoozed_until = EXCLUDED.snoozed_until,
    acknowledged_at = EXCLUDED.acknowledged_at,
    resolved_at = EXCLUDED.resolved_at,
    updated_at = EXCLUDED.updated_at
RETURNING id, dedupe_key, repository_id, workflow_id, kind, severity, state,
          title, summary, assigned_to, snoozed_until, created_at, updated_at,
          acknowledged_at, resolved_at`,
		item.ID, item.DedupeKey, item.RepositoryID, item.WorkflowID, item.Kind, item.Severity, item.State,
		item.Title, item.Summary, item.AssignedTo, item.SnoozedUntil, item.AcknowledgedAt, item.ResolvedAt,
		item.CreatedAt, item.UpdatedAt,
	)
	return scanAttention(row)
}

func (s *Store) AttentionByID(ctx context.Context, id string) (attention.Item, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, dedupe_key, repository_id, workflow_id, kind, severity, state,
       title, summary, assigned_to, snoozed_until, created_at, updated_at,
       acknowledged_at, resolved_at
FROM operator_attention WHERE id = $1`, id)
	item, err := scanAttention(row)
	if err == sql.ErrNoRows {
		return attention.Item{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ActiveAttention(ctx context.Context, repositoryID string, now time.Time) ([]attention.Item, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, dedupe_key, repository_id, workflow_id, kind, severity, state,
       title, summary, assigned_to, snoozed_until, created_at, updated_at,
       acknowledged_at, resolved_at
FROM operator_attention
WHERE state <> $1
  AND (state <> $2 OR snoozed_until IS NULL OR snoozed_until <= $3)
  AND ($4 = '' OR repository_id = $4)
ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,
         updated_at DESC`, attention.StateResolved, attention.StateSnoozed, now.UTC(), repositoryID)
	if err != nil {
		return nil, fmt.Errorf("query active attention: %w", err)
	}
	defer rows.Close()
	var items []attention.Item
	for rows.Next() {
		item, err := scanAttention(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type attentionScanner interface {
	Scan(dest ...any) error
}

func scanAttention(scanner attentionScanner) (attention.Item, error) {
	var item attention.Item
	err := scanner.Scan(
		&item.ID, &item.DedupeKey, &item.RepositoryID, &item.WorkflowID, &item.Kind,
		&item.Severity, &item.State, &item.Title, &item.Summary, &item.AssignedTo,
		&item.SnoozedUntil, &item.CreatedAt, &item.UpdatedAt, &item.AcknowledgedAt, &item.ResolvedAt,
	)
	if err != nil {
		return attention.Item{}, err
	}
	return item, nil
}
