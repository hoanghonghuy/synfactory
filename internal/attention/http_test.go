package attention

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type httpStore struct {
	item Item
}

func (s *httpStore) AttentionByID(context.Context, string) (Item, error) { return s.item, nil }
func (s *httpStore) UpsertAttention(_ context.Context, item Item) (Item, error) {
	s.item = item
	return item, nil
}

type resolvedRevalidator bool

func (r resolvedRevalidator) UnderlyingResolved(context.Context, Item) (bool, error) { return bool(r), nil }

func TestHTTPHandlerRequiresOperatorAuthorization(t *testing.T) {
	h := HTTPHandler{Service: Service{Store: &httpStore{}}, Token: "secret"}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attention/a1/acknowledge", bytes.NewBufferString(`{"actor":"huy"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHTTPHandlerAcknowledgeAndSnooze(t *testing.T) {
	now := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	store := &httpStore{item: Item{ID: "a1", State: StateOpen, CreatedAt: now, UpdatedAt: now}}
	h := HTTPHandler{Service: Service{Store: store, Now: func() time.Time { return now }}, Token: "secret"}
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attention/a1/acknowledge", bytes.NewBufferString(`{"actor":"operator-1"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store.item.State != StateAcknowledged || store.item.AssignedTo != "operator-1" {
		t.Fatalf("ack result: status=%d state=%s actor=%s", rec.Code, store.item.State, store.item.AssignedTo)
	}

	until := now.Add(time.Hour).Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/attention/a1/snooze", bytes.NewBufferString(`{"actor":"operator-1","until":"`+until+`"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store.item.State != StateSnoozed || store.item.SnoozedUntil == nil || !store.item.SnoozedUntil.Equal(now.Add(time.Hour)) {
		t.Fatalf("snooze result: status=%d item=%+v", rec.Code, store.item)
	}
}

func TestHTTPHandlerResolveFailsClosedUntilUnderlyingBlockerClears(t *testing.T) {
	now := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	store := &httpStore{item: Item{ID: "a1", State: StateOpen, CreatedAt: now, UpdatedAt: now}}
	h := HTTPHandler{Service: Service{Store: store, Revalidator: resolvedRevalidator(false), Now: func() time.Time { return now }}, Token: "secret"}
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attention/a1/resolve", bytes.NewBufferString(`{"actor":"operator-1"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if store.item.State == StateResolved {
		t.Fatal("attention item resolved while underlying blocker remained active")
	}
}
