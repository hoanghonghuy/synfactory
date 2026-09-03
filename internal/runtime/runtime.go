package runtime

import (
	"context"
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
}

type Result struct {
	SessionID string
	ExitCode  int
	Summary   string
	Artifacts []string
}

type Adapter interface {
	Name() string
	Probe(ctx context.Context) error
	Run(ctx context.Context, request Request) (Result, error)
	Resume(ctx context.Context, sessionID string, request Request) (Result, error)
	Cancel(ctx context.Context, sessionID string) error
}
