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

func resolveOptionalSecret(ctx context.Context, provider secrets.Provider, logicalName, legacyValue string) (string, error) {
	return secrets.ResolveOptionalString(ctx, provider, logicalName, legacyValue)
}

func configuredAPICredentials(ctx context.Context, cfg config.Config) (apiCredentials, error) {
	provider, err := configuredSecretProvider()
	if err != nil {
		return apiCredentials{}, err
	}
	operatorToken, err := resolveOptionalSecret(ctx, provider, "operator/token", cfg.OperatorToken)
	if err != nil {
		return apiCredentials{}, fmt.Errorf("resolve operator token: %w", err)
	}
	webhookSecret, err := resolveOptionalSecret(ctx, provider, "github/webhook-secret", cfg.GitHubWebhookSecret)
	if err != nil {
		return apiCredentials{}, fmt.Errorf("resolve github webhook secret: %w", err)
	}
	return apiCredentials{operatorToken: operatorToken, webhookSecret: webhookSecret}, nil
}

func isSecretNotFound(err error) bool {
	return errors.Is(err, secrets.ErrNotFound)
}
