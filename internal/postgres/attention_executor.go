package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
)

func (s *Store) ListDueNotificationDeliveries(ctx context.Context, now time.Time, limit int) ([]attention.Delivery, error) {
	return s.DueNotificationDeliveries(ctx, now, limit)
}

func (s *Store) MarkNotificationDeliveryDelivered(ctx context.Context, attentionID, provider string, deliveredAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE notification_deliveries
SET state = $1, delivered_at = $2, updated_at = $2, last_error = '', next_attempt_at = $2
WHERE attention_id = $3 AND provider = $4`,
		attention.DeliveryDelivered, deliveredAt.UTC(), attentionID, provider,
	)
	if err != nil {
		return fmt.Errorf("mark notification delivery delivered: %w", err)
	}
	return ensureDeliveryAffected(result)
}

func (s *Store) MarkNotificationDeliveryRetry(ctx context.Context, attentionID, provider string, attempts int, nextAttemptAt time.Time, lastError string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE notification_deliveries
SET state = $1, attempts = $2, next_attempt_at = $3, last_error = $4, updated_at = $5
WHERE attention_id = $6 AND provider = $7`,
		attention.DeliveryRetrying, attempts, nextAttemptAt.UTC(), lastError, time.Now().UTC(), attentionID, provider,
	)
	if err != nil {
		return fmt.Errorf("mark notification delivery retry: %w", err)
	}
	return ensureDeliveryAffected(result)
}

func (s *Store) MarkNotificationDeliveryFailed(ctx context.Context, attentionID, provider string, attempts int, failedAt time.Time, lastError string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE notification_deliveries
SET state = $1, attempts = $2, next_attempt_at = $3, last_error = $4, updated_at = $3
WHERE attention_id = $5 AND provider = $6`,
		attention.DeliveryFailed, attempts, failedAt.UTC(), lastError, attentionID, provider,
	)
	if err != nil {
		return fmt.Errorf("mark notification delivery failed: %w", err)
	}
	return ensureDeliveryAffected(result)
}

func (s *Store) NotificationForAttention(ctx context.Context, attentionID string) (attention.Notification, error) {
	item, err := s.AttentionByID(ctx, attentionID)
	if err != nil {
		return attention.Notification{}, err
	}
	return attention.Notification{
		AttentionID:  item.ID,
		Severity:     item.Severity,
		Title:        item.Title,
		Summary:      item.Summary,
		RepositoryID: item.RepositoryID,
		Metadata: map[string]string{
			"kind":        string(item.Kind),
			"workflow_id": item.WorkflowID,
		},
	}, nil
}

func ensureDeliveryAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read notification delivery update count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
