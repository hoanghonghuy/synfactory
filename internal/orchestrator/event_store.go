package orchestrator

import (
	"context"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

// WorkflowEventStore lets the durable GitHub inbox keep its claim/retry/dead-letter
// lifecycle while replacing the legacy direct event->role job route with a wake-up
// of the authoritative workflow reconciler. This prevents webhook jobs and workflow
// jobs from duplicating the same repository action.
type WorkflowEventStore struct {
	*postgres.Store
	Wake func()
}

func (s *WorkflowEventStore) CreateJob(_ context.Context, job postgres.NewJob) (domain.Job, bool, error) {
	if s.Wake != nil {
		s.Wake()
	}
	// EventProcessor only requires CreateJob to succeed before it durably marks
	// the inbox event complete. The authoritative workflow coordinator will
	// derive the actual job from current GitHub facts.
	return domain.Job{ID: job.ID}, false, nil
}
