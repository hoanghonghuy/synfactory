package authz

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func TestLegacyTokenAuthorizerPreservesCompatibility(t *testing.T) {
	authorizer := LegacyTokenAuthorizer{Token: "legacy-secret"}
	request := httptest.NewRequest("GET", "/api/v1/overview", nil)
	request.Header.Set("Authorization", "Bearer legacy-secret")

	principal, err := authorizer.Authorize(request, PermissionRead, "repo-a")
	if err != nil {
		t.Fatal(err)
	}
	if principal.Subject != "legacy-operator-token" {
		t.Fatalf("subject = %q", principal.Subject)
	}
	if !principal.Allowed(PermissionTerminalAccess, "repo-a") {
		t.Fatal("legacy token must retain administrator compatibility during migration")
	}
}

func TestLegacyTokenAuthorizerRejectsMissingOrInvalidCredential(t *testing.T) {
	authorizer := LegacyTokenAuthorizer{Token: "legacy-secret"}
	for _, token := range []string{"", "wrong-secret"} {
		request := httptest.NewRequest("GET", "/api/v1/overview", nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		if _, err := authorizer.Authorize(request, PermissionRead, ""); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("token %q error = %v, want ErrUnauthenticated", token, err)
		}
	}
}

func TestLegacyTokenAuthorizerIsDisabledWithoutConfiguredToken(t *testing.T) {
	authorizer := LegacyTokenAuthorizer{}
	request := httptest.NewRequest("GET", "/api/v1/overview", nil)
	request.Header.Set("Authorization", "Bearer anything")
	if _, err := authorizer.Authorize(request, PermissionRead, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}
