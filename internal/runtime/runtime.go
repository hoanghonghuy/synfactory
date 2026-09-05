package runtime

import (
	"context"
	"errors"
	"time"
)

type Permission string

const (
	PermissionReadRepo   Permission = "repo:read"
	PermissionWriteRepo  Permission = "repo:write"
	PermissionRunCommand Permission = "command:run"
	PermissionReviewPR   Permission = "pr:review"
	PermissionMergePR    Permission = "pr:merge"
)

type Outcome string

const (
	OutcomeSucceeded   Outcome = "succeeded"
	OutcomeFailed      Outcome = "failed"
	OutcomeTimedOut    Outcome = "timed_out"
	OutcomeCanceled    Outcome = "canceled"
	OutcomeUnavailable Outcome = "unavailable"
)

type FailureClass string

const (
	FailureUnavailable FailureClass = "unavailable"
	FailureTransient   FailureClass = "transient"
	FailurePermanent   FailureClass = "permanent"
	FailureTimeout     FailureClass = "timeout"
	FailureCanceled    FailureClass = "canceled"
	FailureBudget      FailureClass = "budget"
)

var (
	ErrRuntimeUnavailable = errors.New("runtime unavailable")
	ErrNoRuntimeRoute     = errors.New("no runtime route")
	ErrRunCanceled        = errors.New("runtime run canceled")
	ErrRunTimedOut        = errors.New("runtime run timed out")
)

type Event struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type SandboxMode string

const (
	SandboxHost   SandboxMode = "host"
	SandboxDocker SandboxMode = "docker"
)

type SandboxSpec struct {
	Mode           SandboxMode
	Image          string
	ReadOnly       bool
	NetworkAllowed bool
	Memory         string
	CPUs           string
	ContainerPath  string
}

type Request struct {
	RunID       string
	Repository  string
	Workspace   string
	Role        string
	Prompt      string
	Model       string
	Permissions []Permission
	Timeout     time.Duration
	Metadata    map[string]string
	Sandbox     SandboxSpec
}

type Usage struct {
	RequestCount int64
	InputTokens  int64
	OutputTokens int64
	RuntimeMS    int64
}

type Result struct {
	Runtime     string
	Model       string
	SessionID   string
	ExitCode    int
	Outcome     Outcome
	Summary     string
	Output      string
	Diagnostics string
	Artifacts   []string
	Events      []Event
	Usage       Usage
	StartedAt   time.Time
	FinishedAt  time.Time
}

type Attempt struct {
	Sequence     int
	Runtime      string
	Model        string
	FailureClass FailureClass
	Result       Result
	Err          error
}

type Observer interface {
	AttemptStarted(ctx context.Context, attempt Attempt) error
	AttemptFinished(ctx context.Context, attempt Attempt) error
}

type Adapter interface {
	Name() string
	Probe(ctx context.Context) error
	Run(ctx context.Context, request Request) (Result, error)
	Resume(ctx context.Context, sessionID string, request Request) (Result, error)
	Cancel(ctx context.Context, runID string) error
}

func HasPermission(permissions []Permission, target Permission) bool {
	for _, permission := range permissions {
		if permission == target {
			return true
		}
	}
	return false
}

type FailureError struct {
	Class FailureClass
	Err   error
}

func (e *FailureError) Error() string {
	if e == nil || e.Err == nil {
		return "runtime failure"
	}
	return e.Err.Error()
}

func (e *FailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Failure(class FailureClass, err error) error {
	if err == nil {
		return nil
	}
	return &FailureError{Class: class, Err: err}
}

func ClassifyFailure(err error) FailureClass {
	if err == nil {
		return ""
	}
	var failure *FailureError
	if errors.As(err, &failure) && failure.Class != "" {
		return failure.Class
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrRunTimedOut) {
		return FailureTimeout
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCanceled) {
		return FailureCanceled
	}
	if errors.Is(err, ErrRuntimeUnavailable) {
		return FailureUnavailable
	}
	return FailurePermanent
}
