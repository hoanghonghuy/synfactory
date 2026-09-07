package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func TestCredentialDiagnosticsRequireSecurityPolicy(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "env")
	mux := http.NewServeMux()
	registerCredentialDiagnostics(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/security/credentials", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestCredentialDiagnosticsExposeMetadataWithoutSecretValues(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "env")
	t.Setenv("SYNFACTORY_GITHUB_TOKEN", "provider-secret")
	mux := http.NewServeMux()
	registerCredentialDiagnostics(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, config.Config{
		OperatorToken:           "legacy-operator-value",
		GitHubWebhookSecret:     "legacy-webhook-value",
		GitHubOAuthClientSecret: "legacy-oauth-value",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/security/credentials", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	body := res.Body.String()
	for _, secret := range []string{"provider-secret", "legacy-operator-value", "legacy-webhook-value", "legacy-oauth-value", "operator-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked secret %q: %s", secret, body)
		}
	}

	var response credentialDiagnosticsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]secrets.CredentialDiagnostic, len(response.Credentials))
	for _, diagnostic := range response.Credentials {
		byName[diagnostic.LogicalName] = diagnostic
	}
	if got := byName["github/token"]; !got.Available || got.Provider != "env" || got.State != secrets.DiagnosticHealthy {
		t.Fatalf("github/token diagnostic = %#v", got)
	}
	if got := byName["operator/token"]; !got.Available || got.Provider != "legacy" || got.State != secrets.DiagnosticHealthy {
		t.Fatalf("operator/token diagnostic = %#v", got)
	}
}
