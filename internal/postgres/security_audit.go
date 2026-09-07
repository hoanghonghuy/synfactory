package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

func (s *Store) AppendSecurityAudit(ctx context.Context, event securityaudit.Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	metadata := event.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO security_audit_events (
    id, occurred_at, actor_type, actor_id, action,
    resource_type, resource_id, outcome, request_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (id) DO NOTHING`,
		event.ID,
		event.OccurredAt.UTC(),
		strings.TrimSpace(event.ActorType),
		strings.TrimSpace(event.ActorID),
		strings.TrimSpace(event.Action),
		strings.TrimSpace(event.ResourceType),
		strings.TrimSpace(event.ResourceID),
		strings.TrimSpace(event.Outcome),
		strings.TrimSpace(event.RequestID),
		metadata,
	)
	if err != nil {
		return fmt.Errorf("append security audit event: %w", err)
	}
	return nil
}

func (s *Store) ListSecurityAudit(ctx context.Context, filter securityaudit.Filter) ([]securityaudit.Event, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	query := `
SELECT id, occurred_at, actor_type, actor_id, action,
       resource_type, resource_id, outcome, request_id, metadata
FROM security_audit_events
WHERE ($1 = '' OR actor_id = $1)
  AND ($2 = '' OR action = $2)
  AND ($3 = '' OR resource_type = $3)
  AND ($4 = '' OR resource_id = $4)
  AND ($5 = '' OR outcome = $5)
  AND ($6::timestamptz IS NULL OR occurred_at >= $6)
  AND ($7::timestamptz IS NULL OR occurred_at <= $7)
ORDER BY occurred_at DESC, id DESC
LIMIT $8`

	var since any
	if !filter.Since.IsZero() {
		since = filter.Since.UTC()
	}
	var until any
	if !filter.Until.IsZero() {
		until = filter.Until.UTC()
	}

	rows, err := s.db.QueryContext(ctx, query,
		strings.TrimSpace(filter.ActorID),
		strings.TrimSpace(filter.Action),
		strings.TrimSpace(filter.ResourceType),
		strings.TrimSpace(filter.ResourceID),
		strings.TrimSpace(filter.Outcome),
		since,
		until,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list security audit events: %w", err)
	}
	defer rows.Close()

	events := make([]securityaudit.Event, 0)
	for rows.Next() {
		var event securityaudit.Event
		var occurredAt time.Time
		if err := rows.Scan(
			&event.ID,
			&occurredAt,
			&event.ActorType,
			&event.ActorID,
			&event.Action,
			&event.ResourceType,
			&event.ResourceID,
			&event.Outcome,
			&event.RequestID,
			&event.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan security audit event: %w", err)
		}
		event.OccurredAt = occurredAt.UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate security audit events: %w", err)
	}
	return events, nil
}
