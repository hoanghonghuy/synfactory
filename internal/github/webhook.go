package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/hoanghonghuy/synfactory/internal/events"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

const maxWebhookBodyBytes int64 = 25 << 20

type WebhookStore interface {
	UpsertRepository(ctx context.Context, repository postgres.Repository) (postgres.Repository, error)
	PutEvent(ctx context.Context, event postgres.InboxEvent) (postgres.InboxEvent, bool, error)
}

type WebhookSecretResolver func(context.Context) (string, error)

type WebhookHandler struct {
	secret         string
	secretResolver WebhookSecretResolver
	store          WebhookStore
	wake           func()
}

func NewWebhookHandler(secret string, store WebhookStore, wake func()) *WebhookHandler {
	return newWebhookHandler(secret, nil, store, wake)
}

func NewResolvingWebhookHandler(resolver WebhookSecretResolver, store WebhookStore, wake func()) *WebhookHandler {
	return newWebhookHandler("", resolver, store, wake)
}

func newWebhookHandler(secret string, resolver WebhookSecretResolver, store WebhookStore, wake func()) *WebhookHandler {
	if wake == nil {
		wake = func() {}
	}
	return &WebhookHandler{secret: secret, secretResolver: resolver, store: store, wake: wake}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret, err := h.secretForRequest(r.Context())
	if err != nil {
		http.Error(w, "github webhook credential unavailable", http.StatusServiceUnavailable)
		return
	}
	if secret == "" {
		http.Error(w, "github webhook is not configured", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if !ValidSignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	event, err := NormalizeWebhook(r.Header.Get("X-GitHub-Event"), r.Header.Get("X-GitHub-Delivery"), body)
	if errors.Is(err, ErrUnsupportedEvent) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "invalid github event", http.StatusBadRequest)
		return
	}

	repository, err := h.store.UpsertRepository(r.Context(), postgres.Repository{
		ID:            event.RepositoryID,
		Provider:      "github",
		FullName:      event.RepositoryFullName,
		DefaultBranch: event.DefaultBranch,
		Enabled:       true,
	})
	if err != nil {
		http.Error(w, "persist repository failed", http.StatusInternalServerError)
		return
	}

	stored, inserted, err := h.store.PutEvent(r.Context(), postgres.InboxEvent{
		DedupeKey:    events.DedupeKey("github", repository.FullName, event.Kind, event.Subject, event.Revision),
		Provider:     "github",
		RepositoryID: repository.ID,
		Kind:         event.Kind,
		Subject:      event.Subject,
		Revision:     event.Revision,
		DeliveryID:   event.DeliveryID,
		Payload:      event.Payload,
	})
	if err != nil {
		http.Error(w, "persist event failed", http.StatusInternalServerError)
		return
	}
	if inserted {
		h.wake()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"event_id": stored.ID,
		"inserted": inserted,
	})
}

func (h *WebhookHandler) secretForRequest(ctx context.Context) (string, error) {
	if h.secretResolver == nil {
		return h.secret, nil
	}
	return h.secretResolver(ctx)
}
