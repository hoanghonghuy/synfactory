package main

import (
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

func TestAPISecurityRuntimeSharesPromotionsAcrossConsumers(t *testing.T) {
	t.Setenv("SYNFACTORY_SECRET_PROVIDER", "env")
	t.Setenv("SYNFACTORY_OPERATOR_TOKEN", "active-operator")
	t.Setenv("SYNFACTORY_GITHUB_WEBHOOK_SECRET", "active-webhook")

	runtime, err := configuredAPISecurityRuntime(t.Context(), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := runtime.operatorToken(t.Context()); err != nil || got != "active-operator" {
		t.Fatalf("initial operator token = %q, err = %v", got, err)
	}
	if got, err := runtime.webhookSecret(t.Context()); err != nil || got != "active-webhook" {
		t.Fatalf("initial webhook secret = %q, err = %v", got, err)
	}

	t.Setenv("NEXT_OPERATOR_TOKEN", "promoted-operator")
	t.Setenv("NEXT_GITHUB_WEBHOOK_SECRET", "promoted-webhook")
	candidate := secrets.EnvProvider{Prefix: "NEXT_"}
	if err := runtime.provider.Stage(t.Context(), "operator/token", candidate); err != nil {
		t.Fatal(err)
	}
	if err := runtime.provider.Stage(t.Context(), "github/webhook-secret", candidate); err != nil {
		t.Fatal(err)
	}
	if err := runtime.provider.Promote("operator/token"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.provider.Promote("github/webhook-secret"); err != nil {
		t.Fatal(err)
	}

	if got, err := runtime.operatorToken(t.Context()); err != nil || got != "promoted-operator" {
		t.Fatalf("promoted operator token = %q, err = %v", got, err)
	}
	if got, err := runtime.webhookSecret(t.Context()); err != nil || got != "promoted-webhook" {
		t.Fatalf("promoted webhook secret = %q, err = %v", got, err)
	}
}
