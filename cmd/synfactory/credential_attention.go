package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

const credentialAttentionInterval = 5 * time.Minute

type credentialAttentionStore interface {
	ActiveAttention(context.Context, string, time.Time) ([]attention.Item, error)
	UpsertAttention(context.Context, attention.Item) (attention.Item, error)
}

func configuredCredentialAttention(store credentialAttentionStore, cfg config.Config) namedComponent {
	return namedComponent{
		name: "credential health attention",
		run: func(ctx context.Context) error {
			return runCredentialAttention(ctx, store, cfg, credentialAttentionInterval)
		},
	}
}

func runCredentialAttention(ctx context.Context, store credentialAttentionStore, cfg config.Config, interval time.Duration) error {
	if interval <= 0 {
		interval = credentialAttentionInterval
	}
	for {
		now := time.Now().UTC()
		diagnostics, err := credentialDiagnostics(ctx, cfg, now)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			slog.Warn("credential health probe failed", "error", err)
		} else if err := reconcileCredentialAttention(ctx, store, diagnostics, now); err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			slog.Warn("credential attention reconciliation failed", "error", err)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func reconcileCredentialAttention(ctx context.Context, store credentialAttentionStore, diagnostics []secrets.CredentialDiagnostic, now time.Time) error {
	if store == nil {
		return errors.New("credential attention requires store")
	}
	now = now.UTC()
	active, err := store.ActiveAttention(ctx, "", now)
	if err != nil {
		return fmt.Errorf("list active attention: %w", err)
	}

	activeByKey := make(map[string]attention.Item, len(active))
	for _, item := range active {
		if item.Kind == attention.KindCredential {
			activeByKey[item.DedupeKey] = item
		}
	}

	seen := make(map[string]bool, len(diagnostics))
	for _, diagnostic := range diagnostics {
		key, err := attention.DedupeKey("", "", attention.KindCredential, diagnostic.LogicalName)
		if err != nil {
			return err
		}
		seen[key] = true

		severity, title, summary, actionable := credentialAttentionMessage(diagnostic)
		existing, exists := activeByKey[key]
		if !actionable {
			if exists {
				resolved, err := existing.Resolve("system:credential-monitor", now, true)
				if err != nil {
					return fmt.Errorf("resolve credential attention %s: %w", diagnostic.LogicalName, err)
				}
				if _, err := store.UpsertAttention(ctx, resolved); err != nil {
					return fmt.Errorf("persist resolved credential attention %s: %w", diagnostic.LogicalName, err)
				}
			}
			continue
		}

		item := existing
		if !exists {
			item = attention.Item{
				ID:        credentialAttentionID(key),
				DedupeKey: key,
				Kind:      attention.KindCredential,
				State:     attention.StateOpen,
				CreatedAt: now,
			}
		}
		item.Severity = severity
		item.Title = title
		item.Summary = summary
		item.UpdatedAt = now
		if _, err := store.UpsertAttention(ctx, item); err != nil {
			return fmt.Errorf("persist credential attention %s: %w", diagnostic.LogicalName, err)
		}
	}

	for key, existing := range activeByKey {
		if seen[key] {
			continue
		}
		resolved, err := existing.Resolve("system:credential-monitor", now, true)
		if err != nil {
			return fmt.Errorf("resolve stale credential attention %s: %w", existing.ID, err)
		}
		if _, err := store.UpsertAttention(ctx, resolved); err != nil {
			return fmt.Errorf("persist stale credential attention %s: %w", existing.ID, err)
		}
	}
	return nil
}

func credentialAttentionMessage(diagnostic secrets.CredentialDiagnostic) (attention.Severity, string, string, bool) {
	name := diagnostic.LogicalName
	switch diagnostic.State {
	case secrets.DiagnosticUnavailable:
		return attention.SeverityCritical, "Credential unavailable", fmt.Sprintf("Logical credential %q is unavailable; dependent operations require operator action.", name), true
	case secrets.DiagnosticExpired:
		return attention.SeverityCritical, "Credential expired", fmt.Sprintf("Logical credential %q has expired and must be rotated before dependent operations can recover.", name), true
	case secrets.DiagnosticExpiring:
		return attention.SeverityWarning, "Credential expiring", fmt.Sprintf("Logical credential %q is within the configured pre-expiry warning window and should be rotated.", name), true
	case secrets.DiagnosticRotationRequired:
		return attention.SeverityWarning, "Credential rotation required", fmt.Sprintf("Logical credential %q is marked rotation-required and needs operator action.", name), true
	default:
		return "", "", "", false
	}
}

func credentialAttentionID(dedupeKey string) string {
	sum := sha256.Sum256([]byte(dedupeKey))
	return "credential-attention-" + hex.EncodeToString(sum[:12])
}
