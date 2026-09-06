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

// Authenticate resolves and validates a named-user bearer session without
// requiring a permission. It is used only for session lifecycle operations;
// every protected business API still calls Authorize with an explicit Go-owned
// permission and repository scope.
func (a SessionAuthorizer) Authenticate(r *http.Request) (SessionRecord, error) {
	if a.Store == nil {
		return SessionRecord{}, ErrUnauthenticated
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return SessionRecord{}, ErrUnauthenticated
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return SessionRecord{}, ErrUnauthenticated
	}

	session, err := a.Store.ResolveSession(r.Context(), HashSessionToken(token))
	if err != nil {
		return SessionRecord{}, ErrUnauthenticated
	}
	now := a.now()
	if session.RevokedAt != nil || !session.ExpiresAt.After(now) || strings.TrimSpace(session.Principal.Subject) == "" {
		return SessionRecord{}, ErrSessionInvalid
	}
	return session, nil
}

func (a SessionAuthorizer) Authorize(r *http.Request, permission Permission, repositoryID string) (Principal, error) {
	session, err := a.Authenticate(r)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) && a.Store != nil {
			// Resolve once more only to preserve invalid-session audit evidence when
			// possible. The authoritative decision remains fail-closed.
			const prefix = "Bearer "
			token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), prefix))
			if token != "" {
				if record, resolveErr := a.Store.ResolveSession(r.Context(), HashSessionToken(token)); resolveErr == nil {
					_ = a.Store.RecordAuthorization(r.Context(), record.ID, record.Principal, permission, repositoryID, false, "session_invalid", a.now())
				}
			}
		}
		return Principal{}, err
	}
	if !session.Principal.Allowed(permission, repositoryID) {
		_ = a.Store.RecordAuthorization(r.Context(), session.ID, session.Principal, permission, repositoryID, false, "permission_denied", a.now())
		return Principal{}, ErrForbidden
	}
	_ = a.Store.RecordAuthorization(r.Context(), session.ID, session.Principal, permission, repositoryID, true, "allowed", a.now())
	return session.Principal, nil
}

func (a SessionAuthorizer) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}
