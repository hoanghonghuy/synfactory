package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

func TestPruneSecurityAuditBeforeIsStrictAndBounded(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	cutoff := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	actor := fmt.Sprintf("retention-test-%d", time.Now().UnixNano())
	for index, occurredAt := range []time.Time{
		cutoff.Add(-2 * time.Second),
		cutoff.Add(-time.Second),
		cutoff,
		cutoff.Add(time.Second),
	} {
		if err := store.AppendSecurityAudit(ctx, securityaudit.Event{
			ID:           fmt.Sprintf("%s-%d", actor, index),
			OccurredAt:   occurredAt,
			ActorType:    "operator",
			ActorID:      actor,
			Action:       "security.audit.retention.fixture",
			ResourceType: "security_audit",
			ResourceID:   "retention-test",
			Outcome:      "success",
			Metadata:     json.RawMessage(`{"fixture":true}`),
		}); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := store.PruneSecurityAuditBefore(ctx, cutoff, 1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("first prune deleted = %d, want 1", deleted)
	}
	deleted, err = store.PruneSecurityAuditBefore(ctx, cutoff, 10)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("second prune deleted = %d, want 1", deleted)
	}

	remaining, err := store.ListSecurityAudit(ctx, securityaudit.Filter{ActorID: actor, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining events = %d, want 2", len(remaining))
	}
	for _, event := range remaining {
		if event.OccurredAt.Before(cutoff) {
			t.Fatalf("prune left event older than cutoff: %s", event.OccurredAt)
		}
	}
}

func TestPruneSecurityAuditBeforeRejectsUnsafeBounds(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if _, err := store.PruneSecurityAuditBefore(ctx, time.Time{}, 100); err == nil {
		t.Fatal("zero cutoff succeeded")
	}
	if _, err := store.PruneSecurityAuditBefore(ctx, time.Now(), 0); err == nil {
		t.Fatal("zero limit succeeded")
	}
	if _, err := store.PruneSecurityAuditBefore(ctx, time.Now(), 5001); err == nil {
		t.Fatal("oversized limit succeeded")
	}
}
