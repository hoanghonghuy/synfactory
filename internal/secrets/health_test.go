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

type metadataHealthTestProvider struct {
	healthTestProvider
	metadata CredentialMetadata
	err      error
}

func (p metadataHealthTestProvider) Metadata(context.Context, string) (CredentialMetadata, error) {
	return p.metadata, p.err
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

func TestTrackingProviderCollectsOptionalProviderMetadata(t *testing.T) {
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	provider := NewTrackingProvider(metadataHealthTestProvider{
		healthTestProvider: healthTestProvider{value: newValue([]byte("secret"), "file")},
		metadata:           CredentialMetadata{CreatedAt: created, ExpiresAt: expires},
	})

	if _, err := provider.Resolve(t.Context(), "github/token"); err != nil {
		t.Fatal(err)
	}
	health := provider.Snapshot()[0]
	if !health.CreatedAt.Equal(created) || !health.ExpiresAt.Equal(expires) {
		t.Fatalf("provider metadata not tracked: %#v", health)
	}
}

func TestTrackingProviderIgnoresMetadataFailureAfterSuccessfulResolve(t *testing.T) {
	provider := NewTrackingProvider(metadataHealthTestProvider{
		healthTestProvider: healthTestProvider{value: newValue([]byte("secret"), "file")},
		err:                errors.New("metadata unavailable"),
	})

	if _, err := provider.Resolve(t.Context(), "github/token"); err != nil {
		t.Fatalf("Resolve() inherited metadata error: %v", err)
	}
	health := provider.Snapshot()[0]
	if !health.Available || !health.CreatedAt.IsZero() || !health.ExpiresAt.IsZero() {
		t.Fatalf("health = %#v", health)
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

func TestTrackingProviderSnapshotIsDeterministic(t *testing.T) {
	provider := NewTrackingProvider(healthTestProvider{})
	provider.Register("zeta/token", CredentialMetadata{Owner: "zeta"})
	provider.Register("alpha/token", CredentialMetadata{Owner: "alpha"})

	snapshot := provider.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("Snapshot() len = %d, want 2", len(snapshot))
	}
	if snapshot[0].LogicalName != "alpha/token" || snapshot[1].LogicalName != "zeta/token" {
		t.Fatalf("Snapshot() order = %q, %q", snapshot[0].LogicalName, snapshot[1].LogicalName)
	}
}

func TestTrackingProviderDiagnosticsClassifiesCredentialHealth(t *testing.T) {
	now := time.Date(2026, 9, 7, 7, 0, 0, 0, time.UTC)
	provider := NewTrackingProvider(healthTestProvider{})
	provider.Register("healthy", CredentialMetadata{ExpiresAt: now.Add(72 * time.Hour)})
	provider.Register("expiring", CredentialMetadata{ExpiresAt: now.Add(12 * time.Hour)})
	provider.Register("expired", CredentialMetadata{ExpiresAt: now.Add(-time.Minute)})
	provider.MarkRotation("required", RotationRequired)
	provider.MarkRotation("staged", RotationStaged)

	provider.mu.Lock()
	unavailable := provider.health["unavailable"]
	unavailable.LogicalName = "unavailable"
	unavailable.LastFailure = now.Add(-time.Minute)
	unavailable.Available = false
	unavailable.RotationState = RotationStable
	provider.health["unavailable"] = unavailable
	provider.mu.Unlock()

	diagnostics := provider.Diagnostics(now, 24*time.Hour)
	states := make(map[string]DiagnosticState, len(diagnostics))
	for _, diagnostic := range diagnostics {
		states[diagnostic.LogicalName] = diagnostic.State
	}

	want := map[string]DiagnosticState{
		"healthy":     DiagnosticHealthy,
		"expiring":    DiagnosticExpiring,
		"expired":     DiagnosticExpired,
		"required":    DiagnosticRotationRequired,
		"staged":      DiagnosticRotationStaged,
		"unavailable": DiagnosticUnavailable,
	}
	for logicalName, wantState := range want {
		if got := states[logicalName]; got != wantState {
			t.Fatalf("state for %q = %q, want %q", logicalName, got, wantState)
		}
	}
}

func TestTrackingProviderDiagnosticsDoesNotMutateRotationState(t *testing.T) {
	now := time.Date(2026, 9, 7, 7, 0, 0, 0, time.UTC)
	provider := NewTrackingProvider(healthTestProvider{})
	provider.Register("github/token", CredentialMetadata{ExpiresAt: now.Add(time.Hour)})

	diagnostics := provider.Diagnostics(now, 24*time.Hour)
	if diagnostics[0].State != DiagnosticExpiring {
		t.Fatalf("diagnostic state = %q, want %q", diagnostics[0].State, DiagnosticExpiring)
	}
	if got := provider.Snapshot()[0].RotationState; got != RotationStable {
		t.Fatalf("rotation state mutated to %q, want %q", got, RotationStable)
	}
}
