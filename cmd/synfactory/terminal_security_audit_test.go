package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/terminal"
)

type terminalAuditSinkFunc func(terminal.AuditEvent) error

func (f terminalAuditSinkFunc) Record(event terminal.AuditEvent) error { return f(event) }

func TestTerminalSecurityAuditSinkPersistsValueFreeLifecycleMetadata(t *testing.T) {
	writer := &recordingSecurityAuditWriter{}
	sink := terminalSecurityAuditSink{writer: writer}
	started := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	ended := started.Add(2 * time.Minute)

	if err := sink.Record(terminal.AuditEvent{
		Event:      "opened",
		SessionID:  "term-7",
		Operator:   "github:user-42",
		TargetID:   "worker-a",
		TargetKind: terminal.TargetSSH,
		StartedAt:  started,
	}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Record(terminal.AuditEvent{
		Event:      "closed",
		SessionID:  "term-7",
		Operator:   "github:user-42",
		TargetID:   "worker-a",
		TargetKind: terminal.TargetSSH,
		StartedAt:  started,
		EndedAt:    &ended,
		Reason:     "explicit_close",
	}); err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 2 {
		t.Fatalf("events = %d, want 2", len(writer.events))
	}
	opened, closed := writer.events[0], writer.events[1]
	if opened.Action != "terminal.session.opened" || opened.ResourceType != "terminal_session" || opened.ResourceID != "term-7" || opened.ActorID != "github:user-42" || !opened.OccurredAt.Equal(started) {
		t.Fatalf("opened event = %#v", opened)
	}
	if closed.Action != "terminal.session.closed" || !closed.OccurredAt.Equal(ended) {
		t.Fatalf("closed event = %#v", closed)
	}
	var metadata map[string]any
	if err := json.Unmarshal(closed.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["target_id"] != "worker-a" || metadata["target_kind"] != string(terminal.TargetSSH) || metadata["reason"] != "explicit_close" {
		t.Fatalf("metadata = %#v", metadata)
	}
	for _, forbidden := range []string{"stdin", "stdout", "stderr", "terminal_input", "terminal_output", "token", "secret"} {
		if _, ok := metadata[forbidden]; ok {
			t.Fatalf("sensitive terminal metadata key %q persisted", forbidden)
		}
	}
}

func TestTerminalAuditFanoutFailsBeforeSecondarySinkWhenDurableWriteFails(t *testing.T) {
	writeErr := errors.New("audit unavailable")
	writer := &recordingSecurityAuditWriter{err: writeErr}
	secondaryCalls := 0
	fanout := terminalAuditFanout{
		terminalSecurityAuditSink{writer: writer},
		terminalAuditSinkFunc(func(terminal.AuditEvent) error {
			secondaryCalls++
			return nil
		}),
	}

	err := fanout.Record(terminal.AuditEvent{
		Event:      "opened",
		SessionID:  "term-9",
		Operator:   "github:user-42",
		TargetID:   "worker-a",
		TargetKind: terminal.TargetLocal,
		StartedAt:  time.Now().UTC(),
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("err = %v, want %v", err, writeErr)
	}
	if secondaryCalls != 0 {
		t.Fatalf("secondary audit calls = %d, want 0 after durable failure", secondaryCalls)
	}
}

func TestTerminalSecurityAuditSinkRejectsUnknownLifecycleEvent(t *testing.T) {
	writer := &recordingSecurityAuditWriter{}
	err := (terminalSecurityAuditSink{writer: writer}).Record(terminal.AuditEvent{
		Event:      "data",
		SessionID:  "term-10",
		Operator:   "github:user-42",
		TargetID:   "worker-a",
		TargetKind: terminal.TargetLocal,
		StartedAt:  time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected unsupported terminal lifecycle event to fail closed")
	}
	if len(writer.events) != 0 {
		t.Fatalf("events = %d, want 0", len(writer.events))
	}
}
