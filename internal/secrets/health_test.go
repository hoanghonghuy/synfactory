package secrets

import (
	"context"
	"errors"
	"testing"
	"time"
)

type healthTestProvider struct {
	value Value
	err   error
}

func (p healthTestProvider) Resolve(context.Context, string) (Value, error) {
	return p.value, p.err
}

func TestTrackingProviderRecordsSuccessfulUseWithoutSecretMaterial(t *testing.T) {
	provider := NewTrackingProvider(healthTestProvider{value: newValue([]byte("super-secret"), "env")})
	provider.now = func() time.Time { return time.Date(2026, 9, 7, 6, 0, 0, 0, time.UTC) }

	value, err := provider.Resolve(t.Context(), "github/token")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "super-secret" {
		t.Fatalf("resolved secret = %q, want super-secret", got)
	}

	snapshot := provider.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("Snapshot() len = %d, want 1", len(snapshot))
	}
	health := snapshot[0]
	if health.LogicalName != "github/token" || health.Provider != "env" || !health.Available {
		t.Fatalf("health = %#v", health)
	}
	if health.LastSuccessfulUse != provider.now() {
		t.Fatalf("last successful use = %v, want %v", health.LastSuccessfulUse, provider.now())
	}
}

func TestTrackingProviderRecordsFailureWithoutDestroyingRegistration(t *testing.T) {
	provider := NewTrackingProvider(healthTestProvider{err: errors.New("backend unavailable")})
	provider.now = func() time.Time { return time.Date(2026, 9, 7, 6, 30, 0, 0, time.UTC) }
	created := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	provider.Register("github/token", CredentialMetadata{Owner: "platform", CreatedAt: created, ExpiresAt: expires})

	if _, err := provider.Resolve(t.Context(), "github/token"); err == nil {
		t.Fatal("Resolve() error = nil, want backend error")
	}

	health := provider.Snapshot()[0]
	if health.Owner != "platform" || health.CreatedAt != created || health.ExpiresAt != expires {
		t.Fatalf("registered metadata lost: %#v", health)
	}
	if health.Available {
		t.Fatal("Available = true, want false")
	}
	if health.LastFailure != provider.now() {
		t.Fatalf("last failure = %v, want %v", health.LastFailure, provider.now())
	}
}

func TestTrackingProviderTracksRotationState(t *testing.T) {
	provider := NewTrackingProvider(healthTestProvider{})
	provider.MarkRotation("operator/token", RotationStaged)

	health := provider.Snapshot()[0]
	if health.RotationState != RotationStaged {
		t.Fatalf("rotation state = %q, want %q", health.RotationState, RotationStaged)
	}
}
