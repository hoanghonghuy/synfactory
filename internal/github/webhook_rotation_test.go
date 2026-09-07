package github

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolvingWebhookHandlerUsesStablePerRequestSnapshotAndFutureRotation(t *testing.T) {
	current := "before-rotation"
	handler := NewResolvingWebhookHandler(func(context.Context) (string, error) {
		resolved := current
		if current == "before-rotation" {
			current = "after-rotation"
		}
		return resolved, nil
	}, &webhookMemoryStore{}, nil)
	body := []byte(`{"action":"opened","repository":{"id":42,"full_name":"owner/repo","default_branch":"develop"},"issue":{"number":7,"updated_at":"2026-09-03T08:00:00Z"}}`)

	first := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	first.Header.Set("X-GitHub-Event", "issues")
	first.Header.Set("X-GitHub-Delivery", "delivery-before")
	first.Header.Set("X-Hub-Signature-256", signTestBody("before-rotation", body))
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want 202: %s", firstResponse.Code, firstResponse.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	second.Header.Set("X-GitHub-Event", "issues")
	second.Header.Set("X-GitHub-Delivery", "delivery-after")
	second.Header.Set("X-Hub-Signature-256", signTestBody("after-rotation", body))
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusAccepted {
		t.Fatalf("second status = %d, want 202: %s", secondResponse.Code, secondResponse.Body.String())
	}
}

func TestResolvingWebhookHandlerFailsClosedWhenSecretResolverUnavailable(t *testing.T) {
	store := &webhookMemoryStore{}
	handler := NewResolvingWebhookHandler(func(context.Context) (string, error) {
		return "", errors.New("provider unavailable")
	}, store, nil)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader([]byte(`{}`)))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
	if store.inserted {
		t.Fatal("resolver failure must not reach durable event store")
	}
}
