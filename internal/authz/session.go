package authz

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"
)

var ErrSessionInvalid = errors.New("session invalid")

type SessionRecord struct {
	ID        string
	Principal Principal
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type SessionStore interface {
	ResolveSession(ctx context.Context, tokenHash [sha256.Size]byte) (SessionRecord, error)
	RecordAuthorization(ctx context.Context, sessionID string, principal Principal, permission Permission, repositoryID string, allowed bool, reason string, at time.Time) error
}

type SessionAuthorizer struct {
	Store SessionStore
	Now   func() time.Time
}

func HashSessionToken(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

func (a SessionAuthorizer) Authorize(r *http.Request, permission Permission, repositoryID string) (Principal, error) {
	if a.Store == nil {
		return Principal{}, ErrUnauthenticated
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return Principal{}, ErrUnauthenticated
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}

	session, err := a.Store.ResolveSession(r.Context(), HashSessionToken(token))
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	now := time.Now().UTC()
	if a.Now != nil {
		now = a.Now().UTC()
	}
	if session.RevokedAt != nil || !session.ExpiresAt.After(now) || strings.TrimSpace(session.Principal.Subject) == "" {
		_ = a.Store.RecordAuthorization(r.Context(), session.ID, session.Principal, permission, repositoryID, false, "session_invalid", now)
		return Principal{}, ErrSessionInvalid
	}
	if !session.Principal.Allowed(permission, repositoryID) {
		_ = a.Store.RecordAuthorization(r.Context(), session.ID, session.Principal, permission, repositoryID, false, "permission_denied", now)
		return Principal{}, ErrForbidden
	}
	_ = a.Store.RecordAuthorization(r.Context(), session.ID, session.Principal, permission, repositoryID, true, "allowed", now)
	return session.Principal, nil
}
