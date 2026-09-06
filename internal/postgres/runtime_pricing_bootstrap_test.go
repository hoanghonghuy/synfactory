package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestDecodeRuntimePricingBootstrap(t *testing.T) {
	pricing, err := decodeRuntimePricingBootstrap(strings.NewReader(`{
		"pricing": [{
			"Version":"openai-gpt5-2026-09-01",
			"Provider":"openai",
			"Model":"gpt-5",
			"InputMicroUSDPerMillion":1250000,
			"OutputMicroUSDPerMillion":10000000,
			"RequestMicroUSD":0,
			"EffectiveAt":"2026-09-01T00:00:00Z"
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pricing) != 1 || pricing[0].Version != "openai-gpt5-2026-09-01" || pricing[0].Provider != "openai" || pricing[0].Model != "gpt-5" {
		t.Fatalf("unexpected bootstrap pricing: %+v", pricing)
	}
	if !pricing[0].EffectiveAt.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected effective_at: %s", pricing[0].EffectiveAt)
	}
}

func TestDecodeRuntimePricingBootstrapRejectsDuplicateVersion(t *testing.T) {
	_, err := decodeRuntimePricingBootstrap(strings.NewReader(`{
		"pricing": [
			{"Version":"v1","Provider":"openai","Model":"m1","EffectiveAt":"2026-09-01T00:00:00Z"},
			{"Version":"v1","Provider":"openai","Model":"m2","EffectiveAt":"2026-09-02T00:00:00Z"}
		]
	}`))
	if err == nil {
		t.Fatal("expected duplicate pricing version to fail")
	}
}

func TestDecodeRuntimePricingBootstrapRejectsUnknownField(t *testing.T) {
	_, err := decodeRuntimePricingBootstrap(strings.NewReader(`{
		"pricing": [{"Version":"v1","Provider":"openai","Model":"m1","EffectiveAt":"2026-09-01T00:00:00Z","unexpected":true}]
	}`))
	if err == nil {
		t.Fatal("expected unknown bootstrap field to fail")
	}
}
