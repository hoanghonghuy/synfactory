package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/postgres"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type RepositoryLister interface {
	ListRepositories(ctx context.Context) ([]postgres.Repository, error)
}

type WorkflowReconciler interface {
	Reconcile(ctx context.Context, instance workflow.Instance, facts workflow.Facts) (workflow.Instance, workflow.Decision, error)
}

type RepositoryRefiller struct {
	store  RepositoryLister
	engine WorkflowReconciler
	now    func() time.Time
}

func NewRepositoryRefiller(store RepositoryLister, engine WorkflowReconciler) *RepositoryRefiller {
	return &RepositoryRefiller{store: store, engine: engine, now: func() time.Time { return time.Now().UTC() }}
}

func (r *RepositoryRefiller) RefillBacklog(ctx context.Context) error {
	if r == nil || r.store == nil || r.engine == nil {
		return fmt.Errorf("repository store and workflow engine are required")
	}
	repositories, err := r.store.ListRepositories(ctx)
	if err != nil {
		return err
	}
	bucket := r.now().Truncate(time.Hour).Format(time.RFC3339)
	var failures []error
	for _, repository := range repositories {
		instance := workflow.NewInstance(repository.ID, workflow.KindRepository, "repository", bucket, 50)
		metadata, _ := json.Marshal(map[string]any{"repository_full_name": repository.FullName, "backlog_bucket": bucket})
		instance.Metadata = metadata
		if _, _, err := r.engine.Reconcile(ctx, instance, workflow.Facts{}); err != nil {
			if err == workflow.ErrWIPLimit {
				continue
			}
			failures = append(failures, fmt.Errorf("refill %s: %w", repository.FullName, err))
		}
	}
	return errorsJoin(failures)
}
