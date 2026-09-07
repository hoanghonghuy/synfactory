package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func TestCredentialRotationRequiresSecurityPolicy(t *testing.T) {
	provider := secrets.NewRotatingProvider(secrets.EnvProvider{Prefix: "SYNFACTORY_"})
	mux := http.NewServeMux()
	registerCredentialRotation(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &recordingSecurityAuditWriter{}, provider)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/security/credentials/rotation/stage", bytes.NewBufferString(`{"logical_name":"operator/token","source":"env-next"}`)))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestCredentialRotationStagePromoteUsesServerOwnedCandidate(t *testing.T) {
	t.Setenv("SYNFACTORY_OPERATOR_TOKEN", "active-operator")
	t.Setenv("SYNFACTORY_NEXT_OPERATOR_TOKEN", "next-operator")
	provider := secrets.NewRotatingProvider(secrets.EnvProvider{Prefix: "SYNFACTORY_"})
	audit := &recordingSecurityAuditWriter{}
	mux := http.NewServeMux()
	registerCredentialRotation(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, audit, provider)

	stage := httptest.NewRequest(http.MethodPost, "/api/security/credentials/rotation/stage", bytes.NewBufferString(`{"logical_name":"operator/token","source":"env-next"}`))
	stage.Header.Set("Authorization", "Bearer operator-secret")
	stageRes := httptest.NewRecorder()
	mux.ServeHTTP(stageRes, stage)
	if stageRes.Code != http.StatusOK || !provider.HasStaged("operator/token") {
		t.Fatalf("stage status=%d body=%s", stageRes.Code, stageRes.Body.String())
	}
	before, err := provider.Resolve(t.Context(), "operator/token")
	if err != nil || string(before.CloneBytes()) != "active-operator" {
		t.Fatalf("active before promote=%q err=%v", string(before.CloneBytes()), err)
	}

	promote := httptest.NewRequest(http.MethodPost, "/api/security/credentials/rotation/promote", bytes.NewBufferString(`{"logical_name":"operator/token"}`))
	promote.Header.Set("Authorization", "Bearer operator-secret")
	promoteRes := httptest.NewRecorder()
	mux.ServeHTTP(promoteRes, promote)
	if promoteRes.Code != http.StatusOK {
		t.Fatalf("promote status=%d body=%s", promoteRes.Code, promoteRes.Body.String())
	}
	after, err := provider.Resolve(t.Context(), "operator/token")
	if err != nil || string(after.CloneBytes()) != "next-operator" {
		t.Fatalf("active after promote=%q err=%v", string(after.CloneBytes()), err)
	}
	liveAuthorizer := authz.LegacyTokenAuthorizer{
		Token: "active-operator",
		ResolveToken: func(ctx context.Context) (string, error) {
			return secrets.ResolveOptionalString(ctx, provider, "operator/token", "active-operator")
		},
	}
	nextRequest := httptest.NewRequest(http.MethodGet, "/api/security/credentials", nil)
	nextRequest.Header.Set("Authorization", "Bearer next-operator")
	if _, err := liveAuthorizer.Authorize(nextRequest, authz.PermissionSecurityPolicy, ""); err != nil {
		t.Fatalf("future authorization did not observe promoted credential: %v", err)
	}
	oldRequest := httptest.NewRequest(http.MethodGet, "/api/security/credentials", nil)
	oldRequest.Header.Set("Authorization", "Bearer active-operator")
	if _, err := liveAuthorizer.Authorize(oldRequest, authz.PermissionSecurityPolicy, ""); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Fatalf("old credential remained authoritative after promotion: %v", err)
	}
	if len(audit.events) != 2 || audit.events[0].Action != "security.credentials.rotation.stage" || audit.events[1].Action != "security.credentials.rotation.promote" {
		t.Fatalf("audit=%#v", audit.events)
	}
	for _, event := range audit.events {
		if len(event.Metadata) != 0 || event.ResourceID != "operator/token" || event.Outcome != "requested" {
			t.Fatalf("unsafe or incomplete audit event=%#v", event)
		}
	}
}

func TestCredentialRotationAuditFailurePreventsMutation(t *testing.T) {
	t.Setenv("SYNFACTORY_OPERATOR_TOKEN", "active-operator")
	t.Setenv("SYNFACTORY_NEXT_OPERATOR_TOKEN", "next-operator")
	provider := secrets.NewRotatingProvider(secrets.EnvProvider{Prefix: "SYNFACTORY_"})
	mux := http.NewServeMux()
	registerCredentialRotation(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &recordingSecurityAuditWriter{err: errors.New("down")}, provider)

	req := httptest.NewRequest(http.MethodPost, "/api/security/credentials/rotation/stage", bytes.NewBufferString(`{"logical_name":"operator/token","source":"env-next"}`))
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || provider.HasStaged("operator/token") {
		t.Fatalf("status=%d staged=%v", res.Code, provider.HasStaged("operator/token"))
	}
}

func TestCredentialRotationRejectsUnsupportedSourceAndCredential(t *testing.T) {
	provider := secrets.NewRotatingProvider(secrets.EnvProvider{Prefix: "SYNFACTORY_"})
	mux := http.NewServeMux()
	registerCredentialRotation(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &recordingSecurityAuditWriter{}, provider)
	for _, body := range []string{
		`{"logical_name":"operator/token","source":"inline"}`,
		`{"logical_name":"github/token","source":"env-next"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/security/credentials/rotation/stage", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer operator-secret")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest && res.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body=%s status=%d", body, res.Code)
		}
	}
}

func TestCredentialRotationDiscardKeepsActiveCredential(t *testing.T) {
	t.Setenv("SYNFACTORY_OPERATOR_TOKEN", "active-operator")
	t.Setenv("SYNFACTORY_NEXT_OPERATOR_TOKEN", "next-operator")
	provider := secrets.NewRotatingProvider(secrets.EnvProvider{Prefix: "SYNFACTORY_"})
	audit := &recordingSecurityAuditWriter{}
	mux := http.NewServeMux()
	registerCredentialRotation(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, audit, provider)

	stage := httptest.NewRequest(http.MethodPost, "/api/security/credentials/rotation/stage", bytes.NewBufferString(`{"logical_name":"operator/token","source":"env-next"}`))
	stage.Header.Set("Authorization", "Bearer operator-secret")
	mux.ServeHTTP(httptest.NewRecorder(), stage)
	discard := httptest.NewRequest(http.MethodPost, "/api/security/credentials/rotation/discard", bytes.NewBufferString(`{"logical_name":"operator/token"}`))
	discard.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, discard)
	if res.Code != http.StatusOK || provider.HasStaged("operator/token") {
		t.Fatalf("status=%d staged=%v", res.Code, provider.HasStaged("operator/token"))
	}
	value, err := provider.Resolve(t.Context(), "operator/token")
	if err != nil || string(value.CloneBytes()) != "active-operator" {
		t.Fatalf("active=%q err=%v", string(value.CloneBytes()), err)
	}
}
