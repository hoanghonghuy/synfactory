package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

func TestSecurityAuditPersistsAndFiltersWithoutSecretMaterial(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := fmt.Sprintf("audit-%d", now.UnixNano())
	actorID := fmt.Sprintf("user-security-test-%d", now.UnixNano())

	event := securityaudit.Event{
		ID:           id,
		OccurredAt:   now,
		ActorType:    "operator",
		ActorID:      actorID,
		Action:       "credential.rotation.stage",
		ResourceType: "credential",
		ResourceID:   "github/token",
		Outcome:      "success",
		RequestID:    "request-security-test",
		Metadata:     json.RawMessage(`{"provider":"file","rotation_state":"staged"}`),
	}
	if err := store.AppendSecurityAudit(ctx, event); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListSecurityAudit(ctx, securityaudit.Filter{
		ActorID:      event.ActorID,
		Action:       event.Action,
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		Outcome:      event.Outcome,
		Since:        now.Add(-time.Second),
		Until:        now.Add(time.Second),
		Limit:        10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("ListSecurityAudit() len = %d, want 1", len(got))
	}
	if got[0].ID != id || got[0].ActorID != event.ActorID || got[0].Action != event.Action {
		t.Fatalf("ListSecurityAudit() = %#v, want persisted event", got[0])
	}
	var gotMetadata, wantMetadata any
	if err := json.Unmarshal(got[0].Metadata, &gotMetadata); err != nil {
		t.Fatalf("decode persisted metadata: %v", err)
	}
	if err := json.Unmarshal(event.Metadata, &wantMetadata); err != nil {
		t.Fatalf("decode expected metadata: %v", err)
	}
	if !reflect.DeepEqual(gotMetadata, wantMetadata) {
		t.Fatalf("metadata = %s, want semantically %s", got[0].Metadata, event.Metadata)
	}

	conflict := event
	conflict.Action = "credential.rotation.promote"
	if err := store.AppendSecurityAudit(ctx, conflict); err == nil {
		t.Fatal("duplicate security audit id succeeded; want fail-closed error")
	}
}
