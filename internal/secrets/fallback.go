package secrets

import (
	"context"
	"errors"
	"strings"
)

// FallbackProvider preserves explicit legacy compatibility inside the logical
// provider boundary. Wrapping it with RotatingProvider snapshots the fallback
// exactly like any other active credential, so a newly appearing primary value
// cannot silently bypass an explicit staged promotion.
type FallbackProvider struct {
	Primary Provider
	Values  map[string]string
}

func (p FallbackProvider) Resolve(ctx context.Context, logicalName string) (Value, error) {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return Value{}, err
	}
	if p.Primary != nil {
		value, resolveErr := p.Primary.Resolve(ctx, name)
		if resolveErr == nil {
			return value, nil
		}
		if !errors.Is(resolveErr, ErrNotFound) {
			return Value{}, resolveErr
		}
	}
	fallback := strings.TrimSpace(p.Values[name])
	if fallback == "" {
		return Value{}, errors.Join(ErrNotFound, errors.New("legacy fallback unavailable"))
	}
	return newValue([]byte(fallback), "legacy"), nil
}
