package controlcenter

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type recordingAuthorizer struct {
	permission   authz.Permission
	repositoryID string
	err          error
}

func (a *recordingAuthorizer) Authorize(_ *http.Request, permission authz.Permission, repositoryID string) (authz.Principal, error) {
	a.permission = permission
	a.repositoryID = repositoryID
	return authz.Principal{Subject: "user-1"}, a.err
}

func TestAuthorizedHandlerPassesRepositoryScope(t *testing.T) {
	authorizer := &recordingAuthorizer{}
	h := AuthorizedHandler{Authorizer: authorizer}
	nextCalled := false
	handler := h.authorizeRequest(authz.PermissionRead, repositoryFromQuery, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows?repository_id=repo-1", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if !nextCalled || authorizer.permission != authz.PermissionRead || authorizer.repositoryID != "repo-1" {
		t.Fatalf("authorization scope not enforced: called=%v permission=%q repository=%q", nextCalled, authorizer.permission, authorizer.repositoryID)
	}
}

func TestAuthorizedHandlerDoesNotFallThroughForbiddenSession(t *testing.T) {
	authorizer := &recordingAuthorizer{err: authz.ErrForbidden}
	h := AuthorizedHandler{Authorizer: authorizer}
	nextCalled := false
	handler := h.authorizeRequest(authz.PermissionRead, repositoryFromQuery, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows?repository_id=repo-denied", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden || nextCalled {
		t.Fatalf("forbidden request must stop at authorization boundary: status=%d next=%v", res.Code, nextCalled)
	}
}

func TestAuthorizedHandlerClassifiesAuthenticationFailure(t *testing.T) {
	authorizer := &recordingAuthorizer{err: authz.ErrUnauthenticated}
	h := AuthorizedHandler{Authorizer: authorizer}
	handler := h.authorizeRequest(authz.PermissionRead, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unauthenticated request reached handler")
	}))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d", res.Code, http.StatusUnauthorized)
	}

	authorizer.err = errors.New("backend unavailable")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want %d", res.Code, http.StatusInternalServerError)
	}
}
