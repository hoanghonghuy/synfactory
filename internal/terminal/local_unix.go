//go:build !windows

package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

type LocalBackend struct{}

type localProcess struct {
	ptyFile *os.File
	cmd     *exec.Cmd
	once    sync.Once
	closeErr error
}

func (LocalBackend) Start(ctx context.Context, target Target, size Size) (Process, error) {
	if target.Kind != TargetLocal {
		return nil, fmt.Errorf("local terminal backend cannot start target kind %q", target.Kind)
	}
	shell := target.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = target.WorkDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ws := &pty.Winsize{Rows: normalizeDimension(size.Rows, 24), Cols: normalizeDimension(size.Cols, 80)}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, fmt.Errorf("start local terminal PTY: %w", err)
	}
	process := &localProcess{ptyFile: ptmx, cmd: cmd}
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = process.Close()
			case <-processDone(cmd):
			}
		}()
	}
	return process, nil
}

func normalizeDimension(value, fallback uint16) uint16 {
	if value == 0 {
		return fallback
	}
	return value
}

func processDone(cmd *exec.Cmd) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		if cmd.Process != nil {
			_, _ = cmd.Process.Wait()
		}
		close(done)
	}()
	return done
}

func (p *localProcess) Read(buffer []byte) (int, error) {
	return p.ptyFile.Read(buffer)
}

func (p *localProcess) Write(buffer []byte) (int, error) {
	return p.ptyFile.Write(buffer)
}

func (p *localProcess) Resize(size Size) error {
	if size.Rows == 0 || size.Cols == 0 {
		return fmt.Errorf("terminal rows and columns must be positive")
	}
	return pty.Setsize(p.ptyFile, &pty.Winsize{Rows: size.Rows, Cols: size.Cols})
}

func (p *localProcess) Wait() error {
	if p.cmd.ProcessState != nil {
		return nil
	}
	err := p.cmd.Wait()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func (p *localProcess) Close() error {
	p.once.Do(func() {
		var errs []error
		if p.ptyFile != nil {
			if err := p.ptyFile.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, err)
			}
		}
		if p.cmd != nil && p.cmd.Process != nil && p.cmd.ProcessState == nil {
			// creack/pty starts the shell in a new session whose process-group ID
			// is the shell PID. Killing the group ensures foreground children are
			// not orphaned when an operator closes a session or the API shuts down.
			if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				errs = append(errs, err)
			}
		}
		p.closeErr = errors.Join(errs...)
	})
	return p.closeErr
}

var _ Process = (*localProcess)(nil)
var _ io.ReadWriteCloser = (*localProcess)(nil)
