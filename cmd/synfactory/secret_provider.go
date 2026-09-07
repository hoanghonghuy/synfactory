package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func configuredSecretProvider() (secrets.Provider, error) {
	return secrets.ConfiguredFromEnv()
}

func resolveOptionalSecret(ctx context.Context, provider secrets.Provider, logicalName, legacyValue string) (string, error) {
	value, err := provider.Resolve(ctx, logicalName)
	if err == nil {
		resolved := strings.TrimSpace(string(value.CloneBytes()))
		if resolved == "" {
			return "", fmt.Errorf("secret %q is empty", logicalName)
		}
		return resolved, nil
	}
	if !isSecretNotFound(err) {
		return "", fmt.Errorf("resolve secret %q: %w", logicalName, err)
	}
	return strings.TrimSpace(legacyValue), nil
}

func isSecretNotFound(err error) bool {
	return errors.Is(err, secrets.ErrNotFound)
}
