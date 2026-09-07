package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

type fakeCredentialAttentionStore struct {
	items map[string]attention.Item
}

func (s *fakeCredentialAttentionStore) ActiveAttention(_ context.Context, _ string, _ time.Time) ([]attention.Item, error) {
	result := make([]attention.Item, 0, len(s.items))
	for _, item := range s.items {
		if item.State != attention.StateResolved {
			result = append(result, item)
		}
	}
	return result, nil
}

func (s *fakeCredentialAttentionStore) UpsertAttention(_ context.Context, item attention.Item) (attention.Item, error) {
	if s.items == nil {
		s.items = make(map[string]attention.Item)
	}
	s.items[item.DedupeKey] = item
	return item, nil
}

func TestReconcileCredentialAttentionPreservesOperatorStateAndResolvesRecovery(t *testing.T) {
	now := time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)
	cfg := config.Config{GitHubAuthMode: "pat"}
	key, err := attention.DedupeKey("", "", attention.KindCredential, "github/token")
	if err != nil {
		t.Fatal(err)
	}
	acknowledged := now.Add(-time.Hour)
	store := &fakeCredentialAttentionStore{items: map[string]attention.Item{
		key: {
			ID:             credentialAttentionID(key),
			DedupeKey:      key,
			Kind:           attention.KindCredential,
			Severity:       attention.SeverityWarning,
			State:          attention.StateAcknowledged,
			AssignedTo:     "operator@example.test",
			AcknowledgedAt: &acknowledged,
			CreatedAt:      now.Add(-2 * time.Hour),
			UpdatedAt:      acknowledged,
		},
	}}

	diagnostics := []secrets.CredentialDiagnostic{{
		CredentialHealth: secrets.CredentialHealth{LogicalName: "github/token"},
		State:            secrets.DiagnosticUnavailable,
	}}
	if err := reconcileCredentialAttention(context.Background(), store, cfg, diagnostics, now); err != nil {
		t.Fatal(err)
	}
	item := store.items[key]
	if item.State != attention.StateAcknowledged || item.AssignedTo != "operator@example.test" {
		t.Fatalf("operator state was reset: %#v", item)
	}
	if item.Severity != attention.SeverityCritical {
		t.Fatalf("severity = %q, want critical", item.Severity)
	}

	diagnostics[0].State = secrets.DiagnosticHealthy
	if err := reconcileCredentialAttention(context.Background(), store, cfg, diagnostics, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	item = store.items[key]
	if item.State != attention.StateResolved || item.ResolvedAt == nil {
		t.Fatalf("recovered credential was not resolved: %#v", item)
	}
	if item.AssignedTo != "system:credential-monitor" {
		t.Fatalf("resolver = %q, want system:credential-monitor", item.AssignedTo)
	}
}

func TestReconcileCredentialAttentionIgnoresOptionalOAuthSecret(t *testing.T) {
	store := &fakeCredentialAttentionStore{}
	diagnostics := []secrets.CredentialDiagnostic{{
		CredentialHealth: secrets.CredentialHealth{LogicalName: "github/oauth-client-secret"},
		State:            secrets.DiagnosticUnavailable,
	}}
	if err := reconcileCredentialAttention(context.Background(), store, config.Config{GitHubAuthMode: "pat"}, diagnostics, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(store.items) != 0 {
		t.Fatalf("optional OAuth secret created attention: %#v", store.items)
	}
}

func TestCredentialAttentionMessageDoesNotExposeProviderOrValues(t *testing.T) {
	diagnostic := secrets.CredentialDiagnostic{
		CredentialHealth: secrets.CredentialHealth{
			LogicalName: "github/oauth-client-secret",
			Provider:    "file:/run/secrets/private-value",
		},
		State: secrets.DiagnosticExpiring,
	}
	severity, _, summary, actionable := credentialAttentionMessage(diagnostic)
	if !actionable || severity != attention.SeverityWarning {
		t.Fatalf("unexpected classification: actionable=%v severity=%q", actionable, severity)
	}
	if summary == "" || strings.Contains(summary, diagnostic.Provider) {
		t.Fatalf("summary leaked provider metadata: %q", summary)
	}
}
