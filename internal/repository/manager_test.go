package repository

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

func TestSplitFullNameRejectsTraversal(t *testing.T) {
	for _, value := range []string{"owner/repo", "owner/repo/name", "../repo", "owner/..", ""} {
		_, _, err := splitFullName(value)
		if value == "owner/repo" && err != nil {
			t.Fatalf("valid repository rejected: %v", err)
		}
		if value != "owner/repo" && err == nil {
			t.Fatalf("invalid repository accepted: %q", value)
		}
	}
}

func TestEnsureAcceptsManagedLocalPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "init", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	manager := NewManager(filepath.Join(t.TempDir(), "managed"))
	repository := postgres.Repository{
		ID:       "repo-1",
		Provider: "github",
		FullName: "owner/repo",
		Config:   []byte(`{"local_path":"` + root + `"}`),
	}
	path, err := manager.Ensure(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if path != root {
		t.Fatalf("unexpected local path: %s", path)
	}
}
