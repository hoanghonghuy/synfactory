package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
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

func TestSignAppJWTProducesValidRS256JWT(t *testing.T) {
	key := mustRSAKey(t)
	now := time.Date(2026, 9, 4, 6, 0, 0, 0, time.UTC)

	token, err := signAppJWT(key, 12345, now)
	if err != nil {
		t.Fatalf("signAppJWT() error = %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT parts = %d, want 3", len(parts))
	}

	decode := base64.RawURLEncoding.DecodeString
	headerBytes, err := decode(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatalf("header is not valid JSON: %v; raw=%q", err, headerBytes)
	}
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		t.Fatalf("unexpected header: %#v", header)
	}

	payloadBytes, err := decode(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var payload struct {
		IssuedAt int64  `json:"iat"`
		Expires  int64  `json:"exp"`
		Issuer   string `json:"iss"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("decode payload JSON: %v", err)
	}
	if payload.Issuer != "12345" {
		t.Fatalf("issuer = %q, want 12345", payload.Issuer)
	}
	if payload.IssuedAt != now.Add(-time.Minute).Unix() {
		t.Fatalf("iat = %d", payload.IssuedAt)
	}
	if payload.Expires != now.Add(9*time.Minute).Unix() {
		t.Fatalf("exp = %d", payload.Expires)
	}

	sig, err := decode(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}

func TestAppTokenSourceCachesAndRefreshesBeforeExpiry(t *testing.T) {
	key := mustRSAKey(t)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	now := time.Date(2026, 9, 4, 6, 0, 0, 0, time.UTC)
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/777/access_tokens" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing bearer app JWT")
		}
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"token-%d","expires_at":%q}`, n, now.Add(time.Hour).Format(time.RFC3339))
	}))
	defer server.Close()

	source, err := NewAppTokenSource(server.URL, 12345, 777, pemBytes, server.Client())
	if err != nil {
		t.Fatalf("NewAppTokenSource() error = %v", err)
	}
	source.now = func() time.Time { return now }

	first, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token() error = %v", err)
	}
	second, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token() error = %v", err)
	}
	if first != "token-1" || second != first || calls.Load() != 1 {
		t.Fatalf("cache result first=%q second=%q calls=%d", first, second, calls.Load())
	}

	now = now.Add(56 * time.Minute)
	third, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("refresh Token() error = %v", err)
	}
	if third != "token-2" || calls.Load() != 2 {
		t.Fatalf("refresh result third=%q calls=%d", third, calls.Load())
	}
}

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}
