package attention

import (
	"testing"
	"time"
)

func TestAggregateProviderHealth(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	deliveries := []Delivery{
		{Provider: "webhook", State: DeliveryDelivered, UpdatedAt: now.Add(-10 * time.Minute)},
		{Provider: "webhook", State: DeliveryRetrying, NextAttempt: now.Add(-time.Minute), UpdatedAt: now.Add(-5 * time.Minute)},
		{Provider: "email", State: DeliveryRetrying, NextAttempt: now.Add(5 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
		{Provider: "slack", State: DeliveryFailed, UpdatedAt: now.Add(-20 * time.Minute)},
		{Provider: "slack", State: DeliveryFailed, UpdatedAt: now.Add(-30 * time.Minute)},
	}

	health := AggregateProviderHealth(deliveries, now)
	if len(health) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(health))
	}

	if health[0].Provider != "email" || !health[0].Healthy || health[0].Retrying != 1 {
		t.Fatalf("unexpected email health: %+v", health[0])
	}
	if health[1].Provider != "slack" || health[1].Healthy || health[1].Failed != 2 {
		t.Fatalf("unexpected slack health: %+v", health[1])
	}
	if want := now.Add(-30 * time.Minute); !health[1].OldestUnhealthy.Equal(want) {
		t.Fatalf("expected oldest slack failure %s, got %s", want, health[1].OldestUnhealthy)
	}
	if health[2].Provider != "webhook" || health[2].Healthy || health[2].Retrying != 1 || health[2].Delivered != 1 {
		t.Fatalf("unexpected webhook health: %+v", health[2])
	}
}

func TestAggregateProviderHealthNormalizesEmptyProvider(t *testing.T) {
	now := time.Now().UTC()
	health := AggregateProviderHealth([]Delivery{{State: DeliveryPending}}, now)
	if len(health) != 1 || health[0].Provider != "unknown" || !health[0].Healthy || health[0].Pending != 1 {
		t.Fatalf("unexpected health: %+v", health)
	}
}
