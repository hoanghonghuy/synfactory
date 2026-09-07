package main

import (
	"context"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

type recordingSecurityAuditWriter struct {
	events []securityaudit.Event
	err    error
}

func (w *recordingSecurityAuditWriter) AppendSecurityAudit(_ context.Context, event securityaudit.Event) error {
	if w.err != nil {
		return w.err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	w.events = append(w.events, event)
	return nil
}

func TestAppendSecurityAuditAttributesPrincipalWithoutSensitiveMetadata(t *testing.T) {
	writer := &recordingSecurityAuditWriter{}
	principal := authz.Principal{Subject: "github:user-42"}
	if err := appendSecurityAudit(context.Background(), writer, principal, "security.credentials.read", "security_credentials", "", "success"); err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 1 {
		t.Fatalf("events = %d, want 1", len(writer.events))
	}
	event := writer.events[0]
	if event.ActorID != principal.Subject || event.ActorType != "operator" || event.Action != "security.credentials.read" || len(event.Metadata) != 0 {
		t.Fatalf("event = %#v", event)
	}
}
