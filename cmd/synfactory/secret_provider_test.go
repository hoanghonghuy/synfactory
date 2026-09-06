package main

import (
	"errors"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func TestConfiguredSecretProviderDefaultsToEnvironment(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "")
	t.Setenv("SYNFACTORY_GITHUB_TOKEN", "provider-token")

	provider, err := configuredSecretProvider()
	if err != nil {
		t.Fatal(err)
	}
	value, err := provider.Resolve(t.Context(), "github/token")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "provider-token" {
		t.Fatalf("resolved token = %q, want provider-token", got)
	}
}

func TestConfiguredSecretProviderUsesFileRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "file")
	t.Setenv("SYNFACTORY_SECRET_FILE_ROOT", root)

	provider, err := configuredSecretProvider()
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Resolve(t.Context(), "github/token")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrNotFound", err)
	}
}

func TestConfiguredSecretProviderRejectsUnknownBackend(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "vault")
	if _, err := configuredSecretProvider(); err == nil {
		t.Fatal("configuredSecretProvider() error = nil, want unsupported backend error")
	}
}
