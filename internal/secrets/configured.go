package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

const DefaultFileRoot = "/run/secrets"

// ConfiguredFromEnv builds the process-level logical secret provider without
// exposing backend-specific selection to workflow/runtime consumers.
func ConfiguredFromEnv() (Provider, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("SYNFACTORY_SECRET_PROVIDER")))
	switch backend {
	case "", "env":
		return EnvProvider{Prefix: "SYNFACTORY_"}, nil
	case "file":
		root := strings.TrimSpace(os.Getenv("SYNFACTORY_SECRET_FILE_ROOT"))
		if root == "" {
			root = DefaultFileRoot
		}
		return FileProvider{Root: root}, nil
	default:
		return nil, fmt.Errorf("unsupported SYNFACTORY_SECRET_PROVIDER %q (want env or file)", backend)
	}
}

// ResolveOptionalString prefers a logical secret and falls back to a legacy
// value only when the provider reports the logical secret as absent. A present
// but empty secret fails closed so broken rotation cannot silently revive an
// older credential.
func ResolveOptionalString(ctx context.Context, provider Provider, logicalName, legacyValue string) (string, error) {
	value, err := provider.Resolve(ctx, logicalName)
	if err == nil {
		resolved := strings.TrimSpace(string(value.CloneBytes()))
		if resolved == "" {
			return "", fmt.Errorf("secret %q is empty", logicalName)
		}
		return resolved, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("resolve secret %q: %w", logicalName, err)
	}
	return strings.TrimSpace(legacyValue), nil
}
