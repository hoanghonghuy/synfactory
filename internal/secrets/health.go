package secrets

import (
	"context"
	"sort"
	"sync"
	"time"
)

type RotationState string

const (
	RotationStable   RotationState = "stable"
	RotationStaged   RotationState = "staged"
	RotationRequired RotationState = "required"
)

type DiagnosticState string

const (
	DiagnosticHealthy          DiagnosticState = "healthy"
	DiagnosticExpiring         DiagnosticState = "expiring"
	DiagnosticExpired          DiagnosticState = "expired"
	DiagnosticUnavailable      DiagnosticState = "unavailable"
	DiagnosticRotationStaged   DiagnosticState = "rotation_staged"
	DiagnosticRotationRequired DiagnosticState = "rotation_required"
)

type CredentialMetadata struct {
	Owner     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type CredentialHealth struct {
	LogicalName       string        `json:"logical_name"`
	Provider          string        `json:"provider,omitempty"`
	Owner             string        `json:"owner,omitempty"`
	CreatedAt         time.Time     `json:"created_at,omitempty"`
	ExpiresAt         time.Time     `json:"expires_at,omitempty"`
	LastSuccessfulUse time.Time     `json:"last_successful_use,omitempty"`
	LastFailure       time.Time     `json:"last_failure,omitempty"`
	RotationState     RotationState `json:"rotation_state"`
	Available         bool          `json:"available"`
}

type CredentialDiagnostic struct {
	CredentialHealth
	State DiagnosticState `json:"state"`
}

// TrackingProvider decorates a Provider with concurrency-safe, value-free
// credential health metadata. It never stores secret material.
type TrackingProvider struct {
	provider Provider
	now      func() time.Time

	mu     sync.RWMutex
	health map[string]CredentialHealth
}

func NewTrackingProvider(provider Provider) *TrackingProvider {
	return &TrackingProvider{
		provider: provider,
		now:      time.Now,
		health:   make(map[string]CredentialHealth),
	}
}

func (p *TrackingProvider) Resolve(ctx context.Context, logicalName string) (Value, error) {
	value, err := p.provider.Resolve(ctx, logicalName)
	now := p.now().UTC()

	p.mu.Lock()
	health := p.health[logicalName]
	health.LogicalName = logicalName
	if health.RotationState == "" {
		health.RotationState = RotationStable
	}
	if err == nil {
		health.Provider = value.Provider
		health.LastSuccessfulUse = now
		health.Available = true
	} else {
		health.LastFailure = now
		health.Available = false
	}
	p.health[logicalName] = health
	p.mu.Unlock()

	return value, err
}

func (p *TrackingProvider) Register(logicalName string, metadata CredentialMetadata) {
	p.mu.Lock()
	defer p.mu.Unlock()

	health := p.health[logicalName]
	health.LogicalName = logicalName
	health.Owner = metadata.Owner
	health.CreatedAt = metadata.CreatedAt.UTC()
	health.ExpiresAt = metadata.ExpiresAt.UTC()
	if health.RotationState == "" {
		health.RotationState = RotationStable
	}
	p.health[logicalName] = health
}

func (p *TrackingProvider) MarkRotation(logicalName string, state RotationState) {
	p.mu.Lock()
	defer p.mu.Unlock()

	health := p.health[logicalName]
	health.LogicalName = logicalName
	health.RotationState = state
	p.health[logicalName] = health
}

func (p *TrackingProvider) Snapshot() []CredentialHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]CredentialHealth, 0, len(p.health))
	for _, health := range p.health {
		result = append(result, health)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LogicalName < result[j].LogicalName
	})
	return result
}

// Diagnostics derives operator-safe health states without mutating rotation
// metadata or exposing secret material. An expiry warning window <= 0 disables
// the pre-expiry state while still reporting already-expired credentials.
func (p *TrackingProvider) Diagnostics(now time.Time, expiryWarning time.Duration) []CredentialDiagnostic {
	now = now.UTC()
	snapshot := p.Snapshot()
	result := make([]CredentialDiagnostic, 0, len(snapshot))
	for _, health := range snapshot {
		result = append(result, CredentialDiagnostic{
			CredentialHealth: health,
			State:            diagnosticState(health, now, expiryWarning),
		})
	}
	return result
}

func diagnosticState(health CredentialHealth, now time.Time, expiryWarning time.Duration) DiagnosticState {
	if !health.Available && (!health.LastFailure.IsZero() || !health.LastSuccessfulUse.IsZero()) {
		return DiagnosticUnavailable
	}
	if !health.ExpiresAt.IsZero() && !health.ExpiresAt.After(now) {
		return DiagnosticExpired
	}
	switch health.RotationState {
	case RotationRequired:
		return DiagnosticRotationRequired
	case RotationStaged:
		return DiagnosticRotationStaged
	}
	if expiryWarning > 0 && !health.ExpiresAt.IsZero() && !health.ExpiresAt.After(now.Add(expiryWarning)) {
		return DiagnosticExpiring
	}
	return DiagnosticHealthy
}
