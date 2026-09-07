package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

const defaultSecretFileRoot = "/run/secrets"

func configuredSecretProvider() (secrets.Provider, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("SYNFACTORY_SECRET_PROVIDER")))
	switch backend {
	case "", "env":
		return secrets.EnvProvider{Prefix: "SYNFACTORY_"}, nil
	case "file":
		root := strings.TrimSpace(os.Getenv("SYNFACTORY_SECRET_FILE_ROOT"))
		if root == "" {
			root = defaultSecretFileRoot
		}
		return secrets.FileProvider{Root: root}, nil
	default:
		return nil, fmt.Errorf("unsupported SYNFACTORY_SECRET_PROVIDER %q (want env or file)", backend)
	}
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
