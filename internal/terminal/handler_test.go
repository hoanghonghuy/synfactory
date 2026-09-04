package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTerminalHandlerRequiresOperatorAuth(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{Enabled: true}, []Target{{ID: "local", Kind: TargetLocal}}, map[TargetKind]Backend{TargetLocal: backend})
	handler := &Handler{Manager: manager, Token: "secret"}
	mux := http.NewServeMux()
	handler.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/terminal/targets", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/terminal/targets", nil)
	req.Header.Set("Authorization", "Bearer secret")
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want %d", res.Code, http.StatusOK)
	}
	if bytes.Contains(res.Body.Bytes(), []byte("identity_file")) || bytes.Contains(res.Body.Bytes(), []byte("known_hosts_file")) {
		t.Fatal("terminal target response leaked secret-bearing target paths")
	}
}

func TestTerminalHandlerDisabledOpenIsForbidden(t *testing.T) {
	manager := NewManager(Config{Enabled: false}, nil, nil)
	handler := &Handler{Manager: manager, Token: "secret"}
	mux := http.NewServeMux()
	handler.Register(mux)

	body := bytes.NewBufferString(`{"target_id":"local","rows":24,"cols":80}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/terminal/sessions", body)
	req.Header.Set("Authorization", "Bearer secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusForbidden)
	}
	if len(manager.Active()) != 0 {
		t.Fatal("disabled terminal must not create a session")
	}
}

func TestTerminalHandlerOpenReturnsOneTimeTicket(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{Enabled: true}, []Target{{ID: "local", Kind: TargetLocal}}, map[TargetKind]Backend{TargetLocal: backend})
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	handler := &Handler{Manager: manager, Token: "secret", now: func() time.Time { return now }}
	mux := http.NewServeMux()
	handler.Register(mux)

	body := bytes.NewBufferString(`{"target_id":"local","rows":24,"cols":80}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/terminal/sessions", body)
	req.Header.Set("Authorization", "Bearer secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var response openResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Session.ID == "" || response.Ticket == "" {
		t.Fatalf("missing session/ticket: %+v", response)
	}
	if !handler.consumeTicket(response.Ticket, response.Session.ID) {
		t.Fatal("issued ticket was not accepted")
	}
	if handler.consumeTicket(response.Ticket, response.Session.ID) {
		t.Fatal("stream ticket must be one-time")
	}
}

func TestTerminalHandlerTicketExpires(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{Enabled: true}, []Target{{ID: "local", Kind: TargetLocal}}, map[TargetKind]Backend{TargetLocal: backend})
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	handler := &Handler{Manager: manager, Token: "secret", tickets: map[string]streamTicket{}, now: func() time.Time { return now }}
	session, err := manager.Open(context.Background(), "local", Size{})
	if err != nil {
		t.Fatal(err)
	}
	ticket := handler.issueTicket(session.ID)
	now = now.Add(streamTicketTTL + time.Second)
	if handler.consumeTicket(ticket, session.ID) {
		t.Fatal("expired ticket was accepted")
	}
}
