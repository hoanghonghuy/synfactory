package github

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppRepositoryTokenSourceRoutesRepositoriesToInstallations(t *testing.T) {
	key := mustRSAKey(t)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	var discoveryCalls atomic.Int32
	var tokenCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/one/installation":
			discoveryCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 101})
		case "/repos/acme/two/installation":
			discoveryCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 202})
		case "/app/installations/101/access_tokens":
			tokenCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "token-one", "expires_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339)})
		case "/app/installations/202/access_tokens":
			tokenCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "token-two", "expires_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewAppRepositoryTokenSource(server.URL, 12345, pemBytes, server.Client())
	if err != nil {
		t.Fatalf("NewAppRepositoryTokenSource() error = %v", err)
	}
	one, err := source.TokenForRepository(context.Background(), "acme", "one")
	if err != nil {
		t.Fatalf("token one: %v", err)
	}
	two, err := source.TokenForRepository(context.Background(), "acme", "two")
	if err != nil {
		t.Fatalf("token two: %v", err)
	}
	oneAgain, err := source.TokenForRepository(context.Background(), "ACME", "ONE")
	if err != nil {
		t.Fatalf("token one again: %v", err)
	}
	if one != "token-one" || two != "token-two" || oneAgain != one {
		t.Fatalf("unexpected routing: one=%q two=%q oneAgain=%q", one, two, oneAgain)
	}
	if discoveryCalls.Load() != 2 || tokenCalls.Load() != 2 {
		t.Fatalf("calls discovery=%d token=%d, want 2/2", discoveryCalls.Load(), tokenCalls.Load())
	}
}

func TestClientRefreshesRepositoryInstallationTokenOnceAfter401(t *testing.T) {
	key := mustRSAKey(t)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	var discoveries atomic.Int32
	var mints atomic.Int32
	var branchCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/repo/installation":
			discoveries.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 777})
		case "/app/installations/777/access_tokens":
			n := mints.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      fmt.Sprintf("token-%d", n),
				"expires_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			})
		case "/repos/acme/repo/branches/develop":
			n := branchCalls.Add(1)
			want := "Bearer token-2"
			if n == 1 {
				want = "Bearer token-1"
			}
			if got := r.Header.Get("Authorization"); got != want {
				t.Errorf("authorization call %d = %q, want %q", n, got, want)
			}
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "develop", "commit": map[string]any{"sha": strings.Repeat("a", 40)}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewAppRepositoryTokenSource(server.URL, 12345, pemBytes, server.Client())
	if err != nil {
		t.Fatalf("NewAppRepositoryTokenSource() error = %v", err)
	}
	client := NewClientWithTokenSource(server.URL, source, server.Client())
	branch, err := client.GetBranch(context.Background(), "acme", "repo", "develop")
	if err != nil {
		t.Fatalf("GetBranch() error = %v", err)
	}
	if branch.Name != "develop" {
		t.Fatalf("branch name = %q", branch.Name)
	}
	if branchCalls.Load() != 2 || discoveries.Load() != 2 || mints.Load() != 2 {
		t.Fatalf("calls branch=%d discovery=%d mint=%d, want 2/2/2", branchCalls.Load(), discoveries.Load(), mints.Load())
	}
}

func TestAppRepositoryTokenSourceClassifiesMissingInstallation(t *testing.T) {
	key := mustRSAKey(t)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	source, err := NewAppRepositoryTokenSource(server.URL, 12345, pemBytes, server.Client())
	if err != nil {
		t.Fatalf("NewAppRepositoryTokenSource() error = %v", err)
	}
	_, err = source.TokenForRepository(context.Background(), "acme", "missing")
	if err == nil {
		t.Fatal("TokenForRepository() error = nil")
	}
	installationErr, permanent := IsPermanentInstallationError(err)
	if !permanent || installationErr.StatusCode != http.StatusNotFound || installationErr.Repository != "acme/missing" {
		t.Fatalf("unexpected installation error: %#v permanent=%t", installationErr, permanent)
	}
}
