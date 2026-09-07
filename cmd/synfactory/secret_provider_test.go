package main

import (
	"context"
	"errors"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

type staticSecretProvider struct {
	value secrets.Value
	err   error
}

func (p staticSecretProvider) Resolve(context.Context, string) (secrets.Value, error) {
	return p.value, p.err
}

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

func TestResolveOptionalSecretUsesLegacyOnlyWhenLogicalSecretMissing(t *testing.T) {
	provider := secrets.EnvProvider{Prefix: "TEST_"}
	got, err := resolveOptionalSecret(t.Context(), provider, "github/oauth-client-secret", "legacy-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy-secret" {
		t.Fatalf("resolved secret = %q, want legacy-secret", got)
	}
}

func TestResolveOptionalSecretRejectsPresentEmptyLogicalSecret(t *testing.T) {
	t.Setenv("TEST_GITHUB_OAUTH_CLIENT_SECRET", "   ")
	provider := secrets.EnvProvider{Prefix: "TEST_"}
	if _, err := resolveOptionalSecret(t.Context(), provider, "github/oauth-client-secret", "legacy-secret"); err == nil {
		t.Fatal("resolveOptionalSecret() error = nil, want empty-secret error")
	}
}
