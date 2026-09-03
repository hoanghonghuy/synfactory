package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadOnlyWorktreeDetectsMutationAndCleansUp(t *testing.T) {
	repo := t.TempDir()
	run(t, repo, "git", "init")
	run(t, repo, "git", "config", "user.email", "test@example.com")
	run(t, repo, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "base")

	manager := NewWorktreeManager(filepath.Join(t.TempDir(), "workspaces"))
	handle, err := manager.Acquire(context.Background(), Spec{ID: "review-1", SourcePath: repo, Revision: "HEAD", Access: AccessReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handle.Path, "README.md"), []byte("mutated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manager.Validate(context.Background(), handle); !errors.Is(err, ErrUnauthorizedMutation) {
		t.Fatalf("expected mutation error, got %v", err)
	}
	if err := manager.Release(context.Background(), handle); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(handle.Path); !os.IsNotExist(err) {
		t.Fatalf("workspace still exists: %v", err)
	}
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
}
