package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultOutputLimit = 2 << 20

type CommandSpec struct {
	ExecutionID string
	Name        string
	Args        []string
	Dir         string
	Env         map[string]string
	Stdin       string
	Secrets     []string
	Timeout     time.Duration
}

type ProcessResult struct {
	ExitCode   int
	Stdout     string
	Stderr     string
	StartedAt  time.Time
	FinishedAt time.Time
	TimedOut   bool
	Canceled   bool
}

type Supervisor struct {
	mu          sync.Mutex
	active      map[string]*exec.Cmd
	outputLimit int64
	gracePeriod time.Duration
	now         func() time.Time
}

func NewSupervisor() *Supervisor {
	return &Supervisor{
		active:      make(map[string]*exec.Cmd),
		outputLimit: defaultOutputLimit,
		gracePeriod: 2 * time.Second,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

func (s *Supervisor) Run(ctx context.Context, spec CommandSpec) (ProcessResult, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return ProcessResult{}, Failure(FailureUnavailable, errors.New("runtime command is empty"))
	}
	if spec.ExecutionID == "" {
		return ProcessResult{}, errors.New("execution id is required")
	}
	if spec.Timeout <= 0 {
		spec.Timeout = 30 * time.Minute
	}
	if s == nil {
		s = NewSupervisor()
	}

	runCtx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()
	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdin = strings.NewReader(spec.Stdin)
	cmd.Env = mergeEnv(os.Environ(), spec.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout := &limitedBuffer{limit: s.outputLimit}
	stderr := &limitedBuffer{limit: s.outputLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	started := s.now()
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ProcessResult{StartedAt: started, FinishedAt: s.now(), ExitCode: -1}, Failure(FailureUnavailable, fmt.Errorf("start %s: %w", spec.Name, err))
		}
		return ProcessResult{StartedAt: started, FinishedAt: s.now(), ExitCode: -1}, fmt.Errorf("start runtime command: %w", err)
	}

	s.mu.Lock()
	if _, exists := s.active[spec.ExecutionID]; exists {
		s.mu.Unlock()
		_ = terminateProcessGroup(cmd, 0)
		return ProcessResult{StartedAt: started, FinishedAt: s.now(), ExitCode: -1}, fmt.Errorf("execution %q is already active", spec.ExecutionID)
	}
	s.active[spec.ExecutionID] = cmd
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.active, spec.ExecutionID)
		s.mu.Unlock()
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var waitErr error
	var timedOut, canceled bool
	select {
	case waitErr = <-waitCh:
	case <-runCtx.Done():
		timedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
		canceled = !timedOut
		_ = terminateProcessGroup(cmd, syscall.SIGTERM)
		select {
		case waitErr = <-waitCh:
		case <-time.After(s.gracePeriod):
			_ = terminateProcessGroup(cmd, syscall.SIGKILL)
			waitErr = <-waitCh
		}
	}

	result := ProcessResult{
		ExitCode:   exitCode(waitErr),
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		StartedAt:  started,
		FinishedAt: s.now(),
		TimedOut:   timedOut,
		Canceled:   canceled,
	}
	redactor := NewRedactor(spec.Secrets...)
	result.Stdout = redactor.String(result.Stdout)
	result.Stderr = redactor.String(result.Stderr)

	if timedOut {
		return result, Failure(FailureTimeout, ErrRunTimedOut)
	}
	if canceled {
		return result, Failure(FailureCanceled, ErrRunCanceled)
	}
	if waitErr != nil {
		return result, classifyProcessError(waitErr, result.Stderr)
	}
	return result, nil
}

func (s *Supervisor) Cancel(executionID string) error {
	if s == nil || executionID == "" {
		return nil
	}
	s.mu.Lock()
	cmd := s.active[executionID]
	s.mu.Unlock()
	if cmd == nil {
		return nil
	}
	return terminateProcessGroup(cmd, syscall.SIGTERM)
}

func terminateProcessGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if signal == 0 {
		signal = syscall.SIGKILL
	}
	if err := syscall.Kill(-cmd.Process.Pid, signal); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func classifyProcessError(err error, diagnostics string) error {
	lower := strings.ToLower(diagnostics)
	switch {
	case strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "not authenticated"),
		strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "invalid api key"),
		strings.Contains(lower, "command not found"):
		return Failure(FailureUnavailable, err)
	case strings.Contains(lower, "rate limit"),
		strings.Contains(lower, "too many requests"),
		strings.Contains(lower, "temporarily unavailable"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "timeout"):
		return Failure(FailureTransient, err)
	default:
		return Failure(FailurePermanent, err)
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	values := make(map[string]string, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	for key, value := range overrides {
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	result := make([]string, 0, len(order))
	for _, key := range order {
		result = append(result, key+"="+values[key])
	}
	return result
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.buf.Write(p)
	return original, nil
}

func (b *limitedBuffer) String() string {
	if !b.truncated {
		return b.buf.String()
	}
	var out strings.Builder
	_, _ = io.Copy(&out, bytes.NewReader(b.buf.Bytes()))
	out.WriteString("\n[output truncated]\n")
	return out.String()
}
