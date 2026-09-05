package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
)

func TestNotificationDeliveryUpsertIsIdempotentAndDueQueryIsBounded(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	key, err := attention.DedupeKey("repo-delivery", "wf-delivery", attention.KindCredential, "credential expired")
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.UpsertAttention(ctx, attention.Item{
		ID: "attn-delivery", DedupeKey: key, RepositoryID: "repo-delivery", WorkflowID: "wf-delivery",
		Kind: attention.KindCredential, Severity: attention.SeverityCritical, State: attention.StateOpen,
		Title: "Credential expired", Summary: "provider credential requires rotation", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.UpsertNotificationDelivery(ctx, attention.Delivery{
		ID: "delivery-1", AttentionID: item.ID, Provider: "webhook", State: attention.DeliveryPending,
		NextAttempt: now.Add(-time.Second), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "delivery-1" {
		t.Fatalf("unexpected delivery id %q", first.ID)
	}

	retryAt := now.Add(time.Minute)
	second, err := store.UpsertNotificationDelivery(ctx, attention.Delivery{
		ID: "different-id", AttentionID: item.ID, Provider: "webhook", State: attention.DeliveryRetrying,
		Attempts: 1, NextAttempt: retryAt, LastError: "timeout", CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.CreatedAt.Equal(first.CreatedAt) || second.Attempts != 1 {
		t.Fatalf("delivery dedupe must preserve identity while updating retry state: first=%+v second=%+v", first, second)
	}

	due, err := store.DueNotificationDeliveries(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("future retry must not be due: %+v", due)
	}
	due, err = store.DueNotificationDeliveries(ctx, retryAt.Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != first.ID {
		t.Fatalf("retry should become due once next_attempt_at passes: %+v", due)
	}
}
