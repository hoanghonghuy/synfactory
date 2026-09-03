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
	if cfg.GitHubAPIURL != "https://api.github.com" {
		t.Fatalf("unexpected github API URL: %s", cfg.GitHubAPIURL)
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
