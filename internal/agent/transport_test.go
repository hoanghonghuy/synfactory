package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testVerifier struct{}

func (testVerifier) VerifyAgentCredential(_ context.Context, w, c string) bool {
	return w == "worker-1" && c == "secret"
}

type testBackend struct {
	lease    LeaseDelivery
	ackCalls int
	ackFirst []bool
}

func (*testBackend) Heartbeat(context.Context, Session, Heartbeat) error { return nil }
func (b *testBackend) NextLease(context.Context, Session) (LeaseDelivery, bool, error) {
	return b.lease, b.lease.DeliveryID != "", nil
}
func (b *testBackend) LeaseAcked(_ context.Context, _ Session, _ LeaseAck, first bool) error {
	b.ackCalls++
	b.ackFirst = append(b.ackFirst, first)
	return nil
}
func (*testBackend) NextCancel(context.Context, Session) (CancelRequest, bool, error) {
	return CancelRequest{}, false, nil
}
func (*testBackend) CancelAcked(context.Context, Session, CancelAck, bool) error { return nil }
func (*testBackend) Status(context.Context, Session, Status) error               { return nil }
func request(t *testing.T, mux http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func connect(t *testing.T, mux http.Handler) Session {
	w := request(t, mux, "/agent/connect", ConnectRequest{WorkerID: "worker-1", CapabilityVersion: 1}, "secret")
	if w.Code != 200 {
		t.Fatalf("connect=%d %s", w.Code, w.Body.String())
	}
	var s Session
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("secret")) {
		t.Fatal("credential leaked")
	}
	return s
}
func TestTransportAuthReconnectReplayAndDuplicateAck(t *testing.T) {
	mgr := NewSessionManager(testVerifier{})
	identity := LeaseIdentity{WorkerID: "worker-1", JobID: "job-1", RunID: "run-1", Revision: "abc"}
	backend := &testBackend{lease: LeaseDelivery{DeliveryID: "delivery-1", Identity: identity}}
	mux := http.NewServeMux()
	Transport{Manager: mgr, Backend: backend}.Register(mux)
	if got := request(t, mux, "/agent/connect", ConnectRequest{WorkerID: "worker-1"}, "bad").Code; got != http.StatusUnauthorized {
		t.Fatalf("bad auth=%d", got)
	}
	s := connect(t, mux)
	poll := request(t, mux, "/agent/lease/poll", sessionSequence{Session: s, Sequence: 1}, "")
	if poll.Code != 200 {
		t.Fatalf("poll=%d %s", poll.Code, poll.Body.String())
	}
	ack := leaseAckEnvelope{Session: s, Sequence: 2, Ack: LeaseAck{DeliveryID: "delivery-1", Identity: identity}}
	if got := request(t, mux, "/agent/lease/ack", ack, "").Code; got != 204 {
		t.Fatalf("ack=%d", got)
	}
	ack.Sequence = 3
	if got := request(t, mux, "/agent/lease/ack", ack, "").Code; got != 204 {
		t.Fatalf("duplicate ack=%d", got)
	}
	if backend.ackCalls != 2 || len(backend.ackFirst) != 2 || !backend.ackFirst[0] || backend.ackFirst[1] {
		t.Fatalf("ack state=%v calls=%d", backend.ackFirst, backend.ackCalls)
	}
	replay := heartbeatEnvelope{Session: s, Heartbeat: Heartbeat{WorkerID: s.WorkerID, SessionID: s.ID, Sequence: 3}}
	if got := request(t, mux, "/agent/heartbeat", replay, "").Code; got != http.StatusConflict {
		t.Fatalf("replay=%d", got)
	}
	newer := connect(t, mux)
	stale := heartbeatEnvelope{Session: s, Heartbeat: Heartbeat{WorkerID: s.WorkerID, SessionID: s.ID, Sequence: 4}}
	if got := request(t, mux, "/agent/heartbeat", stale, "").Code; got != http.StatusConflict {
		t.Fatalf("stale=%d newer=%v", got, newer)
	}
}
func TestTransportBoundsAndTypedStatus(t *testing.T) {
	mgr := NewSessionManager(testVerifier{})
	mux := http.NewServeMux()
	Transport{Manager: mgr}.Register(mux)
	s := connect(t, mux)
	huge := bytes.Repeat([]byte("x"), maxTransportBody+1)
	r := httptest.NewRequest(http.MethodPost, "/agent/status", bytes.NewReader(huge))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("huge=%d", w.Code)
	}
	identity := LeaseIdentity{WorkerID: "worker-1", JobID: "j", RunID: "r", Revision: "rev"}
	env := statusEnvelope{Session: s, Sequence: 1, Status: Status{Identity: identity, State: "running", Evidence: EvidenceRef{SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	if got := request(t, mux, "/agent/status", env, "").Code; got != 204 {
		t.Fatalf("status=%d", got)
	}
}
