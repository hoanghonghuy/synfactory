package authz

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"time"
)

const (
	defaultSessionTTL = 12 * time.Hour
	maxSessionTTL     = 7 * 24 * time.Hour
)

type SessionCreator interface {
	CreateAuthSession(ctx context.Context, id, userID string, tokenHash [sha256.Size]byte, expiresAt, now time.Time) error
}

type SessionIssuer struct {
	Store  SessionCreator
	Random io.Reader
	Now    func() time.Time
	TTL    time.Duration
}

type IssuedSession struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (i SessionIssuer) Issue(ctx context.Context, userID string) (IssuedSession, error) {
	if i.Store == nil {
		return IssuedSession{}, fmt.Errorf("session creator is required")
	}
	now := time.Now().UTC()
	if i.Now != nil {
		now = i.Now().UTC()
	}
	ttl := i.TTL
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	if ttl > maxSessionTTL {
		return IssuedSession{}, fmt.Errorf("session ttl exceeds maximum %s", maxSessionTTL)
	}
	random := i.Random
	if random == nil {
		random = rand.Reader
	}

	idBytes := make([]byte, 18)
	if _, err := io.ReadFull(random, idBytes); err != nil {
		return IssuedSession{}, fmt.Errorf("generate session id: %w", err)
	}
	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(random, tokenBytes); err != nil {
		return IssuedSession{}, fmt.Errorf("generate session token: %w", err)
	}
	id := "sess_" + base64.RawURLEncoding.EncodeToString(idBytes)
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expiresAt := now.Add(ttl)
	if err := i.Store.CreateAuthSession(ctx, id, userID, HashSessionToken(token), expiresAt, now); err != nil {
		return IssuedSession{}, err
	}
	return IssuedSession{ID: id, Token: token, ExpiresAt: expiresAt}, nil
}
