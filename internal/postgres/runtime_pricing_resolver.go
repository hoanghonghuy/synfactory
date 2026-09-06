package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ResolveRuntimePricing returns the immutable pricing snapshot that was effective
// for a provider/model at the supplied execution time. Historical usage keeps the
// selected version identity even after newer pricing snapshots are added.
func (s *Store) ResolveRuntimePricing(ctx context.Context, provider, model string, at time.Time) (RuntimePricing, error) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" {
		return RuntimePricing{}, fmt.Errorf("runtime pricing provider and model are required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	var pricing RuntimePricing
	if err := s.db.QueryRowContext(ctx, `
SELECT version, provider, model, input_microusd_per_million,
       output_microusd_per_million, request_microusd, effective_at
  FROM runtime_pricing_versions
 WHERE provider = $1
   AND model = $2
   AND effective_at <= $3
 ORDER BY effective_at DESC, version DESC
 LIMIT 1`, provider, model, at).Scan(
		&pricing.Version, &pricing.Provider, &pricing.Model,
		&pricing.InputMicroUSDPerMillion, &pricing.OutputMicroUSDPerMillion,
		&pricing.RequestMicroUSD, &pricing.EffectiveAt,
	); err != nil {
		return RuntimePricing{}, fmt.Errorf("resolve effective runtime pricing: %w", err)
	}
	return pricing, nil
}
