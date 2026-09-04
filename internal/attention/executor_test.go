package attention

import (
	"context"
	"errors"
	"testing"
	"time"
)

type executorStore struct {
	due       []Delivery
	delivered int
	retried   int
	failed    int
	attempts  int
	next      time.Time
}

func (s *executorStore) ListDueNotificationDeliveries(context.Context, time.Time, int) ([]Delivery, error) {
	return s.due, nil
}
func (s *executorStore) MarkNotificationDeliveryDelivered(context.Context, string, string, time.Time) error {
	s.delivered++
	return nil
}
func (s *executorStore) MarkNotificationDeliveryRetry(_ context.Context, _, _ string, attempts int, next time.Time, _ string) error {
	s.retried++
	s.attempts = attempts
	s.next = next
	return nil
}
func (s *executorStore) MarkNotificationDeliveryFailed(_ context.Context, _, _ string, attempts int, _ time.Time, _ string) error {
	s.failed++
	s.attempts = attempts
	return nil
}

type executorSource struct{ notification Notification }

func (s executorSource) NotificationForAttention(context.Context, string) (Notification, error) {
	return s.notification, nil
}

type executorProvider struct{ err error }

func (executorProvider) Name() string { return "webhook" }
func (p executorProvider) Deliver(context.Context, Notification) error {
	return p.err
}

func validExecutorNotification() Notification {
	return Notification{AttentionID: "att-1", Severity: SeverityCritical, Title: "Needs attention", Summary: "Repair budget exhausted"}
}

func TestExecutorMarksSuccessfulDelivery(t *testing.T) {
	store := &executorStore{due: []Delivery{{AttentionID: "att-1", Provider: "webhook"}}}
	executor := Executor{Store: store, Source: executorSource{validExecutorNotification()}, Providers: map[string]Provider{"webhook": executorProvider{}}}
	processed, err := executor.RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || store.delivered != 1 || store.retried != 0 || store.failed != 0 {
		t.Fatalf("unexpected execution state: processed=%d delivered=%d retried=%d failed=%d", processed, store.delivered, store.retried, store.failed)
	}
}

func TestExecutorSchedulesBoundedRetry(t *testing.T) {
	now := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)
	store := &executorStore{due: []Delivery{{AttentionID: "att-1", Provider: "webhook", Attempts: 0}}}
	executor := Executor{
		Store: store, Source: executorSource{validExecutorNotification()}, Providers: map[string]Provider{"webhook": executorProvider{err: errors.New("provider down")}},
		Policy: DeliveryPolicy{MaxAttempts: 3, BaseDelay: time.Minute, MaxDelay: 5 * time.Minute}, Now: func() time.Time { return now },
	}
	processed, err := executor.RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || store.retried != 1 || store.failed != 0 || store.attempts != 1 || !store.next.Equal(now.Add(time.Minute)) {
		t.Fatalf("unexpected retry state: %+v", store)
	}
}

func TestExecutorStopsAfterRepairBudget(t *testing.T) {
	store := &executorStore{due: []Delivery{{AttentionID: "att-1", Provider: "webhook", Attempts: 2}}}
	executor := Executor{
		Store: store, Source: executorSource{validExecutorNotification()}, Providers: map[string]Provider{"webhook": executorProvider{err: errors.New("provider down")}},
		Policy: DeliveryPolicy{MaxAttempts: 3},
	}
	processed, err := executor.RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || store.failed != 1 || store.retried != 0 || store.attempts != 3 {
		t.Fatalf("unexpected terminal failure state: %+v", store)
	}
}
