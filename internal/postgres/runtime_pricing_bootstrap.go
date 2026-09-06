package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type runtimePricingBootstrap struct {
	Pricing []RuntimePricing `json:"pricing"`
}

// ApplyRuntimePricingBootstrap loads immutable pricing snapshots from an
// operator-managed JSON file. Reapplying the same file is idempotent because
// PutRuntimePricing rejects only version identities whose values changed.
func (s *Store) ApplyRuntimePricingBootstrap(ctx context.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("runtime pricing bootstrap path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open runtime pricing bootstrap: %w", err)
	}
	defer file.Close()

	pricing, err := decodeRuntimePricingBootstrap(file)
	if err != nil {
		return err
	}
	for _, snapshot := range pricing {
		if err := s.PutRuntimePricing(ctx, snapshot); err != nil {
			return fmt.Errorf("apply runtime pricing version %q: %w", snapshot.Version, err)
		}
	}
	return nil
}

func decodeRuntimePricingBootstrap(reader io.Reader) ([]RuntimePricing, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var document runtimePricingBootstrap
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode runtime pricing bootstrap: %w", err)
	}
	if len(document.Pricing) == 0 {
		return nil, errors.New("runtime pricing bootstrap must contain at least one pricing snapshot")
	}

	seen := make(map[string]struct{}, len(document.Pricing))
	for _, pricing := range document.Pricing {
		version := strings.TrimSpace(pricing.Version)
		if version == "" {
			return nil, errors.New("runtime pricing bootstrap version is required")
		}
		if _, exists := seen[version]; exists {
			return nil, fmt.Errorf("runtime pricing bootstrap contains duplicate version %q", version)
		}
		seen[version] = struct{}{}
		if strings.TrimSpace(pricing.Provider) == "" || strings.TrimSpace(pricing.Model) == "" {
			return nil, fmt.Errorf("runtime pricing bootstrap version %q requires provider and model", version)
		}
		if pricing.InputMicroUSDPerMillion < 0 || pricing.OutputMicroUSDPerMillion < 0 || pricing.RequestMicroUSD < 0 {
			return nil, fmt.Errorf("runtime pricing bootstrap version %q contains negative pricing", version)
		}
		if pricing.EffectiveAt.IsZero() {
			return nil, fmt.Errorf("runtime pricing bootstrap version %q requires effective_at", version)
		}
	}
	return document.Pricing, nil
}
