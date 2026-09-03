package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCodexPresetUsesReadOnlyAndParsesSession(t *testing.T) {
	dir := t.TempDir()
	binary := writeExecutable(t, dir, "codex", `#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
printf '%s\n' '{"type":"thread.started","thread_id":"thread-1"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}'
`)
	adapter, err := newPresetAdapter("codex-main", RuntimeConfig{Kind: ProviderCodex, Binary: binary}, NewSupervisor())
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Run(context.Background(), Request{
		RunID: "run-1", Workspace: dir, Prompt: "inspect", Model: "gpt-test",
		Permissions: []Permission{PermissionReadRepo}, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "thread-1" || result.Summary != "done" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestAntigravityParser(t *testing.T) {
	process := ProcessResult{ExitCode: 0, Stdout: `{"conversation_id":"conv-1","status":"SUCCESS","response":"finished"}`}
	result, err := parseAntigravityJSON(process, "agy", "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "conv-1" || result.Summary != "finished" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSecretEnvIsRedactedByCLI(t *testing.T) {
	t.Setenv("CURSOR_API_KEY", "cursor-secret-value")
	dir := t.TempDir()
	binary := writeExecutable(t, dir, "cursor-agent", `#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
echo "{\"result\":\"$CURSOR_API_KEY\",\"session_id\":\"s1\"}"
`)
	adapter, err := newPresetAdapter("cursor", RuntimeConfig{Kind: ProviderCursor, Binary: binary, SecretEnv: []string{"CURSOR_API_KEY"}}, NewSupervisor())
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Run(context.Background(), Request{RunID: "run-secret", Workspace: dir, Prompt: "x", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Output, "cursor-secret-value") || strings.Contains(result.Summary, "cursor-secret-value") {
		t.Fatalf("secret leaked in result: %+v", result)
	}
	if !strings.Contains(result.Output, "[REDACTED]") {
		t.Fatalf("expected redacted output: %q", result.Output)
	}
	_ = os.Getenv("CURSOR_API_KEY")
}
