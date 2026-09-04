package terminal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileAuditSinkPersistsOnlySessionMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "events.jsonl")
	sink, err := NewFileAuditSink(path)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	ended := started.Add(time.Minute)
	event := AuditEvent{
		Event:      "closed",
		SessionID:  "term-1",
		Operator:   "operator",
		TargetID:   "control",
		TargetKind: TargetLocal,
		StartedAt:  started,
		EndedAt:    &ended,
		Reason:     "explicit_close",
	}
	if err := sink.Record(event); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "command") || strings.Contains(string(raw), "output") {
		t.Fatalf("audit log contains terminal I/O-like fields: %s", raw)
	}
	var got AuditEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &got); err != nil {
		t.Fatal(err)
	}
	if got.SessionID != event.SessionID || got.Operator != event.Operator || got.TargetID != event.TargetID || got.Reason != event.Reason {
		t.Fatalf("unexpected persisted audit event: %+v", got)
	}
}

func TestFileAuditSinkRequiresAbsolutePath(t *testing.T) {
	if _, err := NewFileAuditSink("relative/events.jsonl"); err == nil {
		t.Fatal("NewFileAuditSink() error = nil, want absolute-path validation error")
	}
}
