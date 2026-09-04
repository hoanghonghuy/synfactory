package authz

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("permission denied")
)

type RequestAuthorizer interface {
	Authorize(r *http.Request, permission Permission, repositoryID string) (Principal, error)
}

type LegacyTokenAuthorizer struct {
	Token string
}

func (a LegacyTokenAuthorizer) Authorize(r *http.Request, permission Permission, repositoryID string) (Principal, error) {
	expected := strings.TrimSpace(a.Token)
	if expected == "" {
		return Principal{}, ErrUnauthenticated
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return Principal{}, ErrUnauthenticated
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return Principal{}, ErrUnauthenticated
	}
	principal := Principal{
		Subject: "legacy-operator-token",
		Roles:   []RoleGrant{{Role: RoleAdministrator}},
	}
	if !principal.Allowed(permission, repositoryID) {
		return Principal{}, ErrForbidden
	}
	return principal, nil
}
