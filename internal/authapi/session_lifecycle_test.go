package authapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type lifecycleStore struct {
	record      authz.SessionRecord
	revokedID   string
	revokedTime time.Time
}

func (s *lifecycleStore) UpsertAuthUser(context.Context, string, string, string, string) error { return nil }
func (s *lifecycleStore) ReplaceAuthGrants(context.Context, string, []authz.RoleGrant, []authz.PermissionGrant) error {
	return nil
}
func (s *lifecycleStore) RevokeAuthSession(_ context.Context, id string, at time.Time) error {
	s.revokedID = id
	s.revokedTime = at
	return nil
}
func (s *lifecycleStore) ResolveSession(context.Context, [sha256.Size]byte) (authz.SessionRecord, error) {
	return s.record, nil
}
func (s *lifecycleStore) RecordAuthorization(context.Context, string, authz.Principal, authz.Permission, string, bool, string, time.Time) error {
	return nil
}

func TestHandlerCurrentSessionReturnsNamedPrincipal(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := &lifecycleStore{record: authz.SessionRecord{
		ID:        "sess-1",
		ExpiresAt: now.Add(time.Hour),
		Principal: authz.Principal{Subject: "alice", DisplayName: "Alice", Roles: []authz.RoleGrant{{Role: authz.RoleObserver}}},
	}}
	h := Handler{Store: store, Sessions: authz.SessionAuthorizer{Store: store, Now: func() time.Time { return now }}}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set("Authorization", "Bearer named-session")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"subject":"alice"`)) || bytes.Contains(rec.Body.Bytes(), []byte("named-session")) {
		t.Fatalf("unexpected session response %s", rec.Body.String())
	}
}

func TestHandlerRevokeCurrentSessionRevokesResolvedSession(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := &lifecycleStore{record: authz.SessionRecord{
		ID:        "sess-1",
		ExpiresAt: now.Add(time.Hour),
		Principal: authz.Principal{Subject: "alice"},
	}}
	h := Handler{
		Store:    store,
		Sessions: authz.SessionAuthorizer{Store: store, Now: func() time.Time { return now }},
		Now:      func() time.Time { return now },
	}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/session", nil)
	req.Header.Set("Authorization", "Bearer named-session")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if store.revokedID != "sess-1" || !store.revokedTime.Equal(now) {
		t.Fatalf("revoked=%q at=%s", store.revokedID, store.revokedTime)
	}
}
