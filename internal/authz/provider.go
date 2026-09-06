package authz

import (
	"context"
	"fmt"
	"strings"
)

type ExternalIdentity struct {
	Provider string
	Subject  string
	Name     string
	Email    string
}

type IdentityProvider interface {
	Exchange(ctx context.Context, code, redirectURI string) (ExternalIdentity, error)
}

func ValidateExternalIdentity(identity ExternalIdentity) (ExternalIdentity, error) {
	identity.Provider = strings.TrimSpace(identity.Provider)
	identity.Subject = strings.TrimSpace(identity.Subject)
	identity.Name = strings.TrimSpace(identity.Name)
	identity.Email = strings.TrimSpace(identity.Email)
	if identity.Provider == "" {
		return ExternalIdentity{}, fmt.Errorf("identity provider is required")
	}
	if identity.Subject == "" {
		return ExternalIdentity{}, fmt.Errorf("immutable provider subject is required")
	}
	return identity, nil
}
