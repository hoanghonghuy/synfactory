package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const eventColumns = `
id, dedupe_key, provider, COALESCE(repository_id, ''), kind, subject, revision,
COALESCE(delivery_id, ''), payload, received_at, processed_at, COALESCE(process_error, ''),
COALESCE(processing_owner, ''), processing_until, process_attempt, next_attempt_at`

const qualifiedEventColumns = `
e.id, e.dedupe_key, e.provider, COALESCE(e.repository_id, ''), e.kind, e.subject, e.revision,
COALESCE(e.delivery_id, ''), e.payload, e.received_at, e.processed_at, COALESCE(e.process_error, ''),
COALESCE(e.processing_owner, ''), e.processing_until, e.process_attempt, e.next_attempt_at`

func (s *Store) PutEvent(ctx context.Context, event InboxEvent) (InboxEvent, bool, error) {
	if event.DedupeKey == "" || event.Provider == "" || event.Kind == "" || event.Subject == "" {
		return InboxEvent{}, false, fmt.Errorf("event dedupe key, provider, kind and subject are required")
	}

	row := s.db.QueryRowContext(ctx, `
INSERT INTO event_inbox (
    dedupe_key, provider, repository_id, kind, subject, revision, delivery_id, payload
) VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, NULLIF($7, ''), $8)
ON CONFLICT (dedupe_key) DO NOTHING
RETURNING `+eventColumns,
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

	existingRow := s.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM event_inbox WHERE dedupe_key = $1`, event.DedupeKey)
	existing, err := scanEvent(existingRow)
	if err != nil {
		return InboxEvent{}, false, fmt.Errorf("load existing event: %w", err)
	}
	return existing, false, nil
}

func (s *Store) ClaimEvent(ctx context.Context, owner string, now time.Time, leaseDuration time.Duration) (InboxEvent, bool, error) {
	if owner == "" || leaseDuration <= 0 {
		return InboxEvent{}, false, fmt.Errorf("event processor owner and positive lease duration are required")
	}
	processingUntil := now.Add(leaseDuration)
	row := s.db.QueryRowContext(ctx, `
WITH candidate AS (
    SELECT id
    FROM event_inbox
    WHERE processed_at IS NULL
      AND next_attempt_at <= $1
      AND (processing_until IS NULL OR processing_until <= $1)
    ORDER BY received_at ASC, id ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE event_inbox AS e
SET processing_owner = $2,
    processing_until = $3,
    process_attempt = process_attempt + 1,
    process_error = NULL
FROM candidate AS c
WHERE e.id = c.id
RETURNING `+qualifiedEventColumns, now, owner, processingUntil)
	event, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return InboxEvent{}, false, nil
	}
	if err != nil {
		return InboxEvent{}, false, fmt.Errorf("claim event: %w", err)
	}
	return event, true, nil
}

func (s *Store) CompleteEvent(ctx context.Context, id int64, owner string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE event_inbox
SET processed_at = $3,
    process_error = NULL,
    processing_owner = NULL,
    processing_until = NULL
WHERE id = $1
  AND processed_at IS NULL
  AND processing_owner = $2
  AND processing_until > $3`, id, owner, now)
	if err != nil {
		return fmt.Errorf("complete event: %w", err)
	}
	return requireAffected(result, ErrLeaseLost)
}

func (s *Store) RetryEvent(ctx context.Context, id int64, owner string, now, nextAttempt time.Time, processErr string) error {
	if !nextAttempt.After(now) {
		return fmt.Errorf("event retry must be scheduled in the future")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE event_inbox
SET process_error = NULLIF($4, ''),
    processing_owner = NULL,
    processing_until = NULL,
    next_attempt_at = $5
WHERE id = $1
  AND processed_at IS NULL
  AND processing_owner = $2
  AND processing_until > $3`, id, owner, now, processErr, nextAttempt)
	if err != nil {
		return fmt.Errorf("retry event: %w", err)
	}
	return requireAffected(result, ErrLeaseLost)
}

func (s *Store) DeadLetterEvent(ctx context.Context, id int64, owner string, now time.Time, processErr string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE event_inbox
SET processed_at = $3,
    process_error = NULLIF($4, ''),
    processing_owner = NULL,
    processing_until = NULL
WHERE id = $1
  AND processed_at IS NULL
  AND processing_owner = $2
  AND processing_until > $3`, id, owner, now, processErr)
	if err != nil {
		return fmt.Errorf("dead-letter event: %w", err)
	}
	return requireAffected(result, ErrLeaseLost)
}

// MarkEventProcessed remains for compatibility with simple callers that do not
// use leased event processing. New processors should use CompleteEvent.
func (s *Store) MarkEventProcessed(ctx context.Context, id int64, processedAt time.Time, processErr string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE event_inbox
SET processed_at = $2, process_error = NULLIF($3, ''),
    processing_owner = NULL, processing_until = NULL
WHERE id = $1`, id, processedAt, processErr)
	if err != nil {
		return fmt.Errorf("mark event processed: %w", err)
	}
	return requireAffected(result, ErrNotFound)
}

func requireAffected(result sql.Result, zeroErr error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return zeroErr
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
		&event.ProcessingOwner,
		&event.ProcessingUntil,
		&event.ProcessAttempt,
		&event.NextAttemptAt,
	); err != nil {
		return InboxEvent{}, err
	}
	return event, nil
}
