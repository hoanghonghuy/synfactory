package main

import (
	"errors"
	"net/http"
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
	legacyValue string
	owner       string
}

func registerCredentialDiagnostics(mux *http.ServeMux, authorizer authz.RequestAuthorizer, cfg config.Config) {
	mux.HandleFunc("GET /api/security/credentials", func(w http.ResponseWriter, r *http.Request) {
		if _, err := authorizer.Authorize(r, authz.PermissionSecurityPolicy, ""); err != nil {
			status := http.StatusForbidden
			if errors.Is(err, authz.ErrUnauthenticated) {
				status = http.StatusUnauthorized
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}

		provider, err := configuredSecretProvider()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "credential provider unavailable"})
			return
		}
		tracker := secrets.NewTrackingProvider(provider)
		probes := []credentialProbe{
			{logicalName: "operator/token", legacyValue: cfg.OperatorToken, owner: "platform"},
			{logicalName: "github/webhook-secret", legacyValue: cfg.GitHubWebhookSecret, owner: "platform"},
			{logicalName: "github/oauth-client-secret", legacyValue: cfg.GitHubOAuthClientSecret, owner: "platform"},
			{logicalName: "github/token", legacyValue: cfg.GitHubToken, owner: "platform"},
		}

		for _, probe := range probes {
			tracker.Register(probe.logicalName, secrets.CredentialMetadata{Owner: probe.owner})
			_, resolveErr := tracker.Resolve(r.Context(), probe.logicalName)
			if resolveErr == nil {
				continue
			}
			if errors.Is(resolveErr, secrets.ErrNotFound) && strings.TrimSpace(probe.legacyValue) != "" {
				tracker.RecordAvailable(probe.logicalName, "legacy")
			}
		}

		writeJSON(w, http.StatusOK, credentialDiagnosticsResponse{
			Credentials: tracker.Diagnostics(time.Now(), credentialExpiryWarning),
		})
	})
}
