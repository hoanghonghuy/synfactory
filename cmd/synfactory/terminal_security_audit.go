package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
	"github.com/hoanghonghuy/synfactory/internal/terminal"
)

const terminalAuditWriteTimeout = 5 * time.Second

type terminalSecurityAuditSink struct {
	writer securityAuditWriter
}

func newTerminalSecurityAuditSink(authorizer authz.RequestAuthorizer) (terminal.AuditSink, error) {
	writer, ok := terminalSecurityAuditWriter(authorizer)
	if !ok {
		return nil, errors.New("durable terminal security audit writer is required")
	}
	return terminalSecurityAuditSink{writer: writer}, nil
}

func terminalSecurityAuditWriter(authorizer authz.RequestAuthorizer) (securityAuditWriter, bool) {
	var session authz.RequestAuthorizer
	switch hybrid := authorizer.(type) {
	case authz.HybridAuthorizer:
		session = hybrid.Session
	case *authz.HybridAuthorizer:
		if hybrid != nil {
			session = hybrid.Session
		}
	default:
		return nil, false
	}

	var store authz.SessionStore
	switch sessionAuthorizer := session.(type) {
	case authz.SessionAuthorizer:
		store = sessionAuthorizer.Store
	case *authz.SessionAuthorizer:
		if sessionAuthorizer != nil {
			store = sessionAuthorizer.Store
		}
	}
	writer, ok := store.(securityAuditWriter)
	return writer, ok && writer != nil
}

func (s terminalSecurityAuditSink) Record(event terminal.AuditEvent) error {
	if s.writer == nil {
		return errors.New("durable terminal security audit writer is required")
	}
	operator := strings.TrimSpace(event.Operator)
	if operator == "" {
		return errors.New("terminal audit operator is required")
	}
	action := ""
	occurredAt := event.StartedAt.UTC()
	switch event.Event {
	case "opened":
		action = "terminal.session.opened"
	case "closed":
		action = "terminal.session.closed"
		if event.EndedAt != nil {
			occurredAt = event.EndedAt.UTC()
		}
	default:
		return fmt.Errorf("unsupported terminal audit event %q", event.Event)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	metadata, err := json.Marshal(map[string]any{
		"target_id":   event.TargetID,
		"target_kind": event.TargetKind,
		"reason":      event.Reason,
	})
	if err != nil {
		return fmt.Errorf("encode terminal security audit metadata: %w", err)
	}
	id, err := newSecurityAuditID()
	if err != nil {
		return err
	}
	actorType := "operator"
	if operator == "legacy-operator-token" {
		actorType = "legacy_operator"
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminalAuditWriteTimeout)
	defer cancel()
	return s.writer.AppendSecurityAudit(ctx, securityaudit.Event{
		ID:           id,
		OccurredAt:   occurredAt,
		ActorType:    actorType,
		ActorID:      operator,
		Action:       action,
		ResourceType: "terminal_session",
		ResourceID:   event.SessionID,
		Outcome:      "success",
		Metadata:     metadata,
	})
}

type terminalAuditFanout []terminal.AuditSink

func (s terminalAuditFanout) Record(event terminal.AuditEvent) error {
	for _, sink := range s {
		if sink == nil {
			continue
		}
		if err := sink.Record(event); err != nil {
			return err
		}
	}
	return nil
}
