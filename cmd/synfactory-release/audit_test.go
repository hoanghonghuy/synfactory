package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	releasefactory "github.com/hoanghonghuy/synfactory/internal/release"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

type releaseAuditTestWriter struct {
	events []securityaudit.Event
	err    error
}

func (w *releaseAuditTestWriter) AppendSecurityAudit(_ context.Context, event securityaudit.Event) error {
	if w.err != nil {
		return w.err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	w.events = append(w.events, event)
	return nil
}

func TestReleaseAuditorWritesValueFreeAttributionAndDigests(t *testing.T) {
	writer := &releaseAuditTestWriter{}
	auditor := &releaseAuditor{writer: writer, now: func() time.Time {
		return time.Date(2026, 9, 8, 2, 3, 4, 0, time.UTC)
	}}
	input := releasefactory.PublishInput{
		Version:   "v1.2.3",
		SourceSHA: strings.Repeat("a", 40),
		Evidence: releasefactory.Evidence{
			ManifestSHA256: strings.Repeat("b", 64),
			WebLockSHA256:  strings.Repeat("c", 64),
		},
	}
	manifest := releasefactory.Manifest{Images: []releasefactory.Image{{Name: "control", Digest: "sha256:" + strings.Repeat("d", 64)}}}
	identity := releaseAuditIdentity{
		ActorType: "github_actions", ActorID: "release-bot", RequestID: "run-42", Repository: "acme/widget", RunAttempt: "2",
	}

	if err := auditor.RecordPublish(context.Background(), identity, input, manifest, "succeeded", false); err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 1 {
		t.Fatalf("events=%d want 1", len(writer.events))
	}
	event := writer.events[0]
	if event.ActorType != "github_actions" || event.ActorID != "release-bot" || event.RequestID != "run-42" {
		t.Fatalf("unexpected attribution: %#v", event)
	}
	if event.Action != "release.publish" || event.ResourceType != "release" || event.ResourceID != "v1.2.3@"+strings.Repeat("a", 40) || event.Outcome != "succeeded" {
		t.Fatalf("unexpected audit contract: %#v", event)
	}
	var metadata map[string]any
	if err := json.Unmarshal(event.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["repository"] != "acme/widget" || metadata["run_attempt"] != "2" || metadata["reused_existing_manifest"] != false {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if err := securityaudit.ValidateMetadata(event.Metadata); err != nil {
		t.Fatalf("unsafe metadata: %v", err)
	}
}

func TestReleaseAuditorFailsClosedOnAuditPersistenceError(t *testing.T) {
	writer := &releaseAuditTestWriter{err: errors.New("audit unavailable")}
	auditor := &releaseAuditor{writer: writer, now: time.Now}
	input := releasefactory.PublishInput{Version: "v1.2.3", SourceSHA: strings.Repeat("a", 40)}
	err := auditor.RecordPublish(context.Background(), releaseAuditIdentity{ActorType: "operator", ActorID: "alice"}, input, releasefactory.Manifest{}, "started", false)
	if err == nil || !strings.Contains(err.Error(), "append release security audit") {
		t.Fatalf("expected fail-closed audit error, got %v", err)
	}
}

func TestReleaseAuditorRequiresActorIdentity(t *testing.T) {
	writer := &releaseAuditTestWriter{}
	auditor := &releaseAuditor{writer: writer, now: time.Now}
	err := auditor.RecordPublish(context.Background(), releaseAuditIdentity{ActorType: "operator"}, releasefactory.PublishInput{Version: "v1", SourceSHA: "sha"}, releasefactory.Manifest{}, "started", false)
	if err == nil || !strings.Contains(err.Error(), "actor id") {
		t.Fatalf("expected actor identity error, got %v", err)
	}
	if len(writer.events) != 0 {
		t.Fatalf("unexpected audit append: %d", len(writer.events))
	}
}
