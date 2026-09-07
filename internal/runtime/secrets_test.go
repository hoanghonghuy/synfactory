package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

type runtimeSecretProvider map[string][]byte

func (p runtimeSecretProvider) Resolve(_ context.Context, logicalName string) (secrets.Value, error) {
	_, ok := p[logicalName]
	if !ok {
		return secrets.Value{}, secrets.ErrNotFound
	}
	// Tests cannot construct Value internals directly, so use an environment
	// provider with an isolated prefix for successful values.
	return secrets.EnvProvider{Prefix: "TEST_RUNTIME_"}.Resolve(context.Background(), logicalName)
}

func TestResolveRuntimeSecretsInjectsLogicalCLISecret(t *testing.T) {
	t.Setenv("TEST_RUNTIME_RUNTIME_OPENAI_KEY", "provider-value")
	t.Setenv("OPENAI_API_KEY", "legacy-value")
	cfg := RuntimeConfig{
		Kind:       ProviderCodex,
		SecretRefs: map[string]string{"OPENAI_API_KEY": "runtime/openai-key"},
	}
	resolved, err := resolveRuntimeSecrets(context.Background(), cfg, runtimeSecretProvider{"runtime/openai-key": []byte("provider-value")})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Env["OPENAI_API_KEY"]; got != "provider-value" {
		t.Fatalf("logical secret did not win: %q", got)
	}
	if len(resolved.secretValues()) == 0 || resolved.secretValues()[0] != "provider-value" {
		t.Fatal("resolved secret was not registered for redaction")
	}
}

func TestResolveRuntimeSecretsFallsBackOnlyWhenMissing(t *testing.T) {
	t.Setenv("ROUTER_KEY", "legacy-value")
	cfg := RuntimeConfig{Kind: ProviderOpenAI, APIKeyEnv: "ROUTER_KEY", APIKeySecret: "runtime/router-key"}
	resolved, err := resolveRuntimeSecrets(context.Background(), cfg, runtimeSecretProvider{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.resolvedAPIKey != "legacy-value" {
		t.Fatalf("unexpected fallback value %q", resolved.resolvedAPIKey)
	}
}

func TestResolveRuntimeSecretsRejectsPresentEmptySecret(t *testing.T) {
	t.Setenv("TEST_RUNTIME_RUNTIME_ROUTER_KEY", "")
	t.Setenv("ROUTER_KEY", "legacy-value")
	cfg := RuntimeConfig{Kind: ProviderOpenAI, APIKeyEnv: "ROUTER_KEY", APIKeySecret: "runtime/router-key"}
	_, err := resolveRuntimeSecrets(context.Background(), cfg, runtimeSecretProvider{"runtime/router-key": []byte{}})
	if err == nil {
		t.Fatal("expected empty logical secret to fail closed")
	}
}

func TestResolveRuntimeSecretProviderErrorIsPropagated(t *testing.T) {
	provider := failingRuntimeSecretProvider{}
	_, err := resolveRuntimeSecretString(context.Background(), provider, "runtime/key", "legacy")
	if err == nil || errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("expected provider failure")
	}
}

type failingRuntimeSecretProvider struct{}

func (failingRuntimeSecretProvider) Resolve(context.Context, string) (secrets.Value, error) {
	return secrets.Value{}, errors.New("provider unavailable")
}
