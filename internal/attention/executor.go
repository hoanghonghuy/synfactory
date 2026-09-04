package attention

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type DeliveryStore interface {
	ListDueNotificationDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error)
	MarkNotificationDeliveryDelivered(ctx context.Context, attentionID, provider string, deliveredAt time.Time) error
	MarkNotificationDeliveryRetry(ctx context.Context, attentionID, provider string, attempts int, nextAttemptAt time.Time, lastError string) error
	MarkNotificationDeliveryFailed(ctx context.Context, attentionID, provider string, attempts int, failedAt time.Time, lastError string) error
}

type NotificationSource interface {
	NotificationForAttention(ctx context.Context, attentionID string) (Notification, error)
}

type Executor struct {
	Store     DeliveryStore
	Source    NotificationSource
	Providers map[string]Provider
	Policy    DeliveryPolicy
	Now       func() time.Time
}

func (e Executor) RunOnce(ctx context.Context, limit int) (int, error) {
	if e.Store == nil || e.Source == nil {
		return 0, errors.New("notification executor requires store and source")
	}
	if limit <= 0 {
		limit = 20
	}
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now().UTC()
	}
	records, err := e.Store.ListDueNotificationDeliveries(ctx, now, limit)
	if err != nil {
		return 0, fmt.Errorf("list due notification deliveries: %w", err)
	}
	processed := 0
	var errs []error
	for _, record := range records {
		provider := e.Providers[record.Provider]
		if provider == nil {
			errs = append(errs, e.fail(ctx, record, now, fmt.Errorf("notification provider %q unavailable", record.Provider)))
			processed++
			continue
		}
		notification, err := e.Source.NotificationForAttention(ctx, record.AttentionID)
		if err != nil {
			errs = append(errs, e.fail(ctx, record, now, fmt.Errorf("load attention notification: %w", err)))
			processed++
			continue
		}
		if err := ValidateNotification(notification); err != nil {
			errs = append(errs, e.fail(ctx, record, now, fmt.Errorf("invalid notification: %w", err)))
			processed++
			continue
		}
		if err := provider.Deliver(ctx, notification); err != nil {
			errs = append(errs, e.fail(ctx, record, now, err))
			processed++
			continue
		}
		if err := e.Store.MarkNotificationDeliveryDelivered(ctx, record.AttentionID, record.Provider, now); err != nil {
			errs = append(errs, fmt.Errorf("mark notification delivered: %w", err))
		}
		processed++
	}
	return processed, errors.Join(errs...)
}

func (e Executor) fail(ctx context.Context, record Delivery, now time.Time, cause error) error {
	attempts := record.Attempts + 1
	if delay, retry := e.Policy.Delay(attempts); retry {
		if err := e.Store.MarkNotificationDeliveryRetry(ctx, record.AttentionID, record.Provider, attempts, now.Add(delay), cause.Error()); err != nil {
			return fmt.Errorf("record notification retry after %v: %w", cause, err)
		}
		return nil
	}
	if err := e.Store.MarkNotificationDeliveryFailed(ctx, record.AttentionID, record.Provider, attempts, now, cause.Error()); err != nil {
		return fmt.Errorf("record terminal notification failure after %v: %w", cause, err)
	}
	return nil
}
