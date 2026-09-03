package workspace

import (
	"context"
	"errors"

	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
)

type Mode string
type Access string

const (
	ModeWorktree Mode = "worktree"
	ModeDocker   Mode = "docker"
	AccessReadOnly  Access = "read_only"
	AccessReadWrite Access = "read_write"
)

var ErrUnauthorizedMutation = errors.New("read-only workspace was mutated")

type Spec struct {
	ID             string
	SourcePath     string
	Revision       string
	Branch         string
	Mode           Mode
	Access         Access
	ContainerImage string
	NetworkAllowed bool
	Memory         string
	CPUs           string
}

type Handle struct {
	ID         string
	SourcePath string
	Path       string
	Revision   string
	Branch     string
	Mode       Mode
	Access     Access
	Sandbox    factoryruntime.SandboxSpec
}

type Manager interface {
	Acquire(ctx context.Context, spec Spec) (Handle, error)
	Validate(ctx context.Context, handle Handle) error
	Release(ctx context.Context, handle Handle) error
}

func AccessForPermissions(permissions []factoryruntime.Permission) Access {
	if factoryruntime.HasPermission(permissions, factoryruntime.PermissionWriteRepo) {
		return AccessReadWrite
	}
	return AccessReadOnly
}
