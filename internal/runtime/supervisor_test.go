package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupervisorRedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	binary := writeExecutable(t, dir, "emit", `#!/bin/sh
echo "token=$SECRET_TOKEN"
echo "stderr=$SECRET_TOKEN" >&2
`)
	s := NewSupervisor()
	result, err := s.Run(context.Background(), CommandSpec{
		ExecutionID: "redact", Name: binary, Env: map[string]string{"SECRET_TOKEN": "super-secret-value"},
		Secrets: []string{"super-secret-value"}, Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Stdout+result.Stderr, "super-secret-value") {
		t.Fatalf("secret leaked: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "[REDACTED]") || !strings.Contains(result.Stderr, "[REDACTED]") {
		t.Fatalf("expected redaction: %+v", result)
	}
}

func TestSupervisorTimeoutKillsCommand(t *testing.T) {
	dir := t.TempDir()
	binary := writeExecutable(t, dir, "sleepy", `#!/bin/sh
sleep 10
`)
	s := NewSupervisor()
	s.gracePeriod = 10 * time.Millisecond
	started := time.Now()
	result, err := s.Run(context.Background(), CommandSpec{ExecutionID: "timeout", Name: binary, Timeout: 50 * time.Millisecond})
	if !errors.Is(err, ErrRunTimedOut) {
		t.Fatalf("expected timeout, got %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("expected timed out result: %+v", result)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("timeout did not terminate process promptly")
	}
}

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
