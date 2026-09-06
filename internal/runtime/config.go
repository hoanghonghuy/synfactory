package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type ProviderKind string

const (
	ProviderCodex       ProviderKind = "codex"
	ProviderCursor      ProviderKind = "cursor"
	ProviderAntigravity ProviderKind = "antigravity"
	ProviderClaude      ProviderKind = "claude"
	ProviderOpenCode    ProviderKind = "opencode"
	ProviderOpenAI      ProviderKind = "openai_compatible"
)

type RuntimeConfig struct {
	Kind                   ProviderKind      `json:"kind"`
	Binary                 string            `json:"binary,omitempty"`
	Model                  string            `json:"model,omitempty"`
	BaseURL                string            `json:"base_url,omitempty"`
	APIStyle               string            `json:"api_style,omitempty"`
	APIKeyEnv              string            `json:"api_key_env,omitempty"`
	SecretEnv              []string          `json:"secret_env,omitempty"`
	Env                    map[string]string `json:"env,omitempty"`
	ExtraArgs              []string          `json:"extra_args,omitempty"`
	AutoApprove            bool              `json:"auto_approve,omitempty"`
	ProbeTimeout           Duration          `json:"probe_timeout,omitempty"`
	BudgetInputTokenLimit  int64             `json:"budget_input_token_limit,omitempty"`
	BudgetOutputTokenLimit int64             `json:"budget_output_token_limit,omitempty"`
	RoutingCapabilityScore int64             `json:"routing_capability_score,omitempty"`
}

type CandidateConfig struct {
	Runtime string `json:"runtime"`
	Model   string `json:"model,omitempty"`
}

type RoleConfig struct {
	Chain          []CandidateConfig `json:"chain"`
	FallbackOn     []FailureClass    `json:"fallback_on,omitempty"`
	DynamicRouting bool              `json:"dynamic_routing,omitempty"`
}

type Config struct {
	Runtimes map[string]RuntimeConfig `json:"runtimes"`
	Roles    map[string]RoleConfig    `json:"roles"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		d.Duration = 0
		return nil
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func LoadConfigFile(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, errors.New("runtime config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read runtime config: %w", err)
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode runtime config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if len(c.Runtimes) == 0 {
		return errors.New("at least one runtime must be configured")
	}
	for name, runtimeCfg := range c.Runtimes {
		if strings.TrimSpace(name) == "" {
			return errors.New("runtime name cannot be empty")
		}
		if runtimeCfg.BudgetInputTokenLimit < 0 || runtimeCfg.BudgetOutputTokenLimit < 0 {
			return fmt.Errorf("runtime %q budget token limits must be non-negative", name)
		}
		if runtimeCfg.RoutingCapabilityScore < 0 || runtimeCfg.RoutingCapabilityScore > 100 {
			return fmt.Errorf("runtime %q routing capability score must be between 0 and 100", name)
		}
		switch runtimeCfg.Kind {
		case ProviderCodex, ProviderCursor, ProviderAntigravity, ProviderClaude, ProviderOpenCode:
		case ProviderOpenAI:
			if strings.TrimSpace(runtimeCfg.BaseURL) == "" {
				return fmt.Errorf("runtime %q base_url is required", name)
			}
		default:
			return fmt.Errorf("runtime %q has unsupported kind %q", name, runtimeCfg.Kind)
		}
	}
	for role, roleCfg := range c.Roles {
		if strings.TrimSpace(role) == "" {
			return errors.New("role name cannot be empty")
		}
		if len(roleCfg.Chain) == 0 {
			return fmt.Errorf("role %q has an empty runtime chain", role)
		}
		for _, candidate := range roleCfg.Chain {
			if _, ok := c.Runtimes[candidate.Runtime]; !ok {
				return fmt.Errorf("role %q references unknown runtime %q", role, candidate.Runtime)
			}
		}
		for _, class := range roleCfg.FallbackOn {
			switch class {
			case FailureUnavailable, FailureTransient, FailureTimeout:
			default:
				return fmt.Errorf("role %q cannot fallback on %q", role, class)
			}
		}
	}
	return nil
}

func (r RoleConfig) effectiveFallbackOn() map[FailureClass]bool {
	classes := r.FallbackOn
	if len(classes) == 0 {
		classes = []FailureClass{FailureUnavailable, FailureTransient}
	}
	result := make(map[FailureClass]bool, len(classes))
	for _, class := range classes {
		result[class] = true
	}
	return result
}

func (r RuntimeConfig) secretValues() []string {
	names := append([]string(nil), r.SecretEnv...)
	if r.APIKeyEnv != "" {
		names = append(names, r.APIKeyEnv)
	}
	values := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if value := os.Getenv(name); value != "" {
			values = append(values, value)
		}
	}
	return values
}
