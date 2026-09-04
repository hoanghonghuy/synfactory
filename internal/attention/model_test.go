package attention

import (
	"testing"
	"time"
)

func TestDedupeKeyNormalizesUnchangedBlockerIntent(t *testing.T) {
	first, err := DedupeKey("Repo-A", " Workflow-42 ", KindRepairExhausted, " CI   repair budget exhausted ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := DedupeKey("repo-a", "workflow-42", KindRepairExhausted, "ci repair budget exhausted")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same blocker produced different dedupe keys: %q != %q", first, second)
	}
}

func TestDedupeKeySeparatesMateriallyDifferentAttention(t *testing.T) {
	first, _ := DedupeKey("repo-a", "workflow-42", KindRepairExhausted, "ci repair budget exhausted")
	second, _ := DedupeKey("repo-a", "workflow-42", KindSecurityBlocker, "credential rotation required")
	if first == second {
		t.Fatal("different human-attention facts must not collapse into one item")
	}
}

func TestItemActiveDoesNotTreatSnoozeAsWorkflowResolution(t *testing.T) {
	now := time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	if (Item{State: StateResolved}).Active(now) {
		t.Fatal("resolved item should not be active")
	}
	if (Item{State: StateSnoozed, SnoozedUntil: &future}).Active(now) {
		t.Fatal("future snooze should hide the attention item temporarily")
	}
	if !(Item{State: StateSnoozed, SnoozedUntil: &past}).Active(now) {
		t.Fatal("expired snooze should return the item to active attention")
	}
	if !(Item{State: StateAcknowledged}).Active(now) {
		t.Fatal("acknowledgement must not resolve the underlying attention need")
	}
}

func TestAttentionTransitionsRequireGovernedActorAndFactRevalidation(t *testing.T) {
	now := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)
	item := Item{ID: "attn-1", State: StateOpen}

	if _, err := item.Acknowledge("", now); err == nil {
		t.Fatal("acknowledgement without an actor must fail")
	}
	acknowledged, err := item.Acknowledge("operator@example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if acknowledged.State != StateAcknowledged || acknowledged.AssignedTo != "operator@example.com" || acknowledged.AcknowledgedAt == nil {
		t.Fatalf("unexpected acknowledged item: %+v", acknowledged)
	}

	if _, err := acknowledged.Snooze("operator@example.com", now, now); err == nil {
		t.Fatal("non-future snooze must fail")
	}
	snoozed, err := acknowledged.Snooze("operator@example.com", now.Add(2*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if snoozed.State != StateSnoozed || snoozed.SnoozedUntil == nil || !snoozed.SnoozedUntil.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("unexpected snoozed item: %+v", snoozed)
	}

	if _, err := snoozed.Resolve("operator@example.com", now, false); err == nil {
		t.Fatal("resolution must fail until the underlying blocker is revalidated")
	}
	resolved, err := snoozed.Resolve("operator@example.com", now, true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != StateResolved || resolved.ResolvedAt == nil || resolved.SnoozedUntil != nil {
		t.Fatalf("unexpected resolved item: %+v", resolved)
	}
	if _, err := resolved.Acknowledge("operator@example.com", now.Add(time.Minute)); err == nil {
		t.Fatal("resolved item must not be reopened by acknowledgement")
	}
}
