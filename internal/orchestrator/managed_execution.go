package orchestrator

import (
	"context"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/verifier"
)

type RepositoryPreparer interface {
	Ensure(ctx context.Context, repository postgres.Repository) (string, error)
}

type ManagedRequestBuilder struct {
	Base       RequestBuilder
	Repository RepositoryPreparer
}

func (b ManagedRequestBuilder) Build(ctx context.Context, job domain.Job, repository postgres.Repository) (factoryruntime.Request, error) {
	if b.Repository != nil {
		if _, err := b.Repository.Ensure(ctx, repository); err != nil {
			return factoryruntime.Request{}, err
		}
	}
	return b.Base.Build(ctx, job, repository)
}

func (b ManagedRequestBuilder) Plan(ctx context.Context, job domain.Job, repository postgres.Repository, request factoryruntime.Request) (verifier.Plan, error) {
	return b.Base.Plan(ctx, job, repository, request)
}
