package runtime

import "testing"

func TestParseConfigValidatesRoutes(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{
  "runtimes": {
    "primary": {"kind":"codex", "model":"gpt-test"},
    "backup": {"kind":"openai_compatible", "base_url":"http://localhost:9999/v1", "api_style":"responses"}
  },
  "roles": {
    "developer": {"chain":[{"runtime":"primary"},{"runtime":"backup","model":"fallback"}]}
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Roles["developer"].Chain[1].Model; got != "fallback" {
		t.Fatalf("unexpected candidate model %q", got)
	}
}

func TestParseConfigRejectsUnknownRuntime(t *testing.T) {
	_, err := ParseConfig([]byte(`{
  "runtimes":{"primary":{"kind":"codex"}},
  "roles":{"developer":{"chain":[{"runtime":"missing"}]}}
}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
}
