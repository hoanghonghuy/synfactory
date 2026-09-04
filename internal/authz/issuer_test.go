package authz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"
)

type issuedRecord struct {
	id        string
	userID    string
	tokenHash [sha256.Size]byte
	expiresAt time.Time
	now       time.Time
}

type fakeSessionCreator struct {
	record issuedRecord
}

func (s *fakeSessionCreator) CreateAuthSession(_ context.Context, id, userID string, tokenHash [sha256.Size]byte, expiresAt, now time.Time) error {
	s.record = issuedRecord{id: id, userID: userID, tokenHash: tokenHash, expiresAt: expiresAt, now: now}
	return nil
}

func TestSessionIssuerReturnsRawTokenOnceButPersistsOnlyHash(t *testing.T) {
	store := &fakeSessionCreator{}
	now := time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)
	random := bytes.NewReader(bytes.Repeat([]byte{0x42}, 64))
	issuer := SessionIssuer{Store: store, Random: random, Now: func() time.Time { return now }, TTL: time.Hour}

	issued, err := issuer.Issue(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if issued.ID == "" || issued.Token == "" {
		t.Fatalf("issued session is incomplete: %+v", issued)
	}
	if store.record.id != issued.ID || store.record.userID != "alice" {
		t.Fatalf("unexpected persisted session metadata: %+v", store.record)
	}
	if store.record.tokenHash != HashSessionToken(issued.Token) {
		t.Fatal("persisted token hash does not match returned opaque token")
	}
	if string(store.record.tokenHash[:]) == issued.Token {
		t.Fatal("raw bearer token leaked into durable session record")
	}
	if !issued.ExpiresAt.Equal(now.Add(time.Hour)) || !store.record.expiresAt.Equal(issued.ExpiresAt) {
		t.Fatalf("unexpected expiry: issued=%s stored=%s", issued.ExpiresAt, store.record.expiresAt)
	}
}

func TestSessionIssuerBoundsTTL(t *testing.T) {
	issuer := SessionIssuer{
		Store:  &fakeSessionCreator{},
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)),
		TTL:    maxSessionTTL + time.Second,
	}
	if _, err := issuer.Issue(context.Background(), "alice"); err == nil {
		t.Fatal("session ttl above maximum must be rejected")
	}
}
