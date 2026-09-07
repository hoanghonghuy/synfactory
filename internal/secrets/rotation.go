package secrets

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrNoStagedRotation = errors.New("no staged credential rotation")

// RotatingProvider keeps the active provider stable while a replacement value
// is staged and validated. Promotion affects only future Resolve calls; callers
// that already cloned an active value keep their existing credential until they
// rebuild naturally.
type RotatingProvider struct {
	active Provider

	mu       sync.RWMutex
	staged   map[string]Value
	promoted map[string]Value
}

func NewRotatingProvider(active Provider) *RotatingProvider {
	return &RotatingProvider{
		active:   active,
		staged:   make(map[string]Value),
		promoted: make(map[string]Value),
	}
}

func (p *RotatingProvider) Resolve(ctx context.Context, logicalName string) (Value, error) {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return Value{}, err
	}

	p.mu.RLock()
	value, ok := p.promoted[name]
	p.mu.RUnlock()
	if ok {
		return newValue(value.CloneBytes(), value.Provider), nil
	}
	if p.active == nil {
		return Value{}, fmt.Errorf("active secret provider is not configured")
	}
	return p.active.Resolve(ctx, name)
}

// Stage resolves the candidate immediately so an unavailable replacement
// cannot displace the active credential. The staged value remains private to
// this provider until Promote is called.
func (p *RotatingProvider) Stage(ctx context.Context, logicalName string, candidate Provider) error {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return err
	}
	if candidate == nil {
		return errors.New("candidate secret provider is required")
	}
	value, err := candidate.Resolve(ctx, name)
	if err != nil {
		return fmt.Errorf("resolve staged secret %q: %w", name, err)
	}

	p.mu.Lock()
	p.staged[name] = newValue(value.CloneBytes(), value.Provider)
	p.mu.Unlock()
	return nil
}

func (p *RotatingProvider) Promote(logicalName string) error {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	value, ok := p.staged[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoStagedRotation, name)
	}
	p.promoted[name] = newValue(value.CloneBytes(), value.Provider)
	delete(p.staged, name)
	return nil
}

func (p *RotatingProvider) DiscardStaged(logicalName string) error {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.staged[name]; !ok {
		return fmt.Errorf("%w: %s", ErrNoStagedRotation, name)
	}
	delete(p.staged, name)
	return nil
}

func (p *RotatingProvider) HasStaged(logicalName string) bool {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.staged[name]
	return ok
}
