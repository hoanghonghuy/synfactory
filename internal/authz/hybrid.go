package authz

import (
	"errors"
	"net/http"
)

// HybridAuthorizer preserves the explicit legacy operator-token compatibility
// path while allowing named-user sessions to become the primary authority.
// Session authorization is attempted first. Only authentication/session failures
// fall back to the legacy token; an authenticated-but-forbidden session is never
// upgraded through fallback.
type HybridAuthorizer struct {
	Session RequestAuthorizer
	Legacy  RequestAuthorizer
}

func (a HybridAuthorizer) Authorize(r *http.Request, permission Permission, repositoryID string) (Principal, error) {
	if a.Session != nil {
		principal, err := a.Session.Authorize(r, permission, repositoryID)
		if err == nil {
			return principal, nil
		}
		if errors.Is(err, ErrForbidden) {
			return Principal{}, err
		}
	}
	if a.Legacy != nil {
		return a.Legacy.Authorize(r, permission, repositoryID)
	}
	return Principal{}, ErrUnauthenticated
}
