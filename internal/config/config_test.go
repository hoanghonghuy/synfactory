package config

import (
	"errors"
	"testing"
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
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("unexpected addr: %s", cfg.Addr)
	}
	if cfg.DBMaxOpenConns != 20 || cfg.DBMaxIdleConns != 5 {
		t.Fatalf("unexpected pool defaults: %+v", cfg)
	}
}
