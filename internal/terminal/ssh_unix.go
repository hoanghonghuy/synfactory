//go:build !windows

package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

type SSHBackend struct{}

type sshProcess struct {
	ptyFile  *os.File
	cmd      *exec.Cmd
	once     sync.Once
	closeErr error
}

func (SSHBackend) Start(ctx context.Context, target Target, size Size) (Process, error) {
	if target.Kind != TargetSSH {
		return nil, fmt.Errorf("ssh terminal backend cannot start target kind %q", target.Kind)
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	port := target.Port
	if port == 0 {
		port = 22
	}
	args := []string{
		"-tt",
		"-p", strconv.Itoa(port),
		"-i", target.IdentityFile,
		"-o", "BatchMode=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + target.KnownHostsFile,
		"--",
		target.User + "@" + target.Host,
	}
	if command := remoteShellCommand(target); command != "" {
		args = append(args, command)
	}
	// The request context only governs admission/startup. Once the PTY has been
	// created, its lifetime belongs to the terminal Manager so the shell is not
	// killed as soon as the HTTP create-session request finishes.
	cmd := exec.Command("ssh", args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ws := &pty.Winsize{Rows: normalizeDimension(size.Rows, 24), Cols: normalizeDimension(size.Cols, 80)}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, fmt.Errorf("start SSH terminal PTY: %w", err)
	}
	return &sshProcess{ptyFile: ptmx, cmd: cmd}, nil
}

func remoteShellCommand(target Target) string {
	shell := strings.TrimSpace(target.Shell)
	workDir := strings.TrimSpace(target.WorkDir)
	if shell == "" && workDir == "" {
		return ""
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	if workDir == "" {
		return "exec " + shellQuote(shell)
	}
	return "cd -- " + shellQuote(workDir) + " && exec " + shellQuote(shell)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (p *sshProcess) Read(buffer []byte) (int, error) { return p.ptyFile.Read(buffer) }
func (p *sshProcess) Write(buffer []byte) (int, error) { return p.ptyFile.Write(buffer) }
func (p *sshProcess) Resize(size Size) error {
	return pty.Setsize(p.ptyFile, &pty.Winsize{Rows: normalizeDimension(size.Rows, 24), Cols: normalizeDimension(size.Cols, 80)})
}
func (p *sshProcess) Wait() error { return p.cmd.Wait() }
func (p *sshProcess) Close() error {
	p.once.Do(func() {
		_ = p.ptyFile.Close()
		if p.cmd.Process != nil {
			if pgid, err := syscall.Getpgid(p.cmd.Process.Pid); err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
			}
			_ = p.cmd.Process.Kill()
		}
	})
	return p.closeErr
}
