package attention

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeAttentionStore struct {
	item  Item
	saves int
}

func (s *fakeAttentionStore) AttentionByID(context.Context, string) (Item, error) {
	return s.item, nil
}

func (s *fakeAttentionStore) UpsertAttention(_ context.Context, item Item) (Item, error) {
	s.item = item
	s.saves++
	return item, nil
}

type fakeRevalidator struct {
	resolved bool
	err      error
	calls    int
}

func (r *fakeRevalidator) UnderlyingResolved(context.Context, Item) (bool, error) {
	r.calls++
	return r.resolved, r.err
}

func TestServiceResolveRevalidatesUnderlyingFactBeforePersisting(t *testing.T) {
	now := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	store := &fakeAttentionStore{item: Item{ID: "attn-1", State: StateAcknowledged}}
	revalidator := &fakeRevalidator{resolved: false}
	service := Service{Store: store, Revalidator: revalidator, Now: func() time.Time { return now }}

	if _, err := service.Resolve(context.Background(), "attn-1", "operator@example.com"); err == nil {
		t.Fatal("resolve must fail while underlying blocker remains active")
	}
	if revalidator.calls != 1 {
		t.Fatalf("expected one revalidation call, got %d", revalidator.calls)
	}
	if store.saves != 0 {
		t.Fatalf("unresolved fact must not be persisted as resolved, saves=%d", store.saves)
	}

	revalidator.resolved = true
	resolved, err := service.Resolve(context.Background(), "attn-1", "operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != StateResolved || store.saves != 1 {
		t.Fatalf("expected one persisted resolution, item=%+v saves=%d", resolved, store.saves)
	}
}

func TestServiceResolveDoesNotPersistOnRevalidationFailure(t *testing.T) {
	store := &fakeAttentionStore{item: Item{ID: "attn-1", State: StateOpen}}
	revalidator := &fakeRevalidator{err: errors.New("workflow store unavailable")}
	service := Service{Store: store, Revalidator: revalidator}

	if _, err := service.Resolve(context.Background(), "attn-1", "operator@example.com"); err == nil {
		t.Fatal("revalidation failure must fail closed")
	}
	if store.saves != 0 {
		t.Fatalf("failed revalidation must not mutate attention state, saves=%d", store.saves)
	}
}

func TestServiceAcknowledgeAndSnoozeRemainAttentionOnlyActions(t *testing.T) {
	now := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	store := &fakeAttentionStore{item: Item{ID: "attn-1", State: StateOpen}}
	service := Service{Store: store, Now: func() time.Time { return now }}

	acknowledged, err := service.Acknowledge(context.Background(), "attn-1", "operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if acknowledged.State != StateAcknowledged {
		t.Fatalf("unexpected acknowledged state: %s", acknowledged.State)
	}

	snoozed, err := service.Snooze(context.Background(), "attn-1", "operator@example.com", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if snoozed.State != StateSnoozed || snoozed.ResolvedAt != nil {
		t.Fatalf("snooze must not imply workflow resolution: %+v", snoozed)
	}
	if store.saves != 2 {
		t.Fatalf("expected two attention-state saves, got %d", store.saves)
	}
}
