package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredFromEnvDefaultsToEnv(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "")
	provider, err := ConfiguredFromEnv()
	if err != nil {
		t.Fatalf("ConfiguredFromEnv() error = %v", err)
	}
	if _, ok := provider.(EnvProvider); !ok {
		t.Fatalf("ConfiguredFromEnv() type = %T, want EnvProvider", provider)
	}
}

func TestConfiguredFromEnvUsesFileRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "file")
	t.Setenv("SYNFACTORY_SECRET_FILE_ROOT", root)
	provider, err := ConfiguredFromEnv()
	if err != nil {
		t.Fatalf("ConfiguredFromEnv() error = %v", err)
	}
	fileProvider, ok := provider.(FileProvider)
	if !ok {
		t.Fatalf("ConfiguredFromEnv() type = %T, want FileProvider", provider)
	}
	if fileProvider.Root != root {
		t.Fatalf("FileProvider.Root = %q, want %q", fileProvider.Root, root)
	}
}

func TestConfiguredFromEnvFileDefaultRoot(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "file")
	_ = os.Unsetenv("SYNFACTORY_SECRET_FILE_ROOT")
	provider, err := ConfiguredFromEnv()
	if err != nil {
		t.Fatalf("ConfiguredFromEnv() error = %v", err)
	}
	fileProvider := provider.(FileProvider)
	if filepath.Clean(fileProvider.Root) != filepath.Clean(DefaultFileRoot) {
		t.Fatalf("FileProvider.Root = %q, want %q", fileProvider.Root, DefaultFileRoot)
	}
}

func TestConfiguredFromEnvRejectsUnsupportedBackend(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "vault")
	if _, err := ConfiguredFromEnv(); err == nil {
		t.Fatal("ConfiguredFromEnv() error = nil, want unsupported backend error")
	}
}
