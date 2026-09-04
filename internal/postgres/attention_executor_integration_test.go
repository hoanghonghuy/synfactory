package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
)

func TestNotificationExecutorStateTransitionsPersist(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	key, err := attention.DedupeKey("repo-executor", "wf-executor", attention.KindRepairExhausted, "repair budget exhausted")
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.UpsertAttention(ctx, attention.Item{
		ID: "attn-executor", DedupeKey: key, RepositoryID: "repo-executor", WorkflowID: "wf-executor",
		Kind: attention.KindRepairExhausted, Severity: attention.SeverityCritical, State: attention.StateOpen,
		Title: "Repair exhausted", Summary: "workflow needs operator attention", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := store.UpsertNotificationDelivery(ctx, attention.Delivery{
		ID: "delivery-executor", AttentionID: item.ID, Provider: "webhook", State: attention.DeliveryPending,
		NextAttempt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	retryAt := now.Add(time.Minute)
	if err := store.MarkNotificationDeliveryRetry(ctx, item.ID, "webhook", 1, retryAt, "timeout"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.NotificationDelivery(ctx, delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != attention.DeliveryRetrying || updated.Attempts != 1 || !updated.NextAttempt.Equal(retryAt) || updated.LastError != "timeout" {
		t.Fatalf("unexpected persisted retry state: %+v", updated)
	}

	if err := store.MarkNotificationDeliveryDelivered(ctx, item.ID, "webhook", retryAt); err != nil {
		t.Fatal(err)
	}
	updated, err = store.NotificationDelivery(ctx, delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != attention.DeliveryDelivered || updated.DeliveredAt == nil || !updated.DeliveredAt.Equal(retryAt) || updated.LastError != "" {
		t.Fatalf("unexpected persisted delivered state: %+v", updated)
	}

	notification, err := store.NotificationForAttention(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if notification.AttentionID != item.ID || notification.RepositoryID != item.RepositoryID || notification.Metadata["workflow_id"] != item.WorkflowID {
		t.Fatalf("unexpected notification projection: %+v", notification)
	}
}
