package authz

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestLegacyTokenAuthorizerResolvesFutureRequestsDynamically(t *testing.T) {
	current := "before-rotation"
	authorizer := LegacyTokenAuthorizer{ResolveToken: func(context.Context) (string, error) {
		resolved := current
		if current == "before-rotation" {
			current = "after-rotation"
		}
		return resolved, nil
	}}

	first := httptest.NewRequest("GET", "/api/v1/overview", nil)
	first.Header.Set("Authorization", "Bearer before-rotation")
	if _, err := authorizer.Authorize(first, PermissionRead, ""); err != nil {
		t.Fatalf("first request should retain its resolved credential: %v", err)
	}

	second := httptest.NewRequest("GET", "/api/v1/overview", nil)
	second.Header.Set("Authorization", "Bearer after-rotation")
	if _, err := authorizer.Authorize(second, PermissionRead, ""); err != nil {
		t.Fatalf("future request should observe promoted credential: %v", err)
	}
}

func TestLegacyTokenAuthorizerFailsClosedWhenResolverUnavailable(t *testing.T) {
	authorizer := LegacyTokenAuthorizer{Token: "stale-token", ResolveToken: func(context.Context) (string, error) {
		return "", errors.New("provider unavailable")
	}}
	request := httptest.NewRequest("GET", "/api/v1/overview", nil)
	request.Header.Set("Authorization", "Bearer stale-token")
	if _, err := authorizer.Authorize(request, PermissionRead, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}
