package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/config"
	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
)

func configuredGitHubClient(cfg config.Config) (*githubfactory.Client, bool, error) {
	switch cfg.GitHubAuthMode {
	case "pat":
		if strings.TrimSpace(cfg.GitHubToken) == "" {
			return nil, false, nil
		}
		return githubfactory.NewClient(cfg.GitHubAPIURL, cfg.GitHubToken, nil), true, nil
	case "app":
		privateKey, err := os.ReadFile(cfg.GitHubAppPrivateKeyFile)
		if err != nil {
			return nil, false, fmt.Errorf("read github app private key file: %w", err)
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
