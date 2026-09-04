package authapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type fakeAuthorizer struct {
	err        error
	permission authz.Permission
}

func (a *fakeAuthorizer) Authorize(_ *http.Request, permission authz.Permission, _ string) (authz.Principal, error) {
	a.permission = permission
	if a.err != nil {
		return authz.Principal{}, a.err
	}
	return authz.Principal{Subject: "admin", Roles: []authz.RoleGrant{{Role: authz.RoleAdministrator}}}, nil
}

type fakeStore struct {
	upsertedUser string
	roles        []authz.RoleGrant
	permissions  []authz.PermissionGrant
	revokedID    string
	created      bool
}

func (s *fakeStore) UpsertAuthUser(_ context.Context, id, _, _, _ string) error {
	s.upsertedUser = id
	return nil
}

func (s *fakeStore) ReplaceAuthGrants(_ context.Context, userID string, roles []authz.RoleGrant, permissions []authz.PermissionGrant) error {
	s.upsertedUser = userID
	s.roles = roles
	s.permissions = permissions
	return nil
}

func (s *fakeStore) RevokeAuthSession(_ context.Context, id string, _ time.Time) error {
	s.revokedID = id
	return nil
}

func (s *fakeStore) CreateAuthSession(_ context.Context, _, _ string, _ [sha256.Size]byte, _, _ time.Time) error {
	s.created = true
	return nil
}

func TestIssueSessionRequiresSecurityPolicyPermission(t *testing.T) {
	authorizer := &fakeAuthorizer{err: authz.ErrForbidden}
	store := &fakeStore{}
	handler := Handler{Store: store, Authorizer: authorizer, Issuer: authz.SessionIssuer{Store: store}}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/users/alice/sessions", strings.NewReader(`{"provider":"github","provider_subject":"123"}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if authorizer.permission != authz.PermissionSecurityPolicy {
		t.Fatalf("permission = %q", authorizer.permission)
	}
	if store.upsertedUser != "" || store.created {
		t.Fatal("forbidden request mutated auth state")
	}
}

func TestIssueSessionCreatesGovernedOpaqueSession(t *testing.T) {
	authorizer := &fakeAuthorizer{}
	store := &fakeStore{}
	now := time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)
	handler := Handler{
		Store:      store,
		Authorizer: authorizer,
		Issuer: authz.SessionIssuer{
			Store:  store,
			Random: strings.NewReader(strings.Repeat("x", 64)),
			Now:    func() time.Time { return now },
			TTL:    time.Hour,
		},
	}
	mux := http.NewServeMux()
	handler.Register(mux)

	body := `{"provider":"github","provider_subject":"123","display_name":"Alice","roles":[{"role":"observer"}],"permissions":[{"permission":"terminal_access","repository_id":"repo-a"}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/users/alice/sessions", strings.NewReader(body))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if store.upsertedUser != "alice" || !store.created {
		t.Fatalf("auth state was not persisted: %+v", store)
	}
	if len(store.roles) != 1 || store.roles[0].Role != authz.RoleObserver {
		t.Fatalf("roles = %+v", store.roles)
	}
	if len(store.permissions) != 1 || store.permissions[0].Permission != authz.PermissionTerminalAccess {
		t.Fatalf("permissions = %+v", store.permissions)
	}
	if strings.Contains(response.Body.String(), "provider_subject") {
		t.Fatal("identity provider subject leaked into session response")
	}
}

func TestRevokeSessionRequiresAuthorization(t *testing.T) {
	authorizer := &fakeAuthorizer{err: authz.ErrUnauthenticated}
	store := &fakeStore{}
	handler := Handler{Store: store, Authorizer: authorizer}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/sess-1", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if store.revokedID != "" {
		t.Fatal("unauthenticated revoke changed session state")
	}
}

func TestRevokeSessionUsesSecurityPolicyGate(t *testing.T) {
	authorizer := &fakeAuthorizer{}
	store := &fakeStore{}
	handler := Handler{Store: store, Authorizer: authorizer, Now: func() time.Time { return time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC) }}
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/sess-1", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if store.revokedID != "sess-1" || authorizer.permission != authz.PermissionSecurityPolicy {
		t.Fatalf("unexpected revoke state: id=%q permission=%q", store.revokedID, authorizer.permission)
	}
}

func TestIssueSessionReturnsUnauthorizedForMissingAuthorizerCredential(t *testing.T) {
	authorizer := &fakeAuthorizer{err: errors.New("invalid credential")}
	handler := Handler{Store: &fakeStore{}, Authorizer: authorizer}
	mux := http.NewServeMux()
	handler.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/users/alice/sessions", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}
