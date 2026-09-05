package attention

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type RoutingStore interface {
	ActiveAttention(context.Context, string, time.Time) ([]Item, error)
	EnsureNotificationDelivery(context.Context, Delivery) (bool, error)
}

// Dispatcher turns active human-attention facts into durable provider delivery
// records. It never mutates workflow truth and never resets an existing
// delivery's retry/delivered/failed state.
type Dispatcher struct {
	Store  RoutingStore
	Router EscalationRouter
	Now    func() time.Time
}

func (d Dispatcher) RunOnce(ctx context.Context) (int, error) {
	if d.Store == nil {
		return 0, errors.New("attention dispatcher requires store")
	}
	now := time.Now().UTC()
	if d.Now != nil {
		now = d.Now().UTC()
	}
	items, err := d.Store.ActiveAttention(ctx, "", now)
	if err != nil {
		return 0, fmt.Errorf("list active attention for dispatch: %w", err)
	}

	created := 0
	var errs []error
	for _, item := range items {
		providers, err := d.Router.Route(item, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("route attention %s: %w", item.ID, err))
			continue
		}
		for _, provider := range providers {
			delivery := Delivery{
				ID:          deliveryID(item.ID, provider),
				AttentionID: item.ID,
				Provider:    provider,
				State:       DeliveryPending,
				NextAttempt: now,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			inserted, err := d.Store.EnsureNotificationDelivery(ctx, delivery)
			if err != nil {
				errs = append(errs, fmt.Errorf("ensure %s delivery for attention %s: %w", provider, item.ID, err))
				continue
			}
			if inserted {
				created++
			}
		}
	}
	return created, errors.Join(errs...)
}

func deliveryID(attentionID, provider string) string {
	sum := sha256.Sum256([]byte(attentionID + "\x00" + provider))
	return "delivery-" + hex.EncodeToString(sum[:16])
}
