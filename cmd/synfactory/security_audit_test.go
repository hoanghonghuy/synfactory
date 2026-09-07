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
	filter      securityaudit.Filter
	events      []securityaudit.Event
	err         error
	pruneCutoff time.Time
	pruneLimit  int
	pruneCount  int64
	pruneErr    error
	pruneCalls  int
}

func (f *fakeSecurityAuditOperations) ListSecurityAudit(_ context.Context, filter securityaudit.Filter) ([]securityaudit.Event, error) {
	f.filter = filter
	return f.events, f.err
}

func (f *fakeSecurityAuditOperations) PruneSecurityAuditBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	f.pruneCalls++
	f.pruneCutoff = cutoff
	f.pruneLimit = limit
	return f.pruneCount, f.pruneErr
}

func TestSecurityAuditRequiresSecurityPolicy(t *testing.T) {
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &fakeSecurityAuditOperations{}, &recordingSecurityAuditWriter{})

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/security/audit", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestSecurityAuditOperatorEndpointsRequireSecurityPolicy(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "export", method: http.MethodGet, path: "/api/security/audit/export"},
		{name: "retention", method: http.MethodPost, path: "/api/security/audit/retention", body: `{"cutoff":"2026-09-01T00:00:00Z","limit":100}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &fakeSecurityAuditOperations{}, &recordingSecurityAuditWriter{})
			req := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, req)
			if res.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestSecurityAuditSearchParsesBoundedFilters(t *testing.T) {
	reader := &fakeSecurityAuditOperations{events: []securityaudit.Event{{
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
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &fakeSecurityAuditOperations{}, &recordingSecurityAuditWriter{err: errors.New("down")})
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
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, &fakeSecurityAuditOperations{}, &recordingSecurityAuditWriter{})
	req := httptest.NewRequest(http.MethodGet, "/api/security/audit?since=2026-09-08T00:00:00Z&until=2026-09-07T00:00:00Z", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusBadRequest)
	}
}

func TestSecurityAuditExportIsBoundedAndAttributed(t *testing.T) {
	operations := &fakeSecurityAuditOperations{events: []securityaudit.Event{{
		ID:       "audit-1",
		Metadata: json.RawMessage(`{"provider":"github","attempt":1}`),
	}}}
	audit := &recordingSecurityAuditWriter{}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, operations, audit)

	req := httptest.NewRequest(http.MethodGet, "/api/security/audit/export?limit=50", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if operations.filter.Limit != 50 {
		t.Fatalf("limit = %d, want 50", operations.filter.Limit)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "security.audit.export" {
		t.Fatalf("audit events = %#v", audit.events)
	}
	var response securityAuditExportResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.SchemaVersion != "v1" || response.Count != 1 || len(response.Events) != 1 {
		t.Fatalf("response = %#v", response)
	}
}

func TestSecurityAuditExportRejectsSensitiveMetadata(t *testing.T) {
	operations := &fakeSecurityAuditOperations{events: []securityaudit.Event{{
		ID:       "audit-1",
		Metadata: json.RawMessage(`{"nested":{"github_token":"should-never-export"}}`),
	}}}
	audit := &recordingSecurityAuditWriter{}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, operations, audit)

	req := httptest.NewRequest(http.MethodGet, "/api/security/audit/export", nil)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnprocessableEntity)
	}
	if len(audit.events) != 0 {
		t.Fatalf("unexpected export audit events = %#v", audit.events)
	}
}

func TestSecurityAuditRetentionFailsClosedBeforeMutationWhenAuditUnavailable(t *testing.T) {
	operations := &fakeSecurityAuditOperations{pruneCount: 3}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, operations, &recordingSecurityAuditWriter{err: errors.New("down")})

	body := bytes.NewBufferString(`{"cutoff":"2026-09-01T00:00:00Z","limit":100}`)
	req := httptest.NewRequest(http.MethodPost, "/api/security/audit/retention", body)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
	if operations.pruneCalls != 0 {
		t.Fatalf("prune calls = %d, want 0", operations.pruneCalls)
	}
}

func TestSecurityAuditRetentionPrunesOnlyAfterAttribution(t *testing.T) {
	operations := &fakeSecurityAuditOperations{pruneCount: 3}
	audit := &recordingSecurityAuditWriter{}
	mux := http.NewServeMux()
	registerSecurityAudit(mux, authz.LegacyTokenAuthorizer{Token: "operator-secret"}, operations, audit)

	body := bytes.NewBufferString(`{"cutoff":"2026-09-01T00:00:00Z","limit":100}`)
	req := httptest.NewRequest(http.MethodPost, "/api/security/audit/retention", body)
	req.Header.Set("Authorization", "Bearer operator-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if operations.pruneCalls != 1 || operations.pruneLimit != 100 {
		t.Fatalf("prune calls=%d limit=%d", operations.pruneCalls, operations.pruneLimit)
	}
	if want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC); !operations.pruneCutoff.Equal(want) {
		t.Fatalf("cutoff = %s, want %s", operations.pruneCutoff, want)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "security.audit.retention" || audit.events[0].Outcome != "requested" {
		t.Fatalf("audit events = %#v", audit.events)
	}
	var response securityAuditRetentionResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Deleted != 3 {
		t.Fatalf("deleted = %d, want 3", response.Deleted)
	}
}
