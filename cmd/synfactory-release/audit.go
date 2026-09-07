package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
	releasefactory "github.com/hoanghonghuy/synfactory/internal/release"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

type releaseAuditWriter interface {
	AppendSecurityAudit(context.Context, securityaudit.Event) error
}

type releaseAuditor struct {
	writer releaseAuditWriter
	close  func() error
	now    func() time.Time
}

type releaseAuditIdentity struct {
	ActorType  string
	ActorID    string
	RequestID  string
	Repository string
	RunAttempt string
}

func openReleaseAuditor(ctx context.Context, databaseURL string) (*releaseAuditor, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("release audit database URL is required")
	}
	store, err := postgres.Open(ctx, databaseURL, postgres.Options{})
	if err != nil {
		return nil, fmt.Errorf("open release audit store: %w", err)
	}
	return &releaseAuditor{
		writer: store,
		close:  store.Close,
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

func (a *releaseAuditor) Close() error {
	if a == nil || a.close == nil {
		return nil
	}
	return a.close()
}

func (a *releaseAuditor) RecordPublish(ctx context.Context, identity releaseAuditIdentity, input releasefactory.PublishInput, manifest releasefactory.Manifest, outcome string, reused bool) error {
	return a.record(ctx, identity, input, manifest, "release.publish", outcome, reused)
}

func (a *releaseAuditor) RecordVerification(ctx context.Context, identity releaseAuditIdentity, input releasefactory.PublishInput, outcome string) error {
	return a.record(ctx, identity, input, releasefactory.Manifest{}, "release.verify", outcome, false)
}

func (a *releaseAuditor) record(ctx context.Context, identity releaseAuditIdentity, input releasefactory.PublishInput, manifest releasefactory.Manifest, action, outcome string, reused bool) error {
	if a == nil || a.writer == nil {
		return fmt.Errorf("release audit writer is required")
	}
	if strings.TrimSpace(identity.ActorID) == "" {
		return fmt.Errorf("release audit actor id is required")
	}
	id, err := newReleaseAuditID()
	if err != nil {
		return err
	}
	metadata := map[string]any{
		"evidence_sha256":          input.Evidence.ManifestSHA256,
		"repository":               strings.TrimSpace(identity.Repository),
		"reused_existing_manifest": reused,
		"run_attempt":              strings.TrimSpace(identity.RunAttempt),
		"source_sha":               input.SourceSHA,
		"version":                  input.Version,
		"web_lock_sha256":          input.Evidence.WebLockSHA256,
	}
	if len(manifest.Images) > 0 {
		digests := make(map[string]string, len(manifest.Images))
		for _, image := range manifest.Images {
			digests[image.Name] = image.Digest
		}
		metadata["image_digests"] = digests
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode release audit metadata: %w", err)
	}
	event := securityaudit.Event{
		ID:           id,
		OccurredAt:   a.now(),
		ActorType:    strings.TrimSpace(identity.ActorType),
		ActorID:      strings.TrimSpace(identity.ActorID),
		Action:       strings.TrimSpace(action),
		ResourceType: "release",
		ResourceID:   input.Version + "@" + input.SourceSHA,
		Outcome:      strings.TrimSpace(outcome),
		RequestID:    strings.TrimSpace(identity.RequestID),
		Metadata:     raw,
	}
	if err := a.writer.AppendSecurityAudit(ctx, event); err != nil {
		return fmt.Errorf("append release security audit: %w", err)
	}
	return nil
}

func newReleaseAuditID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate release audit id: %w", err)
	}
	return "audit-" + hex.EncodeToString(entropy[:]), nil
}
