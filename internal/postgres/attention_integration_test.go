package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
)

func TestAttentionUpsertDeduplicatesAndActiveQueryHonorsSnoozeResolution(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	key, err := attention.DedupeKey("repo-a", "wf-a", attention.KindReleaseBlocker, "release blocked")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.UpsertAttention(ctx, attention.Item{
		ID: "attn-1", DedupeKey: key, RepositoryID: "repo-a", WorkflowID: "wf-a",
		Kind: attention.KindReleaseBlocker, Severity: attention.SeverityCritical,
		State: attention.StateOpen, Title: "Release blocked", Summary: "CI gate failed",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "attn-1" {
		t.Fatalf("unexpected id %q", first.ID)
	}

	snoozeUntil := now.Add(time.Hour)
	second, err := store.UpsertAttention(ctx, attention.Item{
		ID: "different-id", DedupeKey: key, RepositoryID: "repo-a", WorkflowID: "wf-a",
		Kind: attention.KindReleaseBlocker, Severity: attention.SeverityWarning,
		State: attention.StateSnoozed, Title: "Release blocked", Summary: "same logical blocker",
		SnoozedUntil: &snoozeUntil, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("dedupe must preserve logical identity: first=%+v second=%+v", first, second)
	}
	active, err := store.ActiveAttention(ctx, "repo-a", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("snoozed item should not be active: %+v", active)
	}
	active, err = store.ActiveAttention(ctx, "repo-a", snoozeUntil.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("expired snooze should reactivate item: %+v", active)
	}

	resolvedAt := now.Add(2 * time.Hour)
	_, err = store.UpsertAttention(ctx, attention.Item{
		ID: "ignored", DedupeKey: key, RepositoryID: "repo-a", WorkflowID: "wf-a",
		Kind: attention.KindReleaseBlocker, Severity: attention.SeverityWarning,
		State: attention.StateResolved, Title: "Release blocked", Summary: "resolved",
		ResolvedAt: &resolvedAt, UpdatedAt: resolvedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err = store.ActiveAttention(ctx, "repo-a", resolvedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("resolved item should not be active: %+v", active)
	}
}
