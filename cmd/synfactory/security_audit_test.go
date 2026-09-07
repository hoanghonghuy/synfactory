package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
)

type fakeSecurityAuditReader struct {
	filter securityaudit.Filter
	events []securityaudit.Event
	err    error
}

func (f *fakeSecurityAuditReader) ListSecurityAudit(_ context.Context, filter securityaudit.Filter) ([]securityaudit.Event, error) {
	f.filter = filter
	return f.events, f.err
}

func TestSecurityAuditRequiresSecurityPolicy(t *testing.T) {
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &fakeSecurityAuditReader{}, &recordingSecurityAuditWriter{})

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/security/audit", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestSecurityAuditSearchParsesBoundedFilters(t *testing.T) {
	reader := &fakeSecurityAuditReader{events: []securityaudit.Event{{
		ID:           "audit-1",
		OccurredAt:   time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC),
		ActorType:    "operator",
		ActorID:      "user-1",
		Action:       "credential.rotation.stage",
		ResourceType: "credential",
		ResourceID:   "github/token",
		Outcome:      "success",
	}}}
	audit := &recordingSecurityAuditWriter{}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, reader, audit)

	req := httptest.NewRequest(http.MethodGet, "/api/security/audit?actor_id=user-1&action=credential.rotation.stage&resource_type=credential&resource_id=github%2Ftoken&outcome=success&since=2026-09-07T09:00:00Z&until=2026-09-07T11:00:00Z&limit=25", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if reader.filter.ActorID != "user-1" || reader.filter.ResourceID != "github/token" || reader.filter.Limit != 25 {
		t.Fatalf("filter = %#v", reader.filter)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "security.audit.read" {
		t.Fatalf("audit events = %#v", audit.events)
	}
	var response securityAuditResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Events) != 1 || response.Events[0].ID != "audit-1" {
		t.Fatalf("response = %#v", response)
	}
}

func TestSecurityAuditSearchFailsClosedWhenAuditAppendFails(t *testing.T) {
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &fakeSecurityAuditReader{}, &recordingSecurityAuditWriter{err: errors.New("down")})
	req := httptest.NewRequest(http.MethodGet, "/api/security/audit", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
}

func TestSecurityAuditRejectsInvalidRange(t *testing.T) {
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &fakeSecurityAuditReader{}, &recordingSecurityAuditWriter{})
	req := httptest.NewRequest(http.MethodGet, "/api/security/audit?since=2026-09-08T00:00:00Z&until=2026-09-07T00:00:00Z", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusBadRequest)
	}
}
