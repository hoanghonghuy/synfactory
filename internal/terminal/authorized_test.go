package terminal

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/authz"
)

type terminalAuthorizerFunc func(*http.Request, authz.Permission, string) (authz.Principal, error)

func (f terminalAuthorizerFunc) Authorize(r *http.Request, permission authz.Permission, repositoryID string) (authz.Principal, error) {
	return f(r, permission, repositoryID)
}

func TestAuthorizedHandlerRejectsUnauthenticatedTerminalAccess(t *testing.T) {
	mux := http.NewServeMux()
	AuthorizedHandler{
		Handler: &Handler{},
		Authorizer: terminalAuthorizerFunc(func(_ *http.Request, permission authz.Permission, repositoryID string) (authz.Principal, error) {
			if permission != authz.PermissionTerminalAccess {
				t.Fatalf("permission = %q, want %q", permission, authz.PermissionTerminalAccess)
			}
			if repositoryID != "" {
				t.Fatalf("repositoryID = %q, want global terminal scope", repositoryID)
			}
			return authz.Principal{}, authz.ErrUnauthenticated
		}),
	}.Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/terminal/targets", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthorizedHandlerRejectsTerminalPermissionDenial(t *testing.T) {
	mux := http.NewServeMux()
	AuthorizedHandler{
		Handler: &Handler{},
		Authorizer: terminalAuthorizerFunc(func(_ *http.Request, _ authz.Permission, _ string) (authz.Principal, error) {
			return authz.Principal{}, authz.ErrForbidden
		}),
	}.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/terminal/sessions", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestAuthorizedHandlerAllowsAuthorizedRequestToReachTerminalHandler(t *testing.T) {
	mux := http.NewServeMux()
	AuthorizedHandler{
		Handler: &Handler{},
		Authorizer: terminalAuthorizerFunc(func(_ *http.Request, _ authz.Permission, _ string) (authz.Principal, error) {
			return authz.Principal{Subject: "operator@example.test"}, nil
		}),
	}.Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/terminal/targets", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestAuthorizedHandlerFailsClosedOnAuthorizationError(t *testing.T) {
	mux := http.NewServeMux()
	AuthorizedHandler{
		Handler: &Handler{},
		Authorizer: terminalAuthorizerFunc(func(_ *http.Request, _ authz.Permission, _ string) (authz.Principal, error) {
			return authz.Principal{}, errors.New("authorization store unavailable")
		}),
	}.Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/terminal/sessions", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
