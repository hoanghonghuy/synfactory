package attention

import (
	"context"
	"fmt"
	"time"
)

type Store interface {
	AttentionByID(context.Context, string) (Item, error)
	UpsertAttention(context.Context, Item) (Item, error)
}

type ResolutionRevalidator interface {
	UnderlyingResolved(context.Context, Item) (bool, error)
}

type Service struct {
	Store       Store
	Revalidator ResolutionRevalidator
	Now         func() time.Time
}

func (s Service) Acknowledge(ctx context.Context, id, actor string) (Item, error) {
	item, err := s.load(ctx, id)
	if err != nil {
		return Item{}, err
	}
	item, err = item.Acknowledge(actor, s.now())
	if err != nil {
		return Item{}, err
	}
	return s.Store.UpsertAttention(ctx, item)
}

func (s Service) Snooze(ctx context.Context, id, actor string, until time.Time) (Item, error) {
	item, err := s.load(ctx, id)
	if err != nil {
		return Item{}, err
	}
	item, err = item.Snooze(actor, until, s.now())
	if err != nil {
		return Item{}, err
	}
	return s.Store.UpsertAttention(ctx, item)
}

func (s Service) Resolve(ctx context.Context, id, actor string) (Item, error) {
	item, err := s.load(ctx, id)
	if err != nil {
		return Item{}, err
	}
	if s.Revalidator == nil {
		return Item{}, fmt.Errorf("attention resolution revalidator is required")
	}
	resolved, err := s.Revalidator.UnderlyingResolved(ctx, item)
	if err != nil {
		return Item{}, fmt.Errorf("revalidate underlying blocker: %w", err)
	}
	item, err = item.Resolve(actor, s.now(), resolved)
	if err != nil {
		return Item{}, err
	}
	return s.Store.UpsertAttention(ctx, item)
}

func (s Service) load(ctx context.Context, id string) (Item, error) {
	if s.Store == nil {
		return Item{}, fmt.Errorf("attention store is required")
	}
	if id == "" {
		return Item{}, fmt.Errorf("attention id is required")
	}
	return s.Store.AttentionByID(ctx, id)
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
