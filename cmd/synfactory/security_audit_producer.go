package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

type securityAuditWriter interface {
	AppendSecurityAudit(context.Context, securityaudit.Event) error
}

func appendSecurityAudit(ctx context.Context, writer securityAuditWriter, principal authz.Principal, action, resourceType, resourceID, outcome string) error {
	if writer == nil {
		return fmt.Errorf("security audit writer is required")
	}
	id, err := newSecurityAuditID()
	if err != nil {
		return err
	}
	actorType := "operator"
	if principal.Subject == "legacy-operator-token" {
		actorType = "legacy_operator"
	}
	return writer.AppendSecurityAudit(ctx, securityaudit.Event{
		ID:           id,
		OccurredAt:   time.Now().UTC(),
		ActorType:    actorType,
		ActorID:      principal.Subject,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Outcome:      outcome,
	})
}

func newSecurityAuditID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate security audit id: %w", err)
	}
	return "audit-" + hex.EncodeToString(entropy[:]), nil
}
