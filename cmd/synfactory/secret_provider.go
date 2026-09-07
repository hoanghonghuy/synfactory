package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

type apiCredentials struct {
	operatorToken string
	webhookSecret string
}

func configuredSecretProvider() (secrets.Provider, error) {
	return secrets.ConfiguredFromEnv()
}

func configuredAPIRotatingProvider(cfg config.Config) (*secrets.RotatingProvider, error) {
	primary, err := configuredSecretProvider()
	if err != nil {
		return nil, err
	}
	return secrets.NewRotatingProvider(secrets.FallbackProvider{
		Primary: primary,
		Values: map[string]string{
			"operator/token":             cfg.OperatorToken,
			"github/webhook-secret":      cfg.GitHubWebhookSecret,
			"github/oauth-client-secret": cfg.GitHubOAuthClientSecret,
			"github/token":               cfg.GitHubToken,
		},
	}), nil
}

func resolveOptionalSecret(ctx context.Context, provider secrets.Provider, logicalName, legacyValue string) (string, error) {
	return secrets.ResolveOptionalString(ctx, provider, logicalName, legacyValue)
}

func configuredAPICredentials(ctx context.Context, cfg config.Config) (apiCredentials, error) {
	provider, err := configuredAPIRotatingProvider(cfg)
	if err != nil {
		return apiCredentials{}, err
	}
	return resolveAPICredentialsWithProvider(ctx, provider)
}

func resolveAPICredentialsWithProvider(ctx context.Context, provider secrets.Provider) (apiCredentials, error) {
	operatorToken, err := resolveOptionalSecret(ctx, provider, "operator/token", "")
	if err != nil {
		return apiCredentials{}, fmt.Errorf("resolve operator token: %w", err)
	}
	webhookSecret, err := resolveOptionalSecret(ctx, provider, "github/webhook-secret", "")
	if err != nil {
		return apiCredentials{}, fmt.Errorf("resolve github webhook secret: %w", err)
	}
	return apiCredentials{operatorToken: operatorToken, webhookSecret: webhookSecret}, nil
}

func isSecretNotFound(err error) bool {
	return errors.Is(err, secrets.ErrNotFound)
}
