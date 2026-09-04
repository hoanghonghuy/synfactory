package config

import (
	"errors"
	"testing"
	"time"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("SYNFACTORY_DATABASE_URL", "")
	_, err := Load()
	if !errors.Is(err, ErrDatabaseURLRequired) {
		t.Fatalf("expected ErrDatabaseURLRequired, got %v", err)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("SYNFACTORY_DATABASE_URL", "postgres://example")
	t.Setenv("SYNFACTORY_OPERATOR_TOKEN", "")
	t.Setenv("SYNFACTORY_GITHUB_AUTH_MODE", "")
	t.Setenv("SYNFACTORY_TERMINAL_ENABLED", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.Mode != "all" {
		t.Fatalf("unexpected process defaults: %+v", cfg)
	}
	if cfg.DBMaxOpenConns != 20 || cfg.DBMaxIdleConns != 5 {
		t.Fatalf("unexpected pool defaults: %+v", cfg)
	}
	if cfg.ReconcileInterval != time.Hour || cfg.EventPollInterval != 5*time.Second || cfg.LeaseRecoveryInterval != 30*time.Second {
		t.Fatalf("unexpected scheduler defaults: %+v", cfg)
	}
	if cfg.EventLeaseDuration != 30*time.Second || cfg.EventMaxAttempts != 5 {
		t.Fatalf("unexpected event processor defaults: %+v", cfg)
	}
	if cfg.GitHubAPIURL != "https://api.github.com" || cfg.GitHubAuthMode != "pat" {
		t.Fatalf("unexpected github defaults: api=%s auth=%s", cfg.GitHubAPIURL, cfg.GitHubAuthMode)
	}
	if cfg.RepositoryRoot != "/var/lib/synfactory/repos" || cfg.WorkspaceRoot != "/var/lib/synfactory/workspaces" {
		t.Fatalf("unexpected worker storage defaults: %+v", cfg)
	}
	if cfg.RuntimeConfigPath != "/etc/synfactory/runtimes.json" || cfg.WorkerCapacity != 1 {
		t.Fatalf("unexpected runtime defaults: %+v", cfg)
	}
	if cfg.WorkerLeaseDuration != 2*time.Minute || cfg.WorkerHeartbeat != 30*time.Second || cfg.WorkerStaleAfter != 2*time.Minute {
		t.Fatalf("unexpected worker timing defaults: %+v", cfg)
	}
	if cfg.OperatorToken != "" {
		t.Fatalf("operator API must be disabled by default")
	}
	if cfg.TerminalEnabled {
		t.Fatal("operator terminal must be disabled by default")
	}
	if cfg.TerminalTargetsPath != "/etc/synfactory/terminal-targets.json" || cfg.TerminalMaxSessions != 2 || cfg.TerminalMaxPerTarget != 1 {
		t.Fatalf("unexpected terminal defaults: %+v", cfg)
	}
	if cfg.TerminalIdleTimeout != 15*time.Minute || cfg.TerminalMaxLifetime != 2*time.Hour {
		t.Fatalf("unexpected terminal timing defaults: %+v", cfg)
	}
}

func TestLoadOperatorToken(t *testing.T) {
	t.Setenv("SYNFACTORY_DATABASE_URL", "postgres://example")
	t.Setenv("SYNFACTORY_OPERATOR_TOKEN", "operator-secret")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OperatorToken != "operator-secret" {
		t.Fatalf("operator token was not loaded")
	}
}

func TestLoadTerminalConfiguration(t *testing.T) {
	t.Setenv("SYNFACTORY_DATABASE_URL", "postgres://example")
	t.Setenv("SYNFACTORY_OPERATOR_TOKEN", "operator-secret")
	t.Setenv("SYNFACTORY_TERMINAL_ENABLED", "true")
	t.Setenv("SYNFACTORY_TERMINAL_TARGETS", "/run/synfactory/terminal-targets.json")
	t.Setenv("SYNFACTORY_TERMINAL_MAX_SESSIONS", "4")
	t.Setenv("SYNFACTORY_TERMINAL_MAX_PER_TARGET", "2")
	t.Setenv("SYNFACTORY_TERMINAL_IDLE_TIMEOUT", "10m")
	t.Setenv("SYNFACTORY_TERMINAL_MAX_LIFETIME", "90m")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TerminalEnabled || cfg.TerminalTargetsPath != "/run/synfactory/terminal-targets.json" {
		t.Fatalf("unexpected terminal config: %+v", cfg)
	}
	if cfg.TerminalMaxSessions != 4 || cfg.TerminalMaxPerTarget != 2 || cfg.TerminalIdleTimeout != 10*time.Minute || cfg.TerminalMaxLifetime != 90*time.Minute {
		t.Fatalf("unexpected terminal policy: %+v", cfg)
	}
}

func TestLoadTerminalRequiresOperatorAuth(t *testing.T) {
	t.Setenv("SYNFACTORY_DATABASE_URL", "postgres://example")
	t.Setenv("SYNFACTORY_OPERATOR_TOKEN", "")
	t.Setenv("SYNFACTORY_TERMINAL_ENABLED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want terminal/operator authentication configuration error")
	}
}

func TestLoadGitHubAppMode(t *testing.T) {
	t.Setenv("SYNFACTORY_DATABASE_URL", "postgres://example")
	t.Setenv("SYNFACTORY_GITHUB_AUTH_MODE", "APP")
	t.Setenv("SYNFACTORY_GITHUB_APP_ID", "12345")
	t.Setenv("SYNFACTORY_GITHUB_APP_PRIVATE_KEY_FILE", "/run/secrets/github-app.pem")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHubAuthMode != "app" || cfg.GitHubAppID != 12345 || cfg.GitHubAppPrivateKeyFile != "/run/secrets/github-app.pem" {
		t.Fatalf("unexpected github app config: %+v", cfg)
	}
}

func TestLoadGitHubAppModeRequiresAppConfiguration(t *testing.T) {
	t.Setenv("SYNFACTORY_DATABASE_URL", "postgres://example")
	t.Setenv("SYNFACTORY_GITHUB_AUTH_MODE", "app")
	t.Setenv("SYNFACTORY_GITHUB_APP_ID", "")
	t.Setenv("SYNFACTORY_GITHUB_APP_PRIVATE_KEY_FILE", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing GitHub App configuration error")
	}
}

func TestLoadRejectsMixedOrUnknownGitHubAuthMode(t *testing.T) {
	t.Setenv("SYNFACTORY_DATABASE_URL", "postgres://example")
	t.Setenv("SYNFACTORY_GITHUB_AUTH_MODE", "auto")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want unsupported auth mode error")
	}
}
