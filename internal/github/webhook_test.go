package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type webhookMemoryStore struct {
	repository postgres.Repository
	event      postgres.InboxEvent
	inserted   bool
}

func (s *webhookMemoryStore) UpsertRepository(_ context.Context, repository postgres.Repository) (postgres.Repository, error) {
	s.repository = repository
	return repository, nil
}

func (s *webhookMemoryStore) PutEvent(_ context.Context, event postgres.InboxEvent) (postgres.InboxEvent, bool, error) {
	event.ID = 1
	s.event = event
	if s.inserted {
		return event, false, nil
	}
	s.inserted = true
	return event, true, nil
}

func TestWebhookPersistsBeforeAcknowledgement(t *testing.T) {
	store := &webhookMemoryStore{}
	woken := 0
	handler := NewWebhookHandler("secret", store, func() { woken++ })
	body := []byte(`{
		"action":"opened",
		"repository":{"id":42,"full_name":"owner/repo","default_branch":"develop"},
		"issue":{"number":7,"updated_at":"2026-09-03T08:00:00Z"}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", signTestBody("secret", body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.repository.ID != "github:42" || store.event.Kind != KindIssueChanged {
		t.Fatalf("unexpected persisted state: repo=%+v event=%+v", store.repository, store.event)
	}
	if woken != 1 {
		t.Fatalf("expected processor wake, got %d", woken)
	}
}

func TestWebhookRejectsInvalidSignature(t *testing.T) {
	store := &webhookMemoryStore{}
	handler := NewWebhookHandler("secret", store, nil)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if store.inserted {
		t.Fatal("invalid request must not reach durable store")
	}
}

func signTestBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
