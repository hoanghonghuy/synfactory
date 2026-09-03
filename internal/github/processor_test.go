package github

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type processorMemoryStore struct {
	events       []postgres.InboxEvent
	created      []postgres.NewJob
	completed    []int64
	retried      []int64
	deadLettered []int64
	createErr    error
}

func (s *processorMemoryStore) ClaimEvent(_ context.Context, owner string, now time.Time, lease time.Duration) (postgres.InboxEvent, bool, error) {
	if len(s.events) == 0 {
		return postgres.InboxEvent{}, false, nil
	}
	event := s.events[0]
	s.events = s.events[1:]
	event.ProcessingOwner = owner
	until := now.Add(lease)
	event.ProcessingUntil = &until
	event.ProcessAttempt++
	return event, true, nil
}

func (s *processorMemoryStore) CreateJob(_ context.Context, job postgres.NewJob) (domain.Job, bool, error) {
	if s.createErr != nil {
		return domain.Job{}, false, s.createErr
	}
	s.created = append(s.created, job)
	return domain.Job{ID: job.ID}, true, nil
}

func (s *processorMemoryStore) CompleteEvent(_ context.Context, id int64, _ string, _ time.Time) error {
	s.completed = append(s.completed, id)
	return nil
}

func (s *processorMemoryStore) RetryEvent(_ context.Context, id int64, _ string, _, _ time.Time, _ string) error {
	s.retried = append(s.retried, id)
	return nil
}

func (s *processorMemoryStore) DeadLetterEvent(_ context.Context, id int64, _ string, _ time.Time, _ string) error {
	s.deadLettered = append(s.deadLettered, id)
	return nil
}

func TestRouteEventMapsCIFailureToGuardian(t *testing.T) {
	event := postgres.InboxEvent{
		ID:           1,
		DedupeKey:    "event-key",
		RepositoryID: "repo-1",
		Kind:         KindCICheckChanged,
		Subject:      "check_run:10",
		Revision:     "rev-1",
	}
	job, err := RouteEvent(event, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if job.Role != domain.RoleCIGuardian || job.Priority != 130 {
		t.Fatalf("unexpected route: %+v", job)
	}
}

func TestEventProcessorCreatesJobAndCompletesEvent(t *testing.T) {
	store := &processorMemoryStore{events: []postgres.InboxEvent{{
		ID:           7,
		DedupeKey:    "event-key",
		RepositoryID: "repo-1",
		Kind:         KindPRChanged,
		Subject:      "55",
		Revision:     "rev-1",
	}}}
	processor := NewEventProcessor(store, "router-1", time.Second, time.Minute, 5, nil)
	if err := processor.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.created) != 1 || store.created[0].Role != domain.RoleTeamLead {
		t.Fatalf("unexpected created jobs: %+v", store.created)
	}
	if len(store.completed) != 1 || store.completed[0] != 7 {
		t.Fatalf("event was not completed: %+v", store.completed)
	}
}

func TestEventProcessorDeadLettersAfterBudget(t *testing.T) {
	store := &processorMemoryStore{
		events: []postgres.InboxEvent{{
			ID:             9,
			DedupeKey:      "event-key",
			RepositoryID:   "repo-1",
			Kind:           KindPRChanged,
			Subject:        "55",
			Revision:       "rev-1",
			ProcessAttempt: 4,
		}},
		createErr: errors.New("database unavailable"),
	}
	processor := NewEventProcessor(store, "router-1", time.Second, time.Minute, 5, nil)
	if err := processor.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.deadLettered) != 1 || len(store.retried) != 0 {
		t.Fatalf("expected dead letter after budget: dead=%v retry=%v", store.deadLettered, store.retried)
	}
}
