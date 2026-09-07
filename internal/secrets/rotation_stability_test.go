package secrets

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type mutableRotationProvider struct {
	mu    sync.RWMutex
	value Value
}

func (p *mutableRotationProvider) Resolve(_ context.Context, _ string) (Value, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return newValue(p.value.CloneBytes(), p.value.Provider), nil
}

func (p *mutableRotationProvider) set(value string) {
	p.mu.Lock()
	p.value = newValue([]byte(value), "active")
	p.mu.Unlock()
}

func TestRotatingProviderFreezesMutableActiveBackendBeforeStage(t *testing.T) {
	active := &mutableRotationProvider{value: newValue([]byte("old-secret"), "active")}
	candidate := rotationTestProvider{values: map[string]Value{
		"github/token": newValue([]byte("new-secret"), "candidate"),
	}}
	provider := NewRotatingProvider(active)

	if err := provider.Stage(t.Context(), "github/token", candidate); err != nil {
		t.Fatal(err)
	}
	active.set("unexpected-external-change")

	value, err := provider.Resolve(t.Context(), "github/token")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "old-secret" {
		t.Fatalf("Resolve() while staged = %q, want frozen old-secret", got)
	}

	if err := provider.Promote("github/token"); err != nil {
		t.Fatal(err)
	}
	value, err = provider.Resolve(t.Context(), "github/token")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "new-secret" {
		t.Fatalf("Resolve() after promote = %q, want new-secret", got)
	}
}

func TestRotatingProviderConcurrentResolveUsesSingleFrozenBaseline(t *testing.T) {
	active := &mutableRotationProvider{value: newValue([]byte("old-secret"), "active")}
	provider := NewRotatingProvider(active)

	const callers = 32
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := provider.Resolve(t.Context(), "github/token")
			if err != nil {
				errs <- err
				return
			}
			if got := string(value.CloneBytes()); got != "old-secret" {
				errs <- errors.New("concurrent resolution returned a non-baseline value")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	active.set("external-change")
	value, err := provider.Resolve(t.Context(), "github/token")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "old-secret" {
		t.Fatalf("Resolve() after backend mutation = %q, want frozen old-secret", got)
	}
}
