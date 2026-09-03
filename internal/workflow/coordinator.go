package workflow

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"
)

type Snapshot struct {
	Instance Instance
	Facts    Facts
}

type SnapshotSource interface {
	Snapshots(ctx context.Context) ([]Snapshot, error)
}

type Reconciler interface {
	Reconcile(ctx context.Context, instance Instance, facts Facts) (Instance, Decision, error)
}

type BacklogRefiller interface {
	RefillBacklog(ctx context.Context) error
}

type Coordinator struct {
	source   SnapshotSource
	engine   Reconciler
	refiller BacklogRefiller
	interval time.Duration
	wake     <-chan struct{}
}

func NewCoordinator(source SnapshotSource, engine Reconciler, refiller BacklogRefiller, interval time.Duration) *Coordinator {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Coordinator{source: source, engine: engine, refiller: refiller, interval: interval}
}

func (c *Coordinator) WithWake(wake <-chan struct{}) *Coordinator {
	c.wake = wake
	return c
}

func (c *Coordinator) Tick(ctx context.Context) error {
	if c == nil || c.source == nil || c.engine == nil {
		return errors.New("workflow source and engine are required")
	}
	snapshots, err := c.source.Snapshots(ctx)
	if err != nil {
		return err
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		left, right := snapshots[i].Instance, snapshots[j].Instance
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if left.LastDispatchedAt == nil && right.LastDispatchedAt != nil {
			return true
		}
		if left.LastDispatchedAt != nil && right.LastDispatchedAt == nil {
			return false
		}
		if left.LastDispatchedAt != nil && right.LastDispatchedAt != nil && !left.LastDispatchedAt.Equal(*right.LastDispatchedAt) {
			return left.LastDispatchedAt.Before(*right.LastDispatchedAt)
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})
	eligible := 0
	var failures []error
	for _, snapshot := range snapshots {
		_, decision, reconcileErr := c.engine.Reconcile(ctx, snapshot.Instance, snapshot.Facts)
		if reconcileErr != nil {
			if errors.Is(reconcileErr, ErrWIPLimit) || errors.Is(reconcileErr, ErrDependencyBlocked) {
				continue
			}
			failures = append(failures, reconcileErr)
			continue
		}
		if decision.Action != nil {
			eligible++
		}
	}
	if eligible == 0 && c.refiller != nil {
		if err := c.refiller.RefillBacklog(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (c *Coordinator) Run(ctx context.Context) error {
	for {
		if err := c.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("workflow reconciliation tick failed", "error", err)
		}
		timer := time.NewTimer(c.interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		case <-c.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}
