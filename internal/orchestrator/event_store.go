package orchestrator

import (
	"context"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type WorkflowEventStore struct{ *postgres.Store; Wake func() }
func (s *WorkflowEventStore) CreateJob(_ context.Context,job postgres.NewJob)(domain.Job,bool,error){if s.Wake!=nil{s.Wake()};return domain.Job{ID:job.ID},false,nil}
