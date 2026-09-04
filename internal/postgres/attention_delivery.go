package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
)

func (s *Store) UpsertNotificationDelivery(ctx context.Context, delivery attention.Delivery) (attention.Delivery, error) {
	if delivery.ID == "" || delivery.AttentionID == "" || delivery.Provider == "" {
		return attention.Delivery{}, fmt.Errorf("delivery id, attention id and provider are required")
	}
	now := time.Now().UTC()
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = now
	}
	if delivery.NextAttempt.IsZero() {
		delivery.NextAttempt = now
	}
	if delivery.State == "" {
		delivery.State = attention.DeliveryPending
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO notification_deliveries (
    id, attention_id, provider, state, attempts, next_attempt_at, last_error,
    created_at, updated_at, delivered_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (attention_id, provider) DO UPDATE SET
    state = EXCLUDED.state,
    attempts = EXCLUDED.attempts,
    next_attempt_at = EXCLUDED.next_attempt_at,
    last_error = EXCLUDED.last_error,
    updated_at = EXCLUDED.updated_at,
    delivered_at = EXCLUDED.delivered_at
RETURNING id, attention_id, provider, state, attempts, next_attempt_at, last_error,
          created_at, updated_at, delivered_at`,
		delivery.ID, delivery.AttentionID, delivery.Provider, delivery.State, delivery.Attempts,
		delivery.NextAttempt.UTC(), delivery.LastError, delivery.CreatedAt.UTC(), delivery.UpdatedAt.UTC(), delivery.DeliveredAt,
	)
	return scanDelivery(row)
}

func (s *Store) DueNotificationDeliveries(ctx context.Context, now time.Time, limit int) ([]attention.Delivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, attention_id, provider, state, attempts, next_attempt_at, last_error,
       created_at, updated_at, delivered_at
FROM notification_deliveries
WHERE state IN ($1, $2) AND next_attempt_at <= $3
ORDER BY next_attempt_at, created_at
LIMIT $4`, attention.DeliveryPending, attention.DeliveryRetrying, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("query due notification deliveries: %w", err)
	}
	defer rows.Close()
	var deliveries []attention.Delivery
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (s *Store) NotificationDelivery(ctx context.Context, id string) (attention.Delivery, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, attention_id, provider, state, attempts, next_attempt_at, last_error,
       created_at, updated_at, delivered_at
FROM notification_deliveries WHERE id = $1`, id)
	delivery, err := scanDelivery(row)
	if err == sql.ErrNoRows {
		return attention.Delivery{}, ErrNotFound
	}
	return delivery, err
}

type deliveryScanner interface {
	Scan(dest ...any) error
}

func scanDelivery(scanner deliveryScanner) (attention.Delivery, error) {
	var delivery attention.Delivery
	if err := scanner.Scan(
		&delivery.ID, &delivery.AttentionID, &delivery.Provider, &delivery.State, &delivery.Attempts,
		&delivery.NextAttempt, &delivery.LastError, &delivery.CreatedAt, &delivery.UpdatedAt, &delivery.DeliveredAt,
	); err != nil {
		return attention.Delivery{}, err
	}
	return delivery, nil
}
