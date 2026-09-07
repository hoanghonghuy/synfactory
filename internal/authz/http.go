package authz

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
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

type TokenResolver func(context.Context) (string, error)

type LegacyTokenAuthorizer struct {
	Token        string
	ResolveToken TokenResolver
}

func (a LegacyTokenAuthorizer) Authorize(r *http.Request, permission Permission, repositoryID string) (Principal, error) {
	expected, err := a.expectedToken(r.Context())
	if err != nil {
		return Principal{}, err
	}
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

func (a LegacyTokenAuthorizer) expectedToken(ctx context.Context) (string, error) {
	if a.ResolveToken == nil {
		return strings.TrimSpace(a.Token), nil
	}
	resolved, err := a.ResolveToken(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: legacy token resolver unavailable", ErrUnauthenticated)
	}
	return strings.TrimSpace(resolved), nil
}
