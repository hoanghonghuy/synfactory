package secrets

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrNoStagedRotation = errors.New("no staged credential rotation")

// RotatingProvider keeps a per-logical-name snapshot of the last known-good
// active credential while a replacement is staged. Promotion affects only
// future Resolve calls; values already handed to callers remain independent
// clones and are never mutated in place.
type RotatingProvider struct {
	active Provider

	mu       sync.RWMutex
	baseline map[string]Value
	staged   map[string]Value
	promoted map[string]Value
}

func NewRotatingProvider(active Provider) *RotatingProvider {
	return &RotatingProvider{
		active:   active,
		baseline: make(map[string]Value),
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
	if value, ok := p.promoted[name]; ok {
		p.mu.RUnlock()
		return newValue(value.CloneBytes(), value.Provider), nil
	}
	if value, ok := p.baseline[name]; ok {
		p.mu.RUnlock()
		return newValue(value.CloneBytes(), value.Provider), nil
	}
	p.mu.RUnlock()

	// Serialize the first resolution so concurrent callers cannot establish
	// different baselines if the backing source changes during rotation.
	p.mu.Lock()
	defer p.mu.Unlock()
	if value, ok := p.promoted[name]; ok {
		return newValue(value.CloneBytes(), value.Provider), nil
	}
	if value, ok := p.baseline[name]; ok {
		return newValue(value.CloneBytes(), value.Provider), nil
	}
	if p.active == nil {
		return Value{}, errors.New("active secret provider is not configured")
	}
	value, err := p.active.Resolve(ctx, name)
	if err != nil {
		return Value{}, err
	}
	cached := newValue(value.CloneBytes(), value.Provider)
	p.baseline[name] = cached
	return newValue(cached.CloneBytes(), cached.Provider), nil
}

// Stage first establishes the current effective credential as a stable
// baseline, then resolves and validates the candidate. A mutable backing source
// therefore cannot replace the active credential merely because its contents
// changed while a rotation is pending.
func (p *RotatingProvider) Stage(ctx context.Context, logicalName string, candidate Provider) error {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return err
	}
	if candidate == nil {
		return errors.New("candidate secret provider is required")
	}
	if _, err := p.Resolve(ctx, name); err != nil {
		return fmt.Errorf("resolve active secret %q before stage: %w", name, err)
	}
	value, err := candidate.Resolve(ctx, name)
	if err != nil {
		return fmt.Errorf("resolve staged secret %q: %w", name, err)
	}
	if len(value.CloneBytes()) == 0 {
		return fmt.Errorf("staged secret %q is empty", name)
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
