package main

import (
	"context"
	"errors"

	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func configuredSecretProvider() (secrets.Provider, error) {
	return secrets.ConfiguredFromEnv()
}

func resolveOptionalSecret(ctx context.Context, provider secrets.Provider, logicalName, legacyValue string) (string, error) {
	return secrets.ResolveOptionalString(ctx, provider, logicalName, legacyValue)
}

func isSecretNotFound(err error) bool {
	return errors.Is(err, secrets.ErrNotFound)
}
