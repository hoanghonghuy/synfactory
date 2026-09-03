package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

var ErrNoEventRoute = errors.New("no route for github logical event")

type EventProcessorStore interface {
	ClaimEvent(ctx context.Context, owner string, now time.Time, leaseDuration time.Duration) (postgres.InboxEvent, bool, error)
	CreateJob(ctx context.Context, job postgres.NewJob) (domain.Job, bool, error)
	CompleteEvent(ctx context.Context, id int64, owner string, now time.Time) error
	RetryEvent(ctx context.Context, id int64, owner string, now, nextAttempt time.Time, processErr string) error
	DeadLetterEvent(ctx context.Context, id int64, owner string, now time.Time, processErr string) error
}

type EventProcessor struct {
	store         EventProcessorStore
	owner         string
	pollInterval  time.Duration
	leaseDuration time.Duration
	maxAttempts   int
	wake          <-chan struct{}
	now           func() time.Time
}

func NewEventProcessor(store EventProcessorStore, owner string, pollInterval, leaseDuration time.Duration, maxAttempts int, wake <-chan struct{}) *EventProcessor {
	if owner == "" {
		owner = "event-processor"
	}
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	return &EventProcessor{
		store:         store,
		owner:         owner,
		pollInterval:  pollInterval,
		leaseDuration: leaseDuration,
		maxAttempts:   maxAttempts,
		wake:          wake,
		now:           func() time.Time { return time.Now().UTC() },
	}
}

func (p *EventProcessor) Run(ctx context.Context) error {
	for {
		if err := p.Drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("event processor drain failed", "error", err)
		}

		timer := time.NewTimer(p.pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		case <-p.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func (p *EventProcessor) Drain(ctx context.Context) error {
	for {
		now := p.now()
		event, claimed, err := p.store.ClaimEvent(ctx, p.owner, now, p.leaseDuration)
		if err != nil {
			return fmt.Errorf("claim inbox event: %w", err)
		}
		if !claimed {
			return nil
		}
		if err := p.process(ctx, event, now); err != nil {
			slog.Warn("route inbox event failed", "event_id", event.ID, "kind", event.Kind, "attempt", event.ProcessAttempt, "error", err)
		}
	}
}

func (p *EventProcessor) process(ctx context.Context, event postgres.InboxEvent, now time.Time) error {
	job, err := RouteEvent(event, now)
	if err == nil {
		if _, _, err = p.store.CreateJob(ctx, job); err == nil {
			if err = p.store.CompleteEvent(ctx, event.ID, p.owner, p.now()); err == nil {
				return nil
			}
		}
	}

	if event.ProcessAttempt >= p.maxAttempts {
		if deadErr := p.store.DeadLetterEvent(ctx, event.ID, p.owner, p.now(), err.Error()); deadErr != nil {
			return errors.Join(err, fmt.Errorf("dead-letter event: %w", deadErr))
		}
		return err
	}

	retryAt := p.now().Add(eventRetryDelay(event.ProcessAttempt))
	if retryErr := p.store.RetryEvent(ctx, event.ID, p.owner, p.now(), retryAt, err.Error()); retryErr != nil {
		return errors.Join(err, fmt.Errorf("schedule event retry: %w", retryErr))
	}
	return err
}

func RouteEvent(event postgres.InboxEvent, now time.Time) (postgres.NewJob, error) {
	role, priority, err := routeRole(event)
	if err != nil {
		return postgres.NewJob{}, err
	}

	sum := sha256.Sum256([]byte(event.DedupeKey + "\x00" + string(role)))
	key := hex.EncodeToString(sum[:])
	sourceEventID := event.ID
	metadata, _ := json.Marshal(map[string]any{
		"source":          "github_event",
		"source_event_id": event.ID,
		"event_kind":      event.Kind,
		"delivery_id":     event.DeliveryID,
	})

	return postgres.NewJob{
		ID:            "job_" + key[:24],
		DedupeKey:     "github_route:" + key,
		RepositoryID:  event.RepositoryID,
		SourceEventID: &sourceEventID,
		Kind:          event.Kind,
		Role:          role,
		Subject:       event.Subject,
		Revision:      event.Revision,
		Priority:      priority,
		MaxAttempts:   3,
		AvailableAt:   now,
		Metadata:      metadata,
	}, nil
}

func routeRole(event postgres.InboxEvent) (domain.Role, int, error) {
	switch event.Kind {
	case KindIssueChanged:
		return domain.RolePM, 100, nil
	case KindIssueCommentChanged:
		if issueCommentTargetsPR(event.Payload) {
			return domain.RoleDev, 115, nil
		}
		return domain.RolePM, 105, nil
	case KindPRChanged:
		return domain.RoleTeamLead, 120, nil
	case KindPRReviewChanged, KindPRReviewCommentChanged:
		return domain.RoleDev, 125, nil
	case KindCICheckChanged, KindWorkflowChanged:
		return domain.RoleCIGuardian, 130, nil
	case KindBranchChanged:
		return domain.RoleTeamLead, 90, nil
	default:
		return "", 0, fmt.Errorf("%w: %s", ErrNoEventRoute, event.Kind)
	}
}

func issueCommentTargetsPR(payload json.RawMessage) bool {
	var envelope struct {
		Issue struct {
			PullRequest json.RawMessage `json:"pull_request"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return false
	}
	return len(envelope.Issue.PullRequest) > 0 && string(envelope.Issue.PullRequest) != "null"
}

func eventRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	delay := time.Second * time.Duration(1<<shift)
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}
