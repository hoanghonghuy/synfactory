package attention

import (
	"context"
	"testing"
	"time"
)

type dispatcherStoreStub struct {
	items      []Item
	inserted   map[string]bool
	deliveries []Delivery
}

func (s *dispatcherStoreStub) ActiveAttention(context.Context, string, time.Time) ([]Item, error) {
	return append([]Item(nil), s.items...), nil
}

func (s *dispatcherStoreStub) EnsureNotificationDelivery(_ context.Context, delivery Delivery) (bool, error) {
	if s.inserted == nil {
		s.inserted = make(map[string]bool)
	}
	key := delivery.AttentionID + "\x00" + delivery.Provider
	if s.inserted[key] {
		return false, nil
	}
	s.inserted[key] = true
	s.deliveries = append(s.deliveries, delivery)
	return true, nil
}

func TestDispatcherCreatesOneDurableDeliveryPerAttentionProvider(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := &dispatcherStoreStub{items: []Item{{
		ID: "attn-1", DedupeKey: "repo|repair", RepositoryID: "repo-1",
		Kind: KindRepairExhausted, Severity: SeverityCritical, State: StateOpen,
		Title: "Repair exhausted", Summary: "CI repair budget exhausted", CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}}}
	dispatcher := Dispatcher{
		Store:  store,
		Router: EscalationRouter{Rules: []EscalationRule{{MinSeverity: SeverityInfo, Providers: []string{"slack"}}}},
		Now:    func() time.Time { return now },
	}

	created, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || len(store.deliveries) != 1 {
		t.Fatalf("first dispatch created=%d deliveries=%d, want 1/1", created, len(store.deliveries))
	}
	firstID := store.deliveries[0].ID
	if firstID == "" || store.deliveries[0].Provider != "slack" || store.deliveries[0].State != DeliveryPending {
		t.Fatalf("unexpected delivery: %#v", store.deliveries[0])
	}

	created, err = dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || len(store.deliveries) != 1 {
		t.Fatalf("second dispatch created=%d deliveries=%d, want 0/1", created, len(store.deliveries))
	}
	if deliveryID("attn-1", "slack") != firstID {
		t.Fatal("delivery ID must remain deterministic")
	}
}

func TestDispatcherHonorsEscalationAgeBeforeCreatingDelivery(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := &dispatcherStoreStub{items: []Item{{
		ID: "attn-young", DedupeKey: "repo|young", Severity: SeverityWarning, State: StateOpen,
		Title: "Needs attention", Summary: "not old enough", CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	}}}
	dispatcher := Dispatcher{
		Store:  store,
		Router: EscalationRouter{Rules: []EscalationRule{{MinSeverity: SeverityInfo, After: 10 * time.Minute, Providers: []string{"slack"}}}},
		Now:    func() time.Time { return now },
	}
	created, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || len(store.deliveries) != 0 {
		t.Fatalf("young attention dispatched: created=%d deliveries=%d", created, len(store.deliveries))
	}
}
