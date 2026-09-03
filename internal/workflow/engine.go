package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

type Store interface {
	UpsertWorkflow(ctx context.Context, instance Instance) (Instance, error)
	DependenciesSatisfied(ctx context.Context, workflowID string) (bool, error)
	ActiveRoleCount(ctx context.Context, role domain.Role) (int, error)
	ActionSucceeded(ctx context.Context, workflowID string, kind ActionKind, revision string) (bool, error)
	LatestActionStatus(ctx context.Context, workflowID string, kind ActionKind, revision string) (domain.JobStatus, bool, error)
	LatestActionDecision(ctx context.Context, workflowID string, kind ActionKind, revision string) (string, bool, error)
	ApplyDecision(ctx context.Context, workflowID string, decision Decision, actor domain.Role, now time.Time) (Instance, error)
	DispatchAction(ctx context.Context, workflowID string, decision Decision, job JobSpec, actor domain.Role, now time.Time) (Instance, DispatchResult, error)
	RegisterTask(ctx context.Context, repositoryID, fingerprint string, issueNumber int64, state string, seenAt time.Time) (bool, error)
}

type Mutator interface {
	CloseIssue(ctx context.Context, repository string, issueNumber int64) error
	Merge(ctx context.Context, repository string, number int64, expectedHeadSHA string) error
}

type Config struct {
	WIPLimits WIPLimits
}

type Engine struct {
	store   Store
	policy  Policy
	mutator Mutator
	cfg     Config
	now     func() time.Time
}

func NewEngine(store Store, mutator Mutator, cfg Config) *Engine {
	return &Engine{store: store, mutator: mutator, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
}

func (e *Engine) Reconcile(ctx context.Context, instance Instance, facts Facts) (Instance, Decision, error) {
	if e == nil || e.store == nil {
		return Instance{}, Decision{}, fmt.Errorf("workflow store is required")
	}
	stored, err := e.store.UpsertWorkflow(ctx, instance)
	if err != nil {
		return Instance{}, Decision{}, fmt.Errorf("upsert workflow: %w", err)
	}
	dependenciesOK, err := e.store.DependenciesSatisfied(ctx, stored.ID)
	if err != nil {
		return stored, Decision{}, fmt.Errorf("check workflow dependencies: %w", err)
	}
	facts.DependenciesSatisfied = dependenciesOK
	gatePassed, err := e.store.ActionSucceeded(ctx, stored.ID, ActionMergeGate, stored.Revision)
	if err != nil {
		return stored, Decision{}, fmt.Errorf("check merge gate outcome: %w", err)
	}
	facts.TeamLeadGatePassed = gatePassed
	triageDecision, triageDecisionFound, err := e.store.LatestActionDecision(ctx, stored.ID, ActionPMTriage, stored.Revision)
	if err != nil {
		return stored, Decision{}, fmt.Errorf("load PM triage handoff: %w", err)
	}
	if triageDecisionFound {
		switch triageDecision {
		case "DONE":
			facts.IssueReady = true
		case "BLOCKED":
			facts.BlockingReason = "pm_triage_blocked"
		}
	}
	reviewDecision, reviewDecisionFound, err := e.store.LatestActionDecision(ctx, stored.ID, ActionReview, stored.Revision)
	if err != nil {
		return stored, Decision{}, fmt.Errorf("load independent review handoff: %w", err)
	}
	if reviewDecisionFound {
		switch reviewDecision {
		case "REQUEST_CHANGES":
			facts.Review = ReviewChangesRequested
			facts.ReviewedHeadSHA = stored.Revision
		case "APPROVE":
			if facts.Review != ReviewChangesRequested {
				facts.Review = ReviewApproved
				facts.ReviewedHeadSHA = stored.Revision
			}
		}
	}
	facts.ActionStatuses = map[ActionKind]domain.JobStatus{}
	for _, kind := range []ActionKind{ActionPMTriage, ActionImplement, ActionAddressReview, ActionReview, ActionCIRepair, ActionMergeGate, ActionEscalateBlocker, ActionBacklogRefill} {
		status, found, err := e.store.LatestActionStatus(ctx, stored.ID, kind, stored.Revision)
		if err != nil {
			return stored, Decision{}, fmt.Errorf("load %s action status: %w", kind, err)
		}
		if found {
			facts.ActionStatuses[kind] = status
		}
	}
	decision := e.policy.Decide(stored, facts)

	if decision.Action == nil {
		if stored.State == decision.TargetState && stored.BlockedReason == decision.BlockedReason {
			return stored, decision, nil
		}
		updated, err := e.store.ApplyDecision(ctx, stored.ID, decision, actorForDecision(decision), e.now())
		return updated, decision, err
	}
	if decision.Action.Mode == ActionMutation {
		if e.mutator == nil {
			return stored, decision, fmt.Errorf("workflow mutation executor is required for %s", decision.Action.Kind)
		}
		repository := metadataString(stored.Metadata, "repository_full_name")
		if repository == "" {
			return stored, decision, fmt.Errorf("repository_full_name metadata is required for mutation")
		}
		switch decision.Action.Kind {
		case ActionCloseIssue:
			issueNumber, err := parseIssueNumber(stored.Subject)
			if err != nil {
				return stored, decision, err
			}
			if err := e.mutator.CloseIssue(ctx, repository, issueNumber); err != nil {
				return stored, decision, fmt.Errorf("close completed issue: %w", err)
			}
		case ActionMergePullRequest:
			var prNumber int64
			if _, err := fmt.Sscan(decision.Action.Metadata["pull_request_number"], &prNumber); err != nil || prNumber <= 0 {
				return stored, decision, fmt.Errorf("valid pull request number is required for merge mutation")
			}
			head := decision.Action.Metadata["head_sha"]
			if head == "" {
				return stored, decision, fmt.Errorf("expected head sha is required for merge mutation")
			}
			if err := e.mutator.Merge(ctx, repository, prNumber, head); err != nil {
				return stored, decision, fmt.Errorf("merge pull request: %w", err)
			}
			if facts.IssueOpen && stored.Kind == KindIssue {
				issueNumber, err := parseIssueNumber(stored.Subject)
				if err == nil {
					_ = e.mutator.CloseIssue(ctx, repository, issueNumber)
				}
			}
		default:
			return stored, decision, fmt.Errorf("unsupported workflow mutation %s", decision.Action.Kind)
		}
		updated, err := e.store.ApplyDecision(ctx, stored.ID, decision, decision.Action.Role, e.now())
		return updated, decision, err
	}

	limit := e.cfg.WIPLimits[decision.Action.Role]
	if limit > 0 {
		active, err := e.store.ActiveRoleCount(ctx, decision.Action.Role)
		if err != nil {
			return stored, decision, fmt.Errorf("count active role work: %w", err)
		}
		if active >= limit {
			return stored, decision, ErrWIPLimit
		}
	}
	job := buildJob(stored, *decision.Action, e.now())
	updated, _, err := e.store.DispatchAction(ctx, stored.ID, decision, job, decision.Action.Role, e.now())
	if err != nil {
		return stored, decision, err
	}
	return updated, decision, nil
}

func buildJob(instance Instance, action Action, now time.Time) JobSpec {
	seed := instance.ID + "\x00" + action.Key
	sum := sha256.Sum256([]byte(seed))
	digest := hex.EncodeToString(sum[:])
	metadata, _ := json.Marshal(map[string]any{
		"source":          "workflow_engine",
		"workflow_id":     instance.ID,
		"workflow_state":  action.TargetState,
		"workflow_action": action.Kind,
		"budget_kind":     action.Budget,
	})
	return JobSpec{
		ID:           "job_" + digest[:24],
		DedupeKey:    "workflow:" + digest,
		RepositoryID: instance.RepositoryID,
		Kind:         string(action.Kind),
		Role:         action.Role,
		Subject:      instance.Subject,
		Revision:     instance.Revision,
		Priority:     action.Priority,
		MaxAttempts:  action.MaxAttempts,
		AvailableAt:  now,
		Metadata:     metadata,
	}
}

func actorForDecision(decision Decision) domain.Role {
	if decision.Action != nil && decision.Action.Role != "" {
		return decision.Action.Role
	}
	switch decision.TargetState {
	case StateCompleted, StateBlocked, StateParked, StateReady, StatePlanning:
		return domain.RoleTeamLead
	case StateImplementing:
		return domain.RoleDev
	case StateReviewing:
		return domain.RoleReviewer
	case StateVerifying:
		return domain.RoleCIGuardian
	case StateMergeGating, StateMergeReady:
		return domain.RoleTeamLead
	default:
		return domain.RoleTeamLead
	}
}

func metadataString(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func parseIssueNumber(subject string) (int64, error) {
	var number int64
	if _, err := fmt.Sscan(subject, &number); err != nil || number <= 0 {
		return 0, fmt.Errorf("invalid issue subject %q", subject)
	}
	return number, nil
}
