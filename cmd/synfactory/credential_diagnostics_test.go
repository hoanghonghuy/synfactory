package main

import (
	"encoding/json"
	"errors"
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
	registerCredentialDiagnostics(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, config.Config{}, &recordingSecurityAuditWriter{})

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
	audit := &recordingSecurityAuditWriter{}
	registerCredentialDiagnostics(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, config.Config{
		OperatorToken:           "legacy-operator-value",
		GitHubWebhookSecret:     "legacy-webhook-value",
		GitHubOAuthClientSecret: "legacy-oauth-value",
	}, audit)

	req := httptest.NewRequest(http.MethodGet, "/api/security/credentials", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Action != "security.credentials.read" {
		t.Fatalf("audit events = %#v", audit.events)
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

func TestCredentialDiagnosticsFailClosedWhenAuditUnavailable(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "env")
	mux := http.NewServeMux()
	registerCredentialDiagnostics(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, config.Config{}, &recordingSecurityAuditWriter{err: errors.New("down")})
	req := httptest.NewRequest(http.MethodGet, "/api/security/credentials", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
}

func TestCredentialDiagnosticsReportPresentEmptySecretUnavailable(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "env")
	t.Setenv("SYNFACTORY_GITHUB_TOKEN", "")
	mux := http.NewServeMux()
	registerCredentialDiagnostics(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, config.Config{
		GitHubToken: "legacy-token-must-not-revive",
	}, &recordingSecurityAuditWriter{})

	req := httptest.NewRequest(http.MethodGet, "/api/security/credentials", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	if strings.Contains(res.Body.String(), "legacy-token-must-not-revive") {
		t.Fatalf("response leaked or revived legacy token: %s", res.Body.String())
	}
	var response credentialDiagnosticsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range response.Credentials {
		if diagnostic.LogicalName != "github/token" {
			continue
		}
		if diagnostic.Available || diagnostic.Provider != "env" || diagnostic.State != secrets.DiagnosticUnavailable {
			t.Fatalf("github/token diagnostic = %#v", diagnostic)
		}
		return
	}
	t.Fatal("github/token diagnostic not found")
}
