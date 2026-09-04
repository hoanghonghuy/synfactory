package authz

import (
	"errors"
	"net/http"
	"testing"
)

type authorizerFunc func(*http.Request, Permission, string) (Principal, error)

func (f authorizerFunc) Authorize(r *http.Request, permission Permission, repositoryID string) (Principal, error) {
	return f(r, permission, repositoryID)
}

func TestHybridAuthorizerFallsBackOnlyForAuthenticationFailures(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	legacyCalls := 0
	legacy := authorizerFunc(func(*http.Request, Permission, string) (Principal, error) {
		legacyCalls++
		return Principal{Subject: "legacy"}, nil
	})

	authorizer := HybridAuthorizer{
		Session: authorizerFunc(func(*http.Request, Permission, string) (Principal, error) {
			return Principal{}, ErrUnauthenticated
		}),
		Legacy: legacy,
	}
	principal, err := authorizer.Authorize(request, PermissionRead, "repo-a")
	if err != nil || principal.Subject != "legacy" || legacyCalls != 1 {
		t.Fatalf("expected legacy fallback: principal=%+v calls=%d err=%v", principal, legacyCalls, err)
	}

	legacyCalls = 0
	authorizer.Session = authorizerFunc(func(*http.Request, Permission, string) (Principal, error) {
		return Principal{}, ErrForbidden
	})
	_, err = authorizer.Authorize(request, PermissionRead, "repo-a")
	if !errors.Is(err, ErrForbidden) || legacyCalls != 0 {
		t.Fatalf("forbidden named session must not escalate via legacy fallback: calls=%d err=%v", legacyCalls, err)
	}
}
