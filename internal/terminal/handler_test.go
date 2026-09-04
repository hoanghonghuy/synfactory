package terminal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", res.Header().Get("Cache-Control"))
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

func TestTerminalHandlerOpenReturnsHighEntropyOneTimeTicket(t *testing.T) {
	backend := &fakeBackend{}
	manager := NewManager(Config{Enabled: true, MaxSessions: 2, MaxSessionsPerTarget: 2}, []Target{{ID: "local", Kind: TargetLocal}}, map[TargetKind]Backend{TargetLocal: backend})
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	handler := &Handler{Manager: manager, Token: "secret", now: func() time.Time { return now }}
	mux := http.NewServeMux()
	handler.Register(mux)

	open := func() openResponse {
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
		return response
	}
	first := open()
	second := open()
	if first.Session.ID == "" || first.Ticket == "" || len(first.Ticket) < 40 {
		t.Fatalf("missing/weak session ticket: %+v", first)
	}
	if first.Ticket == second.Ticket {
		t.Fatal("stream tickets must be independently random")
	}
	if !handler.consumeTicket(first.Ticket, first.Session.ID) {
		t.Fatal("issued ticket was not accepted")
	}
	if handler.consumeTicket(first.Ticket, first.Session.ID) {
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
	ticket, err := handler.issueTicket(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(streamTicketTTL + time.Second)
	if handler.consumeTicket(ticket, session.ID) {
		t.Fatal("expired ticket was accepted")
	}
}

func TestStreamTicketUsesWebSocketSubprotocolNotURL(t *testing.T) {
	protocol, ticket := streamProtocol("other, synfactory-terminal.ticket-value")
	if protocol != "synfactory-terminal.ticket-value" || ticket != "ticket-value" {
		t.Fatalf("protocol=%q ticket=%q", protocol, ticket)
	}
	if protocol, ticket := streamProtocol(""); protocol != "" || ticket != "" {
		t.Fatalf("empty protocol parsed unexpectedly: %q %q", protocol, ticket)
	}
}

func TestWebSocketDisconnectClosesOwnedLocalPTY(t *testing.T) {
	manager := NewManager(Config{Enabled: true, MaxSessions: 1, MaxSessionsPerTarget: 1}, []Target{{
		ID: "local", Kind: TargetLocal, WorkDir: t.TempDir(), Shell: "/bin/sh",
	}}, map[TargetKind]Backend{TargetLocal: LocalBackend{}})
	mux := http.NewServeMux()
	(&Handler{Manager: manager, Token: "operator-secret"}).Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	defer func() { _ = manager.Shutdown() }()

	body := bytes.NewBufferString(`{"target_id":"local","rows":24,"cols":80}`)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/terminal/sessions", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer operator-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", response.StatusCode)
	}
	var opened openResponse
	if err := json.NewDecoder(response.Body).Decode(&opened); err != nil {
		t.Fatal(err)
	}

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	protocol := streamProtocolPrefix + opened.Ticket
	_, err = fmt.Fprintf(conn,
		"GET /api/v1/terminal/sessions/%s/stream HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Protocol: %s\r\n\r\n",
		opened.Session.ID, parsed.Host, protocol,
	)
	if err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if !strings.Contains(status, "101 Switching Protocols") {
		_ = conn.Close()
		t.Fatalf("unexpected websocket status: %s", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(manager.Active()) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("terminal session remained active after websocket disconnect: %+v", manager.Active())
}
