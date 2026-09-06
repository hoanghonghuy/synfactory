package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/config"
	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func configuredGitHubClient(cfg config.Config) (*githubfactory.Client, bool, error) {
	provider, err := configuredSecretProvider()
	if err != nil {
		return nil, false, err
	}

	switch cfg.GitHubAuthMode {
	case "pat":
		value, err := provider.Resolve(context.Background(), "github/token")
		if isSecretNotFound(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("resolve github token: %w", err)
		}
		token := strings.TrimSpace(string(value.CloneBytes()))
		if token == "" {
			return nil, false, nil
		}
		return githubfactory.NewClient(cfg.GitHubAPIURL, token, nil), true, nil
	case "app":
		privateKey, err := resolveGitHubAppPrivateKey(provider, cfg.GitHubAppPrivateKeyFile)
		if err != nil {
			return nil, false, err
		}
		source, err := githubfactory.NewAppRepositoryTokenSource(cfg.GitHubAPIURL, cfg.GitHubAppID, privateKey, nil)
		if err != nil {
			return nil, false, fmt.Errorf("configure github app authentication: %w", err)
		}
		return githubfactory.NewClientWithTokenSource(cfg.GitHubAPIURL, source, nil), true, nil
	default:
		return nil, false, fmt.Errorf("unsupported github auth mode %q", cfg.GitHubAuthMode)
	}
}

func resolveGitHubAppPrivateKey(provider secrets.Provider, legacyFile string) ([]byte, error) {
	value, err := provider.Resolve(context.Background(), "github/app-private-key")
	if err == nil {
		privateKey := value.CloneBytes()
		if len(strings.TrimSpace(string(privateKey))) == 0 {
			return nil, fmt.Errorf("github app private key secret is empty")
		}
		return privateKey, nil
	}
	if !isSecretNotFound(err) {
		return nil, fmt.Errorf("resolve github app private key: %w", err)
	}
	privateKey, err := os.ReadFile(legacyFile)
	if err != nil {
		return nil, fmt.Errorf("read github app private key file: %w", err)
	}
	return privateKey, nil
}
