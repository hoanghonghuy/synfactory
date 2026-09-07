package securityaudit

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEventValidateAcceptsValueFreeMetadata(t *testing.T) {
	event := Event{
		ID:           "audit-1",
		OccurredAt:   time.Now().UTC(),
		ActorType:    "operator",
		ActorID:      "user-1",
		Action:       "credential.rotation.stage",
		ResourceType: "credential",
		ResourceID:   "github/token",
		Outcome:      "success",
		Metadata:     json.RawMessage(`{"provider":"file","rotation_state":"staged","request":{"id":"req-1"}}`),
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateMetadataRejectsSensitiveFieldsRecursively(t *testing.T) {
	for _, raw := range []string{
		`{"access_token":"abc"}`,
		`{"nested":{"client-secret":"abc"}}`,
		`{"terminal":{"stdout":"data"}}`,
		`{"authorization":"Bearer abc"}`,
		`{"api_key":"abc"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			err := ValidateMetadata(json.RawMessage(raw))
			if !errors.Is(err, ErrSensitiveMetadata) {
				t.Fatalf("ValidateMetadata() error = %v, want ErrSensitiveMetadata", err)
			}
		})
	}
}

func TestValidateMetadataRejectsOversize(t *testing.T) {
	raw := json.RawMessage(`{"note":"` + strings.Repeat("x", MaxMetadataBytes) + `"}`)
	if err := ValidateMetadata(raw); err == nil {
		t.Fatal("ValidateMetadata() error = nil, want oversize error")
	}
}

func TestValidateMetadataRejectsTrailingJSON(t *testing.T) {
	if err := ValidateMetadata(json.RawMessage(`{"provider":"file"}{"provider":"env"}`)); err == nil {
		t.Fatal("ValidateMetadata() error = nil, want trailing JSON error")
	}
}
