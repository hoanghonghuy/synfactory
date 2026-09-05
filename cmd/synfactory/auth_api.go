package main

import (
	"net/http"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/authapi"
	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

func registerAuthAPI(mux *http.ServeMux, store *postgres.Store, authorizer authz.RequestAuthorizer, cfg config.Config) {
	issuer := authz.SessionIssuer{Store: store}
	sessions := authz.SessionAuthorizer{Store: store}
	handler := authapi.Handler{
		Store:      store,
		Authorizer: authorizer,
		Sessions:   sessions,
		Issuer:     issuer,
	}
	handler.Register(mux)

	if cfg.GitHubOAuthClientID == "" {
		return
	}
	oauth := authapi.OAuthHandler{
		Store: store,
		Provider: authz.GitHubOAuthProvider{
			ClientID:     cfg.GitHubOAuthClientID,
			ClientSecret: cfg.GitHubOAuthClientSecret,
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
