package authz

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

type sessionAudit struct {
	allowed bool
	reason  string
}

type fakeSessionStore struct {
	sessions map[[sha256.Size]byte]SessionRecord
	audits   []sessionAudit
}

func (s *fakeSessionStore) ResolveSession(_ context.Context, tokenHash [sha256.Size]byte) (SessionRecord, error) {
	session, ok := s.sessions[tokenHash]
	if !ok {
		return SessionRecord{}, errors.New("not found")
	}
	return session, nil
}

func (s *fakeSessionStore) RecordAuthorization(_ context.Context, _ string, _ Principal, _ Permission, _ string, allowed bool, reason string, _ time.Time) error {
	s.audits = append(s.audits, sessionAudit{allowed: allowed, reason: reason})
	return nil
}

func TestSessionAuthorizerHonorsRepositoryScopeAndAuditsDecision(t *testing.T) {
	now := time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)
	token := "opaque-session-secret"
	store := &fakeSessionStore{sessions: map[[sha256.Size]byte]SessionRecord{
		HashSessionToken(token): {
			ID: "session-1",
			Principal: Principal{
				Subject: "alice",
				Roles:   []RoleGrant{{Role: RoleOperator, RepositoryID: "repo-a"}},
			},
			ExpiresAt: now.Add(time.Hour),
		},
	}}
	authorizer := SessionAuthorizer{Store: store, Now: func() time.Time { return now }}

	allowed := httptest.NewRequest("POST", "/api/v1/repositories/repo-a", nil)
	allowed.Header.Set("Authorization", "Bearer "+token)
	if _, err := authorizer.Authorize(allowed, PermissionRepositoryMutate, "repo-a"); err != nil {
		t.Fatalf("allowed request: %v", err)
	}

	denied := httptest.NewRequest("POST", "/api/v1/repositories/repo-b", nil)
	denied.Header.Set("Authorization", "Bearer "+token)
	if _, err := authorizer.Authorize(denied, PermissionRepositoryMutate, "repo-b"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("out-of-scope error = %v, want ErrForbidden", err)
	}
	if len(store.audits) != 2 || !store.audits[0].allowed || store.audits[1].reason != "permission_denied" {
		t.Fatalf("unexpected audit decisions: %+v", store.audits)
	}
}

func TestSessionAuthorizerRejectsExpiredAndRevokedSessionsDeterministically(t *testing.T) {
	now := time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-time.Minute)
	for _, tc := range []struct {
		name    string
		session SessionRecord
	}{
		{
			name:    "expired",
			session: SessionRecord{ID: "expired", Principal: Principal{Subject: "alice", Roles: []RoleGrant{{Role: RoleObserver}}}, ExpiresAt: now},
		},
		{
			name:    "revoked",
			session: SessionRecord{ID: "revoked", Principal: Principal{Subject: "alice", Roles: []RoleGrant{{Role: RoleObserver}}}, ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := "token-" + tc.name
			store := &fakeSessionStore{sessions: map[[sha256.Size]byte]SessionRecord{HashSessionToken(token): tc.session}}
			authorizer := SessionAuthorizer{Store: store, Now: func() time.Time { return now }}
			request := httptest.NewRequest("GET", "/api/v1/overview", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			if _, err := authorizer.Authorize(request, PermissionRead, ""); !errors.Is(err, ErrSessionInvalid) {
				t.Fatalf("error = %v, want ErrSessionInvalid", err)
			}
			if len(store.audits) != 1 || store.audits[0].reason != "session_invalid" {
				t.Fatalf("invalid session audit missing: %+v", store.audits)
			}
		})
	}
}

func TestSessionTokenHashDoesNotPersistRawBearerValue(t *testing.T) {
	token := "never-store-this-raw-token"
	hash := HashSessionToken(token)
	if string(hash[:]) == token {
		t.Fatal("session hash unexpectedly equals raw token")
	}
	if hash != sha256.Sum256([]byte(token)) {
		t.Fatal("session hash is not deterministic SHA-256")
	}
}
