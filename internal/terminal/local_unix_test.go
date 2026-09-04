//go:build !windows

package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestLocalBackendInteractiveIOResizeAndClose(t *testing.T) {
	process, err := (LocalBackend{}).Start(context.Background(), Target{
		ID:      "local",
		Kind:    TargetLocal,
		WorkDir: t.TempDir(),
		Shell:   "/bin/sh",
	}, Size{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	if err := process.Resize(Size{Rows: 40, Cols: 120}); err != nil {
		t.Fatalf("resize local PTY: %v", err)
	}
	if err := process.Resize(Size{}); err == nil {
		t.Fatal("zero terminal size must be rejected")
	}
	if _, err := process.Write([]byte("printf 'synfactory-pty-ok\\n'\n")); err != nil {
		t.Fatalf("write local PTY: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		buffer := make([]byte, 1024)
		var output bytes.Buffer
		for output.Len() < 16*1024 {
			n, readErr := process.Read(buffer)
			if n > 0 {
				_, _ = output.Write(buffer[:n])
				if bytes.Contains(output.Bytes(), []byte("synfactory-pty-ok")) {
					result <- nil
					return
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					result <- errors.New("PTY closed before expected output")
				} else {
					result <- readErr
				}
				return
			}
		}
		result <- errors.New("expected PTY output not observed")
	}()

	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for interactive PTY output")
	}

	if err := process.Close(); err != nil {
		t.Fatalf("close local PTY: %v", err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("second close must be idempotent: %v", err)
	}
}

func TestLocalBackendRejectsCancelledStartAndWrongTargetKind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (LocalBackend{}).Start(ctx, Target{ID: "local", Kind: TargetLocal}, Size{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled start error = %v", err)
	}
	if _, err := (LocalBackend{}).Start(context.Background(), Target{ID: "remote", Kind: TargetSSH}, Size{}); err == nil {
		t.Fatal("local backend must reject SSH targets")
	}
}
