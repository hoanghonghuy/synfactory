package main

import (
	"context"

	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

type apiSecurityRuntime struct {
	provider    *secrets.RotatingProvider
	credentials apiCredentials
}

func configuredAPISecurityRuntime(ctx context.Context, cfg config.Config) (apiSecurityRuntime, error) {
	provider, err := configuredAPIRotatingProvider(cfg)
	if err != nil {
		return apiSecurityRuntime{}, err
	}
	credentials, err := resolveAPICredentialsWithProvider(ctx, provider)
	if err != nil {
		return apiSecurityRuntime{}, err
	}
	return apiSecurityRuntime{provider: provider, credentials: credentials}, nil
}

func (r apiSecurityRuntime) operatorToken(ctx context.Context) (string, error) {
	return resolveOptionalSecret(ctx, r.provider, "operator/token", "")
}

func (r apiSecurityRuntime) webhookSecret(ctx context.Context) (string, error) {
	return resolveOptionalSecret(ctx, r.provider, "github/webhook-secret", "")
}
