package main

import (
	"bytes"
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

type fakeSecurityAuditOperations struct {
	filter     securityaudit.Filter
	events     []securityaudit.Event
	pruneCalls int
	cutoff     time.Time
	limit      int
	deleted    int64
}

func (f *fakeSecurityAuditOperations) ListSecurityAudit(_ context.Context, filter securityaudit.Filter) ([]securityaudit.Event, error) {
	f.filter = filter
	return f.events, nil
}

func (f *fakeSecurityAuditOperations) PruneSecurityAuditBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	f.pruneCalls++
	f.cutoff = cutoff
	f.limit = limit
	return f.deleted, nil
}

func TestSecurityAuditOperatorEndpointsRequireSecurityPolicy(t *testing.T) {
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/security/audit/export", ""},
		{http.MethodPost, "/api/security/audit/retention", `{"cutoff":"2026-09-01T00:00:00Z","limit":100}`},
	} {
		mux := http.NewServeMux()
		registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &fakeSecurityAuditOperations{}, &recordingSecurityAuditWriter{})
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body)))
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d, want %d", tc.method, tc.path, res.Code, http.StatusUnauthorized)
		}
	}
}

func TestSecurityAuditExportIsBoundedAttributedAndValueSafe(t *testing.T) {
	ops := &fakeSecurityAuditOperations{events: []securityaudit.Event{{ID: "audit-1", Metadata: json.RawMessage(`{"provider":"github","attempt":1}`)}}}
	audit := &recordingSecurityAuditWriter{}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, ops, audit)
	req := httptest.NewRequest(http.MethodGet, "/api/security/audit/export?limit=50", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK || ops.filter.Limit != 50 {
		t.Fatalf("status=%d limit=%d body=%s", res.Code, ops.filter.Limit, res.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Action != "security.audit.export" {
		t.Fatalf("audit=%#v", audit.events)
	}
}

func TestSecurityAuditExportRejectsSensitiveNestedMetadata(t *testing.T) {
	ops := &fakeSecurityAuditOperations{events: []securityaudit.Event{{ID: "audit-1", Metadata: json.RawMessage(`{"nested":{"github_token":"never-export"}}`)}}}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, ops, &recordingSecurityAuditWriter{})
	req := httptest.NewRequest(http.MethodGet, "/api/security/audit/export", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d, want %d", res.Code, http.StatusUnprocessableEntity)
	}
}

func TestSecurityAuditRetentionFailsClosedBeforeMutation(t *testing.T) {
	ops := &fakeSecurityAuditOperations{deleted: 3}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, ops, &recordingSecurityAuditWriter{err: errors.New("down")})
	req := httptest.NewRequest(http.MethodPost, "/api/security/audit/retention", bytes.NewBufferString(`{"cutoff":"2026-09-01T00:00:00Z","limit":100}`))
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || ops.pruneCalls != 0 {
		t.Fatalf("status=%d pruneCalls=%d", res.Code, ops.pruneCalls)
	}
}

func TestSecurityAuditRetentionPrunesAfterAttribution(t *testing.T) {
	ops := &fakeSecurityAuditOperations{deleted: 3}
	audit := &recordingSecurityAuditWriter{}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, ops, audit)
	req := httptest.NewRequest(http.MethodPost, "/api/security/audit/retention", bytes.NewBufferString(`{"cutoff":"2026-09-01T00:00:00Z","limit":100}`))
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK || ops.pruneCalls != 1 || ops.limit != 100 {
		t.Fatalf("status=%d pruneCalls=%d limit=%d body=%s", res.Code, ops.pruneCalls, ops.limit, res.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Action != "security.audit.retention" || audit.events[0].Outcome != "requested" {
		t.Fatalf("audit=%#v", audit.events)
	}
}
