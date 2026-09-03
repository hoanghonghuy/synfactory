package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
)

type WorktreeManager struct {
	Root string
	Git  string
}

func NewWorktreeManager(root string) *WorktreeManager {
	return &WorktreeManager{Root: root, Git: "git"}
}

func (m *WorktreeManager) Acquire(ctx context.Context, spec Spec) (Handle, error) {
	if spec.ID == "" || spec.SourcePath == "" || spec.Revision == "" {
		return Handle{}, errors.New("workspace id, source path and revision are required")
	}
	if spec.Mode == "" {
		spec.Mode = ModeWorktree
	}
	if spec.Access == "" {
		spec.Access = AccessReadOnly
	}
	root := m.Root
	if root == "" {
		root = filepath.Join(os.TempDir(), "synfactory-workspaces")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Handle{}, fmt.Errorf("create workspace root: %w", err)
	}
	path := filepath.Join(root, sanitizeID(spec.ID))
	_ = os.RemoveAll(path)
	git := m.Git
	if git == "" {
		git = "git"
	}
	args := []string{"-C", spec.SourcePath, "worktree", "add"}
	if spec.Branch == "" || spec.Access == AccessReadOnly {
		args = append(args, "--detach", path, spec.Revision)
	} else {
		args = append(args, "-b", spec.Branch, path, spec.Revision)
	}
	if out, err := exec.CommandContext(ctx, git, args...).CombinedOutput(); err != nil {
		return Handle{}, fmt.Errorf("create git worktree: %w: %s", err, strings.TrimSpace(string(out)))
	}
	handle := Handle{ID: spec.ID, SourcePath: spec.SourcePath, Path: path, Revision: spec.Revision, Branch: spec.Branch, Mode: spec.Mode, Access: spec.Access}
	if spec.Mode == ModeDocker {
		if spec.ContainerImage == "" {
			_ = m.Release(ctx, handle)
			return Handle{}, errors.New("docker workspace requires container image")
		}
		handle.Sandbox = factoryruntime.SandboxSpec{Mode: factoryruntime.SandboxDocker, Image: spec.ContainerImage, ReadOnly: spec.Access == AccessReadOnly, NetworkAllowed: spec.NetworkAllowed, Memory: spec.Memory, CPUs: spec.CPUs, ContainerPath: "/workspace"}
	} else {
		handle.Sandbox = factoryruntime.SandboxSpec{Mode: factoryruntime.SandboxHost, ReadOnly: spec.Access == AccessReadOnly}
	}
	if err := ensureClean(ctx, git, handle.Path); err != nil {
		_ = m.Release(ctx, handle)
		return Handle{}, err
	}
	return handle, nil
}

func (m *WorktreeManager) Validate(ctx context.Context, handle Handle) error {
	if handle.Access != AccessReadOnly {
		return nil
	}
	git := m.Git
	if git == "" {
		git = "git"
	}
	status, err := gitStatus(ctx, git, handle.Path)
	if err != nil {
		return err
	}
	if len(status) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrUnauthorizedMutation, strings.TrimSpace(string(status)))
}

func (m *WorktreeManager) Release(ctx context.Context, handle Handle) error {
	if handle.Path == "" {
		return nil
	}
	git := m.Git
	if git == "" {
		git = "git"
	}
	if handle.SourcePath != "" {
		cmd := exec.CommandContext(ctx, git, "-C", handle.SourcePath, "worktree", "remove", "--force", handle.Path)
		if out, err := cmd.CombinedOutput(); err != nil && !os.IsNotExist(err) {
			_ = os.RemoveAll(handle.Path)
			return fmt.Errorf("remove git worktree: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return os.RemoveAll(handle.Path)
}

func ensureClean(ctx context.Context, git, path string) error {
	status, err := gitStatus(ctx, git, path)
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return fmt.Errorf("new workspace is not clean: %s", strings.TrimSpace(string(status)))
	}
	return nil
}

func gitStatus(ctx context.Context, git, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, git, "-C", path, "status", "--porcelain=v1", "--untracked-files=all")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("inspect workspace mutation: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func sanitizeID(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	clean := strings.Trim(b.String(), "-.")
	if clean == "" {
		clean = "workspace"
	}
	return clean
}
