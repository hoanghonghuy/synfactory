package secrets

import (
	"context"
	"errors"
	"testing"
)

type rotationTestProvider struct {
	values map[string]Value
	err    error
}

func (p rotationTestProvider) Resolve(_ context.Context, logicalName string) (Value, error) {
	if p.err != nil {
		return Value{}, p.err
	}
	value, ok := p.values[logicalName]
	if !ok {
		return Value{}, ErrNotFound
	}
	return newValue(value.CloneBytes(), value.Provider), nil
}

func TestRotatingProviderKeepsActiveUntilPromotion(t *testing.T) {
	active := rotationTestProvider{values: map[string]Value{
		"github/token": newValue([]byte("old-secret"), "active"),
	}}
	candidate := rotationTestProvider{values: map[string]Value{
		"github/token": newValue([]byte("new-secret"), "candidate"),
	}}
	provider := NewRotatingProvider(active)

	before, err := provider.Resolve(t.Context(), "github/token")
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Stage(t.Context(), "github/token", candidate); err != nil {
		t.Fatal(err)
	}
	whileStaged, err := provider.Resolve(t.Context(), "github/token")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(whileStaged.CloneBytes()); got != "old-secret" {
		t.Fatalf("staged Resolve() = %q, want old-secret", got)
	}

	if err := provider.Promote("github/token"); err != nil {
		t.Fatal(err)
	}
	after, err := provider.Resolve(t.Context(), "github/token")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(after.CloneBytes()); got != "new-secret" {
		t.Fatalf("promoted Resolve() = %q, want new-secret", got)
	}
	if got := string(before.CloneBytes()); got != "old-secret" {
		t.Fatalf("previously resolved value changed to %q", got)
	}
}

func TestRotatingProviderFailedStageDoesNotChangeActive(t *testing.T) {
	active := rotationTestProvider{values: map[string]Value{
		"operator/token": newValue([]byte("active-secret"), "active"),
	}}
	provider := NewRotatingProvider(active)
	candidateErr := errors.New("candidate backend unavailable")

	if err := provider.Stage(t.Context(), "operator/token", rotationTestProvider{err: candidateErr}); !errors.Is(err, candidateErr) {
		t.Fatalf("Stage() error = %v, want %v", err, candidateErr)
	}
	if provider.HasStaged("operator/token") {
		t.Fatal("failed Stage() left a staged credential")
	}
	value, err := provider.Resolve(t.Context(), "operator/token")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "active-secret" {
		t.Fatalf("Resolve() = %q, want active-secret", got)
	}
}

func TestRotatingProviderDiscardPreservesActive(t *testing.T) {
	active := rotationTestProvider{values: map[string]Value{
		"github/webhook-secret": newValue([]byte("active-secret"), "active"),
	}}
	candidate := rotationTestProvider{values: map[string]Value{
		"github/webhook-secret": newValue([]byte("candidate-secret"), "candidate"),
	}}
	provider := NewRotatingProvider(active)

	if err := provider.Stage(t.Context(), "github/webhook-secret", candidate); err != nil {
		t.Fatal(err)
	}
	if !provider.HasStaged("github/webhook-secret") {
		t.Fatal("HasStaged() = false, want true")
	}
	if err := provider.DiscardStaged("github/webhook-secret"); err != nil {
		t.Fatal(err)
	}
	if provider.HasStaged("github/webhook-secret") {
		t.Fatal("HasStaged() = true after discard")
	}
	value, err := provider.Resolve(t.Context(), "github/webhook-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "active-secret" {
		t.Fatalf("Resolve() = %q, want active-secret", got)
	}
}

func TestRotatingProviderPromotionRequiresStage(t *testing.T) {
	provider := NewRotatingProvider(rotationTestProvider{})
	if err := provider.Promote("github/token"); !errors.Is(err, ErrNoStagedRotation) {
		t.Fatalf("Promote() error = %v, want ErrNoStagedRotation", err)
	}
}
