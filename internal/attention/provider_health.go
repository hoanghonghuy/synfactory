package attention

import (
	"sort"
	"strings"
	"time"
)

// ProviderHealth summarizes notification transport state without changing
// attention or workflow truth. It is intended for operator observability and
// escalation decisions only.
type ProviderHealth struct {
	Provider       string    `json:"provider"`
	Pending        int       `json:"pending"`
	Retrying       int       `json:"retrying"`
	Failed         int       `json:"failed"`
	Delivered      int       `json:"delivered"`
	OldestUnhealthy time.Time `json:"oldest_unhealthy_at,omitempty"`
	Healthy        bool      `json:"healthy"`
}

// AggregateProviderHealth groups durable delivery state by provider. A provider
// is unhealthy when it has any terminal failure or any retry that is already
// due. Future scheduled retries remain observable but do not mark the provider
// unhealthy yet.
func AggregateProviderHealth(deliveries []Delivery, now time.Time) []ProviderHealth {
	now = now.UTC()
	byProvider := make(map[string]*ProviderHealth)
	for _, delivery := range deliveries {
		provider := strings.TrimSpace(delivery.Provider)
		if provider == "" {
			provider = "unknown"
		}
		health := byProvider[provider]
		if health == nil {
			health = &ProviderHealth{Provider: provider, Healthy: true}
			byProvider[provider] = health
		}

		switch delivery.State {
		case DeliveryPending:
			health.Pending++
		case DeliveryRetrying:
			health.Retrying++
			if !delivery.NextAttempt.After(now) {
				markProviderUnhealthy(health, delivery.UpdatedAt)
			}
		case DeliveryFailed:
			health.Failed++
			markProviderUnhealthy(health, delivery.UpdatedAt)
		case DeliveryDelivered:
			health.Delivered++
		}
	}

	result := make([]ProviderHealth, 0, len(byProvider))
	for _, health := range byProvider {
		result = append(result, *health)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Provider < result[j].Provider })
	return result
}

func markProviderUnhealthy(health *ProviderHealth, at time.Time) {
	health.Healthy = false
	at = at.UTC()
	if at.IsZero() {
		return
	}
	if health.OldestUnhealthy.IsZero() || at.Before(health.OldestUnhealthy) {
		health.OldestUnhealthy = at
	}
}
