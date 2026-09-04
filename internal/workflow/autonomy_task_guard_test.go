package workflow

import (
	"context"
	"testing"
	"time"
)

type autonomyTaskRegistry struct {
	reserved map[string]bool
	binds    map[string]int64
}

func (r *autonomyTaskRegistry) ReserveTask(_ context.Context, repositoryID, fingerprint, _ string, _ time.Time, _ time.Duration) (bool, error) {
	if r.reserved == nil {
		r.reserved = map[string]bool{}
	}
	key := repositoryID + ":" + fingerprint
	if r.reserved[key] {
		return false, nil
	}
	r.reserved[key] = true
	return true, nil
}

func (r *autonomyTaskRegistry) BindTask(_ context.Context, repositoryID, fingerprint, _ string, issueNumber int64, _ string, _ time.Time) error {
	if r.binds == nil {
		r.binds = map[string]int64{}
	}
	r.binds[repositoryID+":"+fingerprint] = issueNumber
	return nil
}

func TestAutonomyTaskGuardSuppressesDuplicateUnchangedIntent(t *testing.T) {
	registry := &autonomyTaskRegistry{}
	guard := NewTaskGuard(registry, time.Minute)
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC)

	firstFingerprint, firstReserved, err := guard.Reserve(
		context.Background(), "repo-id", "owner/repo", "autonomy health", "track duplicate tasks", "pm-a", now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !firstReserved {
		t.Fatal("first unchanged product intent should reserve a task")
	}

	secondFingerprint, secondReserved, err := guard.Reserve(
		context.Background(), "repo-id", "OWNER/REPO", "  autonomy   health ", "track duplicate tasks", "pm-b", now.Add(10*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatalf("semantic normalization changed fingerprint: first=%q second=%q", firstFingerprint, secondFingerprint)
	}
	if secondReserved {
		t.Fatal("unchanged product intent must not create a duplicate task")
	}
}

func TestAutonomyTaskGuardAllowsMateriallyDifferentScope(t *testing.T) {
	registry := &autonomyTaskRegistry{}
	guard := NewTaskGuard(registry, time.Minute)
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC)

	_, firstReserved, err := guard.Reserve(context.Background(), "repo-id", "owner/repo", "autonomy health", "duplicate tasks", "pm", now)
	if err != nil || !firstReserved {
		t.Fatalf("first reserve = %v, err=%v", firstReserved, err)
	}
	_, secondReserved, err := guard.Reserve(context.Background(), "repo-id", "owner/repo", "autonomy health", "provider outage recovery", "pm", now)
	if err != nil {
		t.Fatal(err)
	}
	if !secondReserved {
		t.Fatal("materially different scope should remain eligible for a distinct feature-sized task")
	}
}
