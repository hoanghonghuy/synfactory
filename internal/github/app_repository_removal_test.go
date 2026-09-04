package github

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppRepositoryTokenSourceClassifiesRemovedInstallationDuringMint(t *testing.T) {
	key := mustRSAKey(t)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/removed/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 909})
		case "/app/installations/909/access_tokens":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewAppRepositoryTokenSource(server.URL, 12345, pemBytes, server.Client())
	if err != nil {
		t.Fatalf("NewAppRepositoryTokenSource() error = %v", err)
	}
	_, err = source.TokenForRepository(context.Background(), "acme", "removed")
	if err == nil {
		t.Fatal("TokenForRepository() error = nil")
	}
	installationErr, permanent := IsPermanentInstallationError(err)
	if !permanent {
		t.Fatalf("removed installation must be permanent: %v", err)
	}
	if installationErr.Repository != "acme/removed" || installationErr.StatusCode != http.StatusNotFound || installationErr.Message != "Not Found" {
		t.Fatalf("unexpected installation error: %#v", installationErr)
	}
}
