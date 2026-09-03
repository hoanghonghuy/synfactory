package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) PutEvent(ctx context.Context, event InboxEvent) (InboxEvent, bool, error) {
	if event.DedupeKey == "" || event.Provider == "" || event.Kind == "" || event.Subject == "" {
		return InboxEvent{}, false, fmt.Errorf("event dedupe key, provider, kind and subject are required")
	}

	row := s.db.QueryRowContext(ctx, `
INSERT INTO event_inbox (
    dedupe_key, provider, repository_id, kind, subject, revision, delivery_id, payload
) VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, NULLIF($7, ''), $8)
ON CONFLICT (dedupe_key) DO NOTHING
RETURNING id, dedupe_key, provider, COALESCE(repository_id, ''), kind, subject, revision,
          COALESCE(delivery_id, ''), payload, received_at, processed_at, COALESCE(process_error, '')`,
		event.DedupeKey,
		event.Provider,
		event.RepositoryID,
		event.Kind,
		event.Subject,
		event.Revision,
		event.DeliveryID,
		jsonOrEmpty(event.Payload),
	)
	inserted, err := scanEvent(row)
	if err == nil {
		return inserted, true, nil
	}
	if err != sql.ErrNoRows {
		return InboxEvent{}, false, fmt.Errorf("insert event: %w", err)
	}

	existingRow := s.db.QueryRowContext(ctx, `
SELECT id, dedupe_key, provider, COALESCE(repository_id, ''), kind, subject, revision,
       COALESCE(delivery_id, ''), payload, received_at, processed_at, COALESCE(process_error, '')
FROM event_inbox
WHERE dedupe_key = $1`, event.DedupeKey)
	existing, err := scanEvent(existingRow)
	if err != nil {
		return InboxEvent{}, false, fmt.Errorf("load existing event: %w", err)
	}
	return existing, false, nil
}

func (s *Store) MarkEventProcessed(ctx context.Context, id int64, processedAt time.Time, processErr string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE event_inbox
SET processed_at = $2, process_error = NULLIF($3, '')
WHERE id = $1`, id, processedAt, processErr)
	if err != nil {
		return fmt.Errorf("mark event processed: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("event rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func scanEvent(row rowScanner) (InboxEvent, error) {
	var event InboxEvent
	if err := row.Scan(
		&event.ID,
		&event.DedupeKey,
		&event.Provider,
		&event.RepositoryID,
		&event.Kind,
		&event.Subject,
		&event.Revision,
		&event.DeliveryID,
		&event.Payload,
		&event.ReceivedAt,
		&event.ProcessedAt,
		&event.ProcessError,
	); err != nil {
		return InboxEvent{}, err
	}
	return event, nil
}
