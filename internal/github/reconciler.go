package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/events"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

type ReconcileAPI interface {
	ListOpenIssues(ctx context.Context, owner, repo string) ([]Issue, error)
	ListOpenPulls(ctx context.Context, owner, repo string) ([]PullRequest, error)
	ListReviews(ctx context.Context, owner, repo string, number int64) ([]Review, error)
	ListCheckRuns(ctx context.Context, owner, repo, ref string) ([]CheckRun, error)
	GetBranch(ctx context.Context, owner, repo, branch string) (Branch, error)
}

type ReconcileStore interface {
	ListRepositories(ctx context.Context) ([]postgres.Repository, error)
	PutEvent(ctx context.Context, event postgres.InboxEvent) (postgres.InboxEvent, bool, error)
	PutReconcileState(ctx context.Context, state postgres.ReconcileState) (postgres.ReconcileState, error)
}

type Reconciler struct {
	api      ReconcileAPI
	store    ReconcileStore
	interval time.Duration
	wake     func()
	now      func() time.Time
}

func NewReconciler(api ReconcileAPI, store ReconcileStore, interval time.Duration, wake func()) *Reconciler {
	if interval <= 0 {
		interval = time.Hour
	}
	if wake == nil {
		wake = func() {}
	}
	return &Reconciler{
		api: api, store: store, interval: interval, wake: wake,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (r *Reconciler) Run(ctx context.Context) error {
	for {
		delay := r.interval
		if err := r.ReconcileAll(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("github reconciliation failed", "error", err)
			if rateErr, ok := IsRateLimited(err); ok {
				untilReset := time.Until(rateErr.RetryAt)
				if untilReset > 0 {
					delay = untilReset
				}
			}
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *Reconciler) ReconcileAll(ctx context.Context) error {
	repositories, err := r.store.ListRepositories(ctx)
	if err != nil {
		return err
	}
	var reconcileErr error
	for _, repository := range repositories {
		if repository.Provider != "github" || !repository.Enabled {
			continue
		}
		if err := r.ReconcileRepository(ctx, repository); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("reconcile %s: %w", repository.FullName, err))
			if _, rateLimited := IsRateLimited(err); rateLimited {
				break
			}
		}
	}
	return reconcileErr
}

func (r *Reconciler) ReconcileRepository(ctx context.Context, repository postgres.Repository) error {
	owner, repo, err := splitRepository(repository.FullName)
	if err != nil {
		return err
	}
	now := r.now()
	counts := map[string]int{}

	issues, err := r.api.ListOpenIssues(ctx, owner, repo)
	if err != nil {
		return err
	}
	for _, issue := range issues {
		payload, _ := json.Marshal(map[string]any{"reconciled": true, "issue": issue})
		inserted, err := r.emit(ctx, repository, KindIssueChanged, strconv.FormatInt(issue.Number, 10), issue.UpdatedAt, payload)
		if err != nil {
			return err
		}
		if inserted {
			counts["issues"]++
		}
	}

	pulls, err := r.api.ListOpenPulls(ctx, owner, repo)
	if err != nil {
		return err
	}
	for _, pull := range pulls {
		payload, _ := json.Marshal(map[string]any{"reconciled": true, "pull_request": pull})
		inserted, err := r.emit(ctx, repository, KindPRChanged, strconv.FormatInt(pull.Number, 10), pull.UpdatedAt, payload)
		if err != nil {
			return err
		}
		if inserted {
			counts["pull_requests"]++
		}

		reviews, err := r.api.ListReviews(ctx, owner, repo, pull.Number)
		if err != nil {
			return err
		}
		for _, review := range reviews {
			subject := fmt.Sprintf("%d:%d", pull.Number, review.ID)
			revision := strings.Join([]string{review.SubmittedAt, review.State}, ":")
			payload, _ := json.Marshal(map[string]any{"reconciled": true, "pull_request": map[string]any{"number": pull.Number}, "review": review})
			inserted, err := r.emit(ctx, repository, KindPRReviewChanged, subject, revision, payload)
			if err != nil {
				return err
			}
			if inserted {
				counts["reviews"]++
			}
		}

		if pull.Head.SHA != "" {
			checks, err := r.api.ListCheckRuns(ctx, owner, repo, pull.Head.SHA)
			if err != nil {
				return err
			}
			for _, check := range checks {
				subject := "check_run:" + strconv.FormatInt(check.ID, 10)
				revision := firstNonEmpty(check.UpdatedAt, check.CompletedAt, check.HeadSHA)
				payload, _ := json.Marshal(map[string]any{"reconciled": true, "check_run": check})
				inserted, err := r.emit(ctx, repository, KindCICheckChanged, subject, revision, payload)
				if err != nil {
					return err
				}
				if inserted {
					counts["checks"]++
				}
			}
		}
	}

	branchName := repository.DefaultBranch
	if branchName == "" {
		branchName = "main"
	}
	branch, err := r.api.GetBranch(ctx, owner, repo, branchName)
	if err != nil {
		return err
	}
	if branch.Commit.SHA != "" {
		payload, _ := json.Marshal(map[string]any{"reconciled": true, "branch": branch})
		inserted, err := r.emit(ctx, repository, KindBranchChanged, "refs/heads/"+branchName, branch.Commit.SHA, payload)
		if err != nil {
			return err
		}
		if inserted {
			counts["branches"]++
		}
	}

	watermark, _ := json.Marshal(map[string]any{"synthetic_events": counts})
	_, err = r.store.PutReconcileState(ctx, postgres.ReconcileState{
		RepositoryID:        repository.ID,
		LastIncrementalAt:   &now,
		LastFullReconcileAt: &now,
		Watermark:           watermark,
	})
	return err
}

func (r *Reconciler) emit(ctx context.Context, repository postgres.Repository, kind, subject, revision string, payload json.RawMessage) (bool, error) {
	if subject == "" || revision == "" {
		return false, nil
	}
	_, inserted, err := r.store.PutEvent(ctx, postgres.InboxEvent{
		DedupeKey:    events.DedupeKey("github", repository.FullName, kind, subject, revision),
		Provider:     "github",
		RepositoryID: repository.ID,
		Kind:         kind,
		Subject:      subject,
		Revision:     revision,
		Payload:      payload,
	})
	if err != nil {
		return false, err
	}
	if inserted {
		r.wake()
	}
	return inserted, nil
}

func splitRepository(fullName string) (string, string, error) {
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", fmt.Errorf("invalid github repository name %q", fullName)
	}
	return owner, repo, nil
}
