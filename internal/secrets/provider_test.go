package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvProviderResolvesLogicalNameWithoutLeakingValue(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_GITHUB_APP_PRIVATE_KEY", "top-secret")
	value, err := (EnvProvider{Prefix: "SYNFACTORY_SECRET_"}).Resolve(context.Background(), "github/app-private-key")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "top-secret" {
		t.Fatalf("resolved value = %q", got)
	}
	if value.Provider != "env" {
		t.Fatalf("provider = %q", value.Provider)
	}
	if got := value.String(); got != "[REDACTED]" {
		t.Fatalf("String() leaked secret: %q", got)
	}
	for _, formatted := range []string{fmt.Sprint(value), fmt.Sprintf("%+v", value), fmt.Sprintf("%#v", value)} {
		if strings.Contains(formatted, "top-secret") {
			t.Fatalf("formatted Value leaked secret: %q", formatted)
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "top-secret") {
		t.Fatalf("JSON leaked secret: %s", encoded)
	}
}

func TestFileProviderKeepsResolutionInsideConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "github", "webhook-secret")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hook-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	value, err := (FileProvider{Root: root}).Resolve(context.Background(), "github/webhook-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(value.CloneBytes()); got != "hook-value\n" {
		t.Fatalf("resolved value = %q", got)
	}
	if value.Provider != "file" {
		t.Fatalf("provider = %q", value.Provider)
	}

	for _, name := range []string{"../outside", "/absolute", `..\\outside`} {
		if _, err := (FileProvider{Root: root}).Resolve(context.Background(), name); err == nil {
			t.Fatalf("Resolve(%q) unexpectedly succeeded", name)
		}
	}
}

func TestFileProviderRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideSecret := filepath.Join(outside, "token")
	if err := os.WriteFile(outsideSecret, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := (FileProvider{Root: root}).Resolve(context.Background(), "escape/token"); err == nil {
		t.Fatal("symlink escape unexpectedly resolved")
	}
}

func TestProvidersClassifyMissingSecrets(t *testing.T) {
	if _, err := (EnvProvider{Prefix: "SYNFACTORY_SECRET_"}).Resolve(context.Background(), "definitely/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("env missing error = %v", err)
	}
	if _, err := (FileProvider{Root: t.TempDir()}).Resolve(context.Background(), "definitely/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("file missing error = %v", err)
	}
}

func TestFileProviderRequiresAbsoluteRoot(t *testing.T) {
	if _, err := (FileProvider{Root: "relative"}).Resolve(context.Background(), "github/token"); err == nil {
		t.Fatal("relative root unexpectedly accepted")
	}
}
