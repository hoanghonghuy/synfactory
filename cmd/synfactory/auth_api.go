package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authapi"
	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func registerAuthAPI(mux *http.ServeMux, store *postgres.Store, authorizer authz.RequestAuthorizer, cfg config.Config) {
	provider, err := configuredAPIRotatingProvider(cfg)
	if err != nil {
		slog.Error("configure auth secret provider", "error", err)
		return
	}
	registerAuthAPIWithSecretProvider(mux, store, authorizer, cfg, provider)
}

func registerAuthAPIWithSecretProvider(mux *http.ServeMux, store *postgres.Store, authorizer authz.RequestAuthorizer, cfg config.Config, provider secrets.Provider) {
	issuer := authz.SessionIssuer{Store: store}
	sessions := authz.SessionAuthorizer{Store: store}
	handler := authapi.Handler{
		Store:      store,
		Authorizer: authorizer,
		Sessions:   sessions,
		Issuer:     issuer,
	}
	handler.Register(mux)
	registerCredentialDiagnosticsWithProvider(mux, authorizer, cfg, store, provider)
	registerSecurityAudit(mux, authorizer, store, store)

	if cfg.GitHubOAuthClientID == "" {
		return
	}
	clientSecret, err := resolveOptionalSecret(
		context.Background(),
		provider,
		"github/oauth-client-secret",
		"",
	)
	if err != nil {
		slog.Error("resolve github oauth client secret", "error", err)
		return
	}
	if clientSecret == "" {
		slog.Error("github oauth client secret is not configured")
		return
	}
	oauth := authapi.OAuthHandler{
		Store: store,
		Provider: authz.GitHubOAuthProvider{
			ClientID:     cfg.GitHubOAuthClientID,
			ClientSecret: clientSecret,
			Client:       &http.Client{Timeout: 15 * time.Second},
		},
		Issuer:       issuer,
		ClientID:     cfg.GitHubOAuthClientID,
		AuthorizeURL: "https://github.com/login/oauth/authorize",
		RedirectURI:  cfg.GitHubOAuthRedirectURI,
		ReturnPath:   "/",
	}
	oauth.Register(mux)
}
