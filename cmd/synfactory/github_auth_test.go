package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func TestResolveGitHubAppPrivateKeyPrefersLogicalProvider(t *testing.T) {
	t.Setenv("SYNFACTORY_GITHUB_APP_PRIVATE_KEY", "provider-key")
	provider := secrets.EnvProvider{Prefix: "SYNFACTORY_"}

	key, err := resolveGitHubAppPrivateKey(provider, filepath.Join(t.TempDir(), "missing.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(key); got != "provider-key" {
		t.Fatalf("resolved key = %q, want provider-key", got)
	}
}

func TestResolveGitHubAppPrivateKeyFallsBackToLegacyFileWhenLogicalSecretMissing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "github-app.pem")
	if err := os.WriteFile(path, []byte("legacy-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	key, err := resolveGitHubAppPrivateKey(secrets.EnvProvider{Prefix: "SYNFACTORY_TEST_"}, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(key); got != "legacy-key" {
		t.Fatalf("resolved key = %q, want legacy-key", got)
	}
}

func TestResolveGitHubAppPrivateKeyRejectsEmptyProviderValue(t *testing.T) {
	t.Setenv("SYNFACTORY_GITHUB_APP_PRIVATE_KEY", "   ")
	_, err := resolveGitHubAppPrivateKey(secrets.EnvProvider{Prefix: "SYNFACTORY_"}, filepath.Join(t.TempDir(), "legacy.pem"))
	if err == nil {
		t.Fatal("resolveGitHubAppPrivateKey() error = nil, want empty-secret error")
	}
}
