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
