package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

const credentialExpiryWarning = 7 * 24 * time.Hour

type credentialDiagnosticsResponse struct {
	Credentials []secrets.CredentialDiagnostic `json:"credentials"`
}

type credentialProbe struct {
	logicalName string
	owner       string
}

func credentialDiagnostics(ctx context.Context, cfg config.Config, now time.Time) ([]secrets.CredentialDiagnostic, error) {
	provider, err := configuredAPIRotatingProvider(cfg)
	if err != nil {
		return nil, err
	}
	return credentialDiagnosticsWithProvider(ctx, cfg, now, provider)
}

func credentialDiagnosticsWithProvider(ctx context.Context, cfg config.Config, now time.Time, provider secrets.Provider) ([]secrets.CredentialDiagnostic, error) {
	tracker := secrets.NewTrackingProvider(provider)
	probes := []credentialProbe{
		{logicalName: "operator/token", owner: "platform"},
		{logicalName: "github/webhook-secret", owner: "platform"},
		{logicalName: "github/oauth-client-secret", owner: "platform"},
		{logicalName: "github/token", owner: "platform"},
	}

	for _, probe := range probes {
		tracker.Register(probe.logicalName, secrets.CredentialMetadata{Owner: probe.owner})
		value, resolveErr := tracker.Resolve(ctx, probe.logicalName)
		if resolveErr == nil {
			if len(bytes.TrimSpace(value.CloneBytes())) == 0 {
				tracker.RecordUnavailable(probe.logicalName, value.Provider)
			}
			continue
		}
	}

	if cfg.GitHubAuthMode == "app" {
		const logicalName = "github/app-private-key"
		tracker.Register(logicalName, secrets.CredentialMetadata{Owner: "platform"})
		value, resolveErr := tracker.Resolve(ctx, logicalName)
		switch {
		case resolveErr == nil:
			if len(bytes.TrimSpace(value.CloneBytes())) == 0 {
				tracker.RecordUnavailable(logicalName, value.Provider)
			}
		case errors.Is(resolveErr, secrets.ErrNotFound) && legacyPrivateKeyFileAvailable(cfg.GitHubAppPrivateKeyFile):
			tracker.RecordAvailable(logicalName, "legacy-file")
		}
	}

	return tracker.Diagnostics(now, credentialExpiryWarning), nil
}

func legacyPrivateKeyFileAvailable(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func registerCredentialDiagnostics(mux *http.ServeMux, authorizer authz.RequestAuthorizer, cfg config.Config, audit securityAuditWriter) {
	provider, err := configuredAPIRotatingProvider(cfg)
	if err != nil {
		return
	}
	registerCredentialDiagnosticsWithProvider(mux, authorizer, cfg, audit, provider)
}

func registerCredentialDiagnosticsWithProvider(mux *http.ServeMux, authorizer authz.RequestAuthorizer, cfg config.Config, audit securityAuditWriter, provider secrets.Provider) {
	mux.HandleFunc("GET /api/security/credentials", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authorizer.Authorize(r, authz.PermissionSecurityPolicy, "")
		if err != nil {
			status := http.StatusForbidden
			if errors.Is(err, authz.ErrUnauthenticated) {
				status = http.StatusUnauthorized
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}

		diagnostics, err := credentialDiagnosticsWithProvider(r.Context(), cfg, time.Now(), provider)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "credential provider unavailable"})
			return
		}
		if err := appendSecurityAudit(r.Context(), audit, principal, "security.credentials.read", "security_credentials", "", "success"); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, credentialDiagnosticsResponse{Credentials: diagnostics})
	})
}
