package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type Manager struct {
	Root string
	Git  string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

type repositoryConfig struct {
	LocalPath string `json:"local_path"`
	CloneURL  string `json:"clone_url"`
}

func NewManager(root string) *Manager {
	return &Manager{Root: root, Git: "git", locks: map[string]*sync.Mutex{}}
}

func (m *Manager) Ensure(ctx context.Context, repository postgres.Repository) (string, error) {
	cfg, err := decodeConfig(repository.Config)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.LocalPath) != "" {
		if err := ensureGitRepository(ctx, m.git(), cfg.LocalPath); err != nil {
			return "", fmt.Errorf("validate repository local_path: %w", err)
		}
		return cfg.LocalPath, nil
	}
	if strings.TrimSpace(m.Root) == "" {
		return "", errors.New("repository root is required")
	}
	owner, name, err := splitFullName(repository.FullName)
	if err != nil {
		return "", err
	}
	path := filepath.Join(m.Root, owner, name)
	lock := m.repositoryLock(path)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create repository parent: %w", err)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		if out, err := exec.CommandContext(ctx, m.git(), "-C", path, "fetch", "--prune", "origin").CombinedOutput(); err != nil {
			return "", fmt.Errorf("fetch repository %s: %w: %s", repository.FullName, err, strings.TrimSpace(string(out)))
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect repository %s: %w", repository.FullName, err)
	}

	cloneURL := strings.TrimSpace(cfg.CloneURL)
	if cloneURL == "" {
		if repository.Provider != "github" {
			return "", fmt.Errorf("repository %s requires config.clone_url for provider %s", repository.FullName, repository.Provider)
		}
		cloneURL = "https://github.com/" + owner + "/" + name + ".git"
	}
	if out, err := exec.CommandContext(ctx, m.git(), "clone", "--filter=blob:none", "--no-checkout", cloneURL, path).CombinedOutput(); err != nil {
		_ = os.RemoveAll(path)
		return "", fmt.Errorf("clone repository %s: %w: %s", repository.FullName, err, strings.TrimSpace(string(out)))
	}
	return path, nil
}

func (m *Manager) git() string {
	if strings.TrimSpace(m.Git) == "" {
		return "git"
	}
	return m.Git
}

func (m *Manager) repositoryLock(path string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks == nil {
		m.locks = map[string]*sync.Mutex{}
	}
	if lock := m.locks[path]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	m.locks[path] = lock
	return lock
}

func decodeConfig(raw json.RawMessage) (repositoryConfig, error) {
	if len(raw) == 0 {
		return repositoryConfig{}, nil
	}
	var cfg repositoryConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return repositoryConfig{}, fmt.Errorf("decode repository source config: %w", err)
	}
	return cfg, nil
}

func splitFullName(fullName string) (string, string, error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(fullName), "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || owner == "." || owner == ".." || name == "." || name == ".." {
		return "", "", fmt.Errorf("invalid repository full name %q", fullName)
	}
	return owner, name, nil
}

func ensureGitRepository(ctx context.Context, git, path string) error {
	out, err := exec.CommandContext(ctx, git, "-C", path, "rev-parse", "--git-dir").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
