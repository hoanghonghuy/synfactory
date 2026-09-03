package workflow

import (
	"context"
	"fmt"
	"time"
)

type TaskRegistry interface {
	ReserveTask(ctx context.Context, repositoryID, fingerprint, owner string, now time.Time, ttl time.Duration) (bool, error)
	BindTask(ctx context.Context, repositoryID, fingerprint, owner string, issueNumber int64, state string, seenAt time.Time) error
}

type TaskGuard struct {
	store TaskRegistry
	ttl   time.Duration
}

func NewTaskGuard(store TaskRegistry, ttl time.Duration) *TaskGuard {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &TaskGuard{store: store, ttl: ttl}
}

func (g *TaskGuard) Reserve(ctx context.Context, repositoryID, repository, capability, scope, owner string, now time.Time) (string, bool, error) {
	if g == nil || g.store == nil {
		return "", false, fmt.Errorf("task registry is required")
	}
	fingerprint := TaskFingerprint(repository, capability, scope)
	reserved, err := g.store.ReserveTask(ctx, repositoryID, fingerprint, owner, now, g.ttl)
	return fingerprint, reserved, err
}

func (g *TaskGuard) Bind(ctx context.Context, repositoryID, fingerprint, owner string, issueNumber int64, state string, now time.Time) error {
	if g == nil || g.store == nil {
		return fmt.Errorf("task registry is required")
	}
	return g.store.BindTask(ctx, repositoryID, fingerprint, owner, issueNumber, state, now)
}
