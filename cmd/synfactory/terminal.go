package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/terminal"
)

type configuredTerminal struct {
	manager *terminal.Manager
	handler *terminal.Handler
}

func configureTerminal(cfg config.Config) (*configuredTerminal, error) {
	var targets []terminal.Target
	if cfg.TerminalEnabled {
		loaded, err := terminal.LoadTargets(cfg.TerminalTargetsPath)
		if err != nil {
			return nil, fmt.Errorf("load terminal targets: %w", err)
		}
		targets = loaded
	}
	manager := terminal.NewManager(terminal.Config{
		Enabled:              cfg.TerminalEnabled,
		MaxSessions:          cfg.TerminalMaxSessions,
		MaxSessionsPerTarget: cfg.TerminalMaxPerTarget,
		IdleTimeout:          cfg.TerminalIdleTimeout,
		MaxLifetime:          cfg.TerminalMaxLifetime,
	}, targets, map[terminal.TargetKind]terminal.Backend{
		terminal.TargetLocal: terminal.LocalBackend{},
		terminal.TargetSSH:   terminal.SSHBackend{},
	})
	return &configuredTerminal{
		manager: manager,
		handler: &terminal.Handler{Manager: manager, Token: cfg.OperatorToken},
	}, nil
}

func (t *configuredTerminal) register(mux *http.ServeMux) {
	if t != nil && t.handler != nil {
		t.handler.Register(mux)
	}
}

func (t *configuredTerminal) runReaper(ctx context.Context) {
	if t == nil || t.manager == nil {
		return
	}
	interval := time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.manager.ReapExpired()
		}
	}
}

func (t *configuredTerminal) shutdown() error {
	if t == nil || t.manager == nil {
		return nil
	}
	return t.manager.Shutdown()
}
