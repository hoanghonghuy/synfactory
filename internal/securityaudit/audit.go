package securityaudit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const MaxMetadataBytes = 16 << 10

var ErrSensitiveMetadata = errors.New("security audit metadata contains sensitive field")

type Event struct {
	ID           string          `json:"id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	ActorType    string          `json:"actor_type"`
	ActorID      string          `json:"actor_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id,omitempty"`
	Outcome      string          `json:"outcome"`
	RequestID    string          `json:"request_id,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

type Filter struct {
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	Since        time.Time
	Until        time.Time
	Limit        int
}

func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("security audit event id is required")
	}
	if e.OccurredAt.IsZero() {
		return errors.New("security audit occurred_at is required")
	}
	for name, value := range map[string]string{
		"actor_type":    e.ActorType,
		"actor_id":      e.ActorID,
		"action":        e.Action,
		"resource_type": e.ResourceType,
		"outcome":       e.Outcome,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("security audit %s is required", name)
		}
	}
	return ValidateMetadata(e.Metadata)
}

func ValidateMetadata(raw json.RawMessage) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if len(raw) > MaxMetadataBytes {
		return fmt.Errorf("security audit metadata exceeds %d bytes", MaxMetadataBytes)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode security audit metadata: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("security audit metadata must contain exactly one JSON value")
		}
		return fmt.Errorf("decode trailing security audit metadata: %w", err)
	}
	if err := rejectSensitive(value); err != nil {
		return err
	}
	return nil
}

func rejectSensitive(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveKey(key) {
				return fmt.Errorf("%w: %s", ErrSensitiveMetadata, key)
			}
			if err := rejectSensitive(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := rejectSensitive(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", ".", "_").Replace(strings.TrimSpace(key)))
	for _, marker := range []string{
		"secret", "token", "password", "authorization", "cookie", "private_key", "api_key",
		"stdin", "stdout", "stderr", "terminal_io", "terminal_input", "terminal_output",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
