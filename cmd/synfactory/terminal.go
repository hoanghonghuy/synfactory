package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/terminal"
)

const terminalAuditPath = "/var/lib/synfactory/terminal-audit/session-events.jsonl"

type configuredTerminal struct {
	manager *terminal.Manager
	handler *terminal.AuthorizedHandler
}

func configureTerminal(cfg config.Config, authorizer authz.RequestAuthorizer) (*configuredTerminal, error) {
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
	if cfg.TerminalEnabled {
		audit, err := terminal.NewFileAuditSink(terminalAuditPath)
		if err != nil {
			return nil, fmt.Errorf("configure terminal audit: %w", err)
		}
		manager.SetAuditSink(audit)
	}
	legacyEnableFlag := strings.TrimSpace(cfg.OperatorToken)
	if legacyEnableFlag == "" && authorizer != nil {
		legacyEnableFlag = "rbac-enabled"
	}
	baseHandler := &terminal.Handler{Manager: manager, Token: legacyEnableFlag}
	return &configuredTerminal{
		manager: manager,
		handler: &terminal.AuthorizedHandler{Handler: baseHandler, Authorizer: authorizer},
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
