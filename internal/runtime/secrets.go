package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

// resolveRuntimeSecrets materializes logical credentials at the runtime
// composition boundary. Adapters receive only the values they need and remain
// independent from the configured secret backend.
func resolveRuntimeSecrets(ctx context.Context, cfg RuntimeConfig, provider secrets.Provider) (RuntimeConfig, error) {
	if provider == nil {
		configured, err := secrets.ConfiguredFromEnv()
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("configure runtime secret provider: %w", err)
		}
		provider = configured
	}

	resolved := cfg
	resolved.Env = cloneStringMap(cfg.Env)
	resolved.resolvedSecretValues = nil

	for envName, logicalName := range cfg.SecretRefs {
		envName = strings.TrimSpace(envName)
		logicalName = strings.TrimSpace(logicalName)
		if envName == "" || logicalName == "" {
			return RuntimeConfig{}, errorsForRuntimeSecretRef(envName, logicalName)
		}
		value, err := resolveRuntimeSecretString(ctx, provider, logicalName, os.Getenv(envName))
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("resolve runtime secret %q for %s: %w", logicalName, envName, err)
		}
		if value == "" {
			continue
		}
		resolved.Env[envName] = value
		resolved.resolvedSecretValues = append(resolved.resolvedSecretValues, value)
	}

	legacyAPIKey := ""
	if cfg.APIKeyEnv != "" {
		legacyAPIKey = os.Getenv(cfg.APIKeyEnv)
	}
	if strings.TrimSpace(cfg.APIKeySecret) != "" {
		value, err := resolveRuntimeSecretString(ctx, provider, cfg.APIKeySecret, legacyAPIKey)
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("resolve runtime api key %q: %w", cfg.APIKeySecret, err)
		}
		resolved.resolvedAPIKey = value
	} else {
		resolved.resolvedAPIKey = legacyAPIKey
	}
	if resolved.resolvedAPIKey != "" {
		resolved.resolvedSecretValues = append(resolved.resolvedSecretValues, resolved.resolvedAPIKey)
	}

	return resolved, nil
}

func resolveRuntimeSecretString(ctx context.Context, provider secrets.Provider, logicalName, legacyValue string) (string, error) {
	if provider == nil {
		return strings.TrimSpace(legacyValue), nil
	}
	return secrets.ResolveOptionalString(ctx, provider, logicalName, legacyValue)
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func errorsForRuntimeSecretRef(envName, logicalName string) error {
	switch {
	case envName == "":
		return fmt.Errorf("runtime secret_refs contains an empty environment variable name")
	case logicalName == "":
		return fmt.Errorf("runtime secret_refs[%q] has an empty logical secret name", envName)
	default:
		return nil
	}
}
