package workflow

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

type Kind string

const (
	KindIssue      Kind = "issue"
	KindRepository Kind = "repository"
)

type State string

const (
	StateDiscovered   State = "discovered"
	StatePlanning     State = "planning"
	StateReady        State = "ready"
	StateImplementing State = "implementing"
	StateReviewing    State = "reviewing"
	StateVerifying    State = "verifying"
	StateMergeGating  State = "merge_gating"
	StateMergeReady   State = "merge_ready"
	StateBlocked      State = "blocked"
	StateParked       State = "parked"
	StateCompleted    State = "completed"
)

type ReviewState string

const (
	ReviewPending          ReviewState = "pending"
	ReviewApproved         ReviewState = "approved"
	ReviewChangesRequested ReviewState = "changes_requested"
)

type CIState string

const (
	CIUnknown CIState = "unknown"
	CIPending CIState = "pending"
	CIPassing CIState = "passing"
	CIFailing CIState = "failing"
)

type ActionKind string

const (
	ActionPMTriage         ActionKind = "pm_triage"
	ActionImplement        ActionKind = "implement"
	ActionAddressReview    ActionKind = "address_review"
	ActionReview           ActionKind = "review"
	ActionCIRepair         ActionKind = "ci_repair"
	ActionMergeGate        ActionKind = "merge_gate"
	ActionMergePullRequest ActionKind = "merge_pull_request"
	ActionCloseIssue       ActionKind = "close_issue"
	ActionEscalateBlocker  ActionKind = "escalate_blocker"
	ActionBacklogRefill    ActionKind = "backlog_refill"
)

type ActionMode string

const (
	ActionJob      ActionMode = "job"
	ActionMutation ActionMode = "mutation"
)

type BudgetKind string

const (
	BudgetCIRepair     BudgetKind = "ci_repair"
	BudgetReviewRepair BudgetKind = "review_repair"
)

type Instance struct {
	ID                   string
	DedupeKey            string
	RepositoryID         string
	Kind                 Kind
	Subject              string
	Revision             string
	State                State
	Priority             int
	BlockedReason        string
	CIRepairAttempts     int
	CIRepairLimit        int
	ReviewRepairAttempts int
	ReviewRepairLimit    int
	LastDispatchedAt     *time.Time
	Metadata             json.RawMessage
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Facts struct {
	IssueOpen             bool
	IssueReady            bool
	HasImplementationPR   bool
	PullRequestNumber     int64
	PRMerged              bool
	HeadSHA               string
	Review                ReviewState
	ReviewedHeadSHA       string
	CI                    CIState
	BlockingReason        string
	DependenciesSatisfied bool
	TeamLeadGatePassed    bool
	ActionStatuses        map[ActionKind]domain.JobStatus
}

type Action struct {
	Key         string
	Kind        ActionKind
	Mode        ActionMode
	Role        domain.Role
	TargetState State
	Budget      BudgetKind
	Priority    int
	MaxAttempts int
	Metadata    map[string]string
}

type Decision struct {
	TargetState   State
	Reason        string
	BlockedReason string
	Action        *Action
}

type JobSpec struct {
	ID           string
	DedupeKey    string
	RepositoryID string
	Kind         string
	Role         domain.Role
	Subject      string
	Revision     string
	Priority     int
	MaxAttempts  int
	AvailableAt  time.Time
	Metadata     json.RawMessage
}

type DispatchResult struct {
	JobID      string
	Dispatched bool
}

var (
	ErrInvalidTransition = errors.New("invalid workflow state transition")
	ErrUnauthorizedActor = errors.New("role is not authorized for workflow transition")
	ErrWIPLimit          = errors.New("workflow role WIP limit reached")
	ErrDependencyBlocked = errors.New("workflow dependency is not satisfied")
)
