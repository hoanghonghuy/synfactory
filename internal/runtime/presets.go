package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func newPresetAdapter(name string, cfg RuntimeConfig, supervisor *Supervisor) (Adapter, error) {
	binary := cfg.Binary
	if binary == "" {
		switch cfg.Kind {
		case ProviderCodex:
			binary = "codex"
		case ProviderCursor:
			binary = "cursor-agent"
		case ProviderAntigravity:
			binary = "agy"
		case ProviderClaude:
			binary = "claude"
		case ProviderOpenCode:
			binary = "opencode"
		default:
			return nil, fmt.Errorf("provider %q is not a CLI runtime", cfg.Kind)
		}
	}

	probeTimeout := cfg.ProbeTimeout.Duration
	if probeTimeout <= 0 {
		probeTimeout = 5 * time.Second
	}
	secrets := cfg.secretValues()
	common := func(request Request) CommandSpec {
		return CommandSpec{
			Name:    binary,
			Dir:     request.Workspace,
			Env:     cfg.Env,
			Secrets: secrets,
			Timeout: request.Timeout,
		}
	}

	switch cfg.Kind {
	case ProviderCodex:
		return newCLIAdapter(name, binary, supervisor, probeTimeout, []string{"--version"}, func(request Request, sessionID string) CommandSpec {
			spec := common(request)
			args := []string{"--ask-for-approval", "never", "exec"}
			if sessionID != "" {
				args = append(args, "resume", sessionID)
			}
			args = append(args, "--json")
			if HasPermission(request.Permissions, PermissionWriteRepo) {
				args = append(args, "--sandbox", "workspace-write")
			} else {
				args = append(args, "--sandbox", "read-only")
			}
			if request.Model != "" {
				args = append(args, "--model", request.Model)
			}
			args = append(args, cfg.ExtraArgs...)
			args = append(args, "-")
			spec.Args = args
			spec.Stdin = request.Prompt
			return spec
		}, parseGenericJSON), nil

	case ProviderCursor:
		return newCLIAdapter(name, binary, supervisor, probeTimeout, []string{"--version"}, func(request Request, sessionID string) CommandSpec {
			spec := common(request)
			args := []string{"--print", "--output-format", "json"}
			if !HasPermission(request.Permissions, PermissionWriteRepo) {
				args = append(args, "--mode", "ask")
			} else if cfg.AutoApprove {
				args = append(args, "--force")
			}
			if request.Model != "" {
				args = append(args, "--model", request.Model)
			}
			if sessionID != "" {
				args = append(args, "--resume", sessionID)
			}
			args = append(args, cfg.ExtraArgs...)
			args = append(args, request.Prompt)
			spec.Args = args
			return spec
		}, parseGenericJSON), nil

	case ProviderAntigravity:
		return newCLIAdapter(name, binary, supervisor, probeTimeout, []string{"--version"}, func(request Request, sessionID string) CommandSpec {
			spec := common(request)
			args := []string{"--print", request.Prompt, "--output-format", "json"}
			if request.Model != "" {
				args = append(args, "--model", request.Model)
			}
			if sessionID != "" {
				args = append(args, "--conversation", sessionID)
			}
			if cfg.AutoApprove && HasPermission(request.Permissions, PermissionWriteRepo) {
				args = append(args, "--dangerously-skip-permissions")
			}
			if request.Timeout > 0 {
				args = append(args, "--print-timeout", request.Timeout.String())
			}
			args = append(args, cfg.ExtraArgs...)
			spec.Args = args
			return spec
		}, parseAntigravityJSON), nil

	case ProviderClaude:
		return newCLIAdapter(name, binary, supervisor, probeTimeout, []string{"--version"}, func(request Request, sessionID string) CommandSpec {
			spec := common(request)
			args := []string{"--print", request.Prompt, "--output-format", "json"}
			if request.Model != "" {
				args = append(args, "--model", request.Model)
			}
			if sessionID != "" {
				args = append(args, "--resume", sessionID)
			}
			if !HasPermission(request.Permissions, PermissionWriteRepo) {
				args = append(args, "--permission-mode", "plan")
			} else if cfg.AutoApprove {
				args = append(args, "--dangerously-skip-permissions")
			}
			args = append(args, cfg.ExtraArgs...)
			spec.Args = args
			return spec
		}, parseClaudeJSON), nil

	case ProviderOpenCode:
		return newCLIAdapter(name, binary, supervisor, probeTimeout, []string{"--version"}, func(request Request, sessionID string) CommandSpec {
			spec := common(request)
			args := []string{"run", "--format", "json"}
			if request.Workspace != "" {
				args = append(args, "--dir", request.Workspace)
			}
			if request.Model != "" {
				args = append(args, "--model", request.Model)
			}
			if sessionID != "" {
				args = append(args, "--session", sessionID)
			}
			if cfg.AutoApprove && HasPermission(request.Permissions, PermissionWriteRepo) {
				args = append(args, "--auto")
			}
			args = append(args, cfg.ExtraArgs...)
			args = append(args, request.Prompt)
			spec.Args = args
			return spec
		}, parseGenericJSON), nil
	}
	return nil, fmt.Errorf("unsupported CLI provider %q", cfg.Kind)
}

func parseAntigravityJSON(process ProcessResult, runtimeName, model string) (Result, error) {
	result, err := parseGenericJSON(process, runtimeName, model)
	if err != nil {
		return result, err
	}
	objects, _ := decodeJSONObjects(process.Stdout)
	if len(objects) > 0 {
		object := objects[len(objects)-1]
		if status := strings.ToUpper(findString(object, "status")); status != "" && status != "SUCCESS" {
			class := FailurePermanent
			if status == "CANCELED" || status == "INTERRUPTED" {
				class = FailureCanceled
			} else if status == "WAITING" {
				class = FailureUnavailable
			}
			return result, Failure(class, fmt.Errorf("antigravity status %s", status))
		}
	}
	return result, nil
}

func parseClaudeJSON(process ProcessResult, runtimeName, model string) (Result, error) {
	result, err := parseGenericJSON(process, runtimeName, model)
	if err != nil {
		return result, err
	}
	objects, _ := decodeJSONObjects(process.Stdout)
	if len(objects) > 0 {
		object := objects[len(objects)-1]
		if raw, ok := object["is_error"].(bool); ok && raw {
			return result, Failure(FailurePermanent, fmt.Errorf("claude returned is_error=true"))
		}
		if turns, ok := object["num_turns"].(float64); ok {
			result.Events = append(result.Events, Event{Kind: "usage", Data: map[string]any{"num_turns": strconv.Itoa(int(turns))}})
		}
	}
	return result, nil
}
