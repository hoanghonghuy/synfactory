package workflow

import (
	"fmt"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

type Policy struct{}

func (Policy) Decide(instance Instance, facts Facts) Decision {
	if instance.Kind == KindRepository {
		return decideRepository(instance, facts)
	}
	return decideIssue(instance, facts)
}

func decideRepository(instance Instance, facts Facts) Decision {
	status, found := actionStatus(facts, ActionBacklogRefill)
	if found {
		if actionActive(status) {
			return Decision{TargetState: StatePlanning, Reason: "backlog refill is active"}
		}
		if status == domain.JobSucceeded {
			return Decision{TargetState: StateCompleted, Reason: "backlog refill completed"}
		}
		if status == domain.JobFailed || status == domain.JobCancelled {
			return Decision{TargetState: StateParked, BlockedReason: "backlog_refill_failed", Reason: "backlog refill exhausted its job budget"}
		}
	}
	return Decision{TargetState: StatePlanning, Reason: "inspect repository backlog", Action: jobAction(instance, ActionBacklogRefill, domain.RolePM, StatePlanning, "", 50, 2, 0)}
}

func decideIssue(instance Instance, facts Facts) Decision {
	if facts.PRMerged {
		if facts.IssueOpen {
			return Decision{TargetState: StateCompleted, Reason: "implementation merged; close linked issue", Action: mutationAction(instance, ActionCloseIssue, domain.RoleTeamLead, StateCompleted, nil)}
		}
		return Decision{TargetState: StateCompleted, Reason: "implementation merged and issue closed"}
	}
	if !facts.IssueOpen && !facts.HasImplementationPR {
		return Decision{TargetState: StateCompleted, Reason: "issue no longer open"}
	}
	if !facts.DependenciesSatisfied {
		return Decision{TargetState: StateBlocked, BlockedReason: "dependency_blocked", Reason: "workflow dependency is not complete"}
	}
	if facts.BlockingReason != "" {
		return blockedDecision(instance, facts, facts.BlockingReason)
	}
	if !facts.IssueReady {
		if d, ok := terminalFailureDecision(instance, facts, ActionPMTriage, "pm_triage_failed"); ok {
			return d
		}
		if actionIsActive(facts, ActionPMTriage) {
			return Decision{TargetState: StatePlanning, Reason: "PM triage is active"}
		}
		return Decision{TargetState: StatePlanning, Reason: "issue requires product triage", Action: jobAction(instance, ActionPMTriage, domain.RolePM, StatePlanning, "", 120, 2, 0)}
	}
	if !facts.HasImplementationPR {
		if d, ok := terminalFailureDecision(instance, facts, ActionImplement, "implementation_failed"); ok {
			return d
		}
		if actionSucceeded(facts, ActionImplement) {
			return blockedDecision(instance, facts, "implementation_completed_without_pr")
		}
		if actionIsActive(facts, ActionImplement) {
			return Decision{TargetState: StateImplementing, Reason: "developer implementation is active"}
		}
		return Decision{TargetState: StateImplementing, Reason: "implementation required", Action: jobAction(instance, ActionImplement, domain.RoleDev, StateImplementing, "", 130, 3, 0)}
	}
	if facts.Review == ReviewChangesRequested && facts.ReviewedHeadSHA == instance.Revision {
		if instance.ReviewRepairAttempts >= instance.ReviewRepairLimit {
			return blockedDecision(instance, facts, "review_repair_budget_exhausted")
		}
		if d, ok := terminalFailureDecision(instance, facts, ActionAddressReview, "review_repair_failed"); ok {
			return d
		}
		if actionIsActive(facts, ActionAddressReview) {
			return Decision{TargetState: StateImplementing, Reason: "review repair is active"}
		}
		return Decision{TargetState: StateImplementing, Reason: "review changes requested", Action: jobAction(instance, ActionAddressReview, domain.RoleDev, StateImplementing, BudgetReviewRepair, 140, 2, instance.ReviewRepairAttempts+1)}
	}
	if facts.Review != ReviewApproved || facts.ReviewedHeadSHA != instance.Revision {
		if d, ok := terminalFailureDecision(instance, facts, ActionReview, "review_failed"); ok {
			return d
		}
		if actionIsActive(facts, ActionReview) {
			return Decision{TargetState: StateReviewing, Reason: "independent review is active"}
		}
		return Decision{TargetState: StateReviewing, Reason: "exact-head independent review required", Action: jobAction(instance, ActionReview, domain.RoleReviewer, StateReviewing, "", 145, 2, 0)}
	}
	switch facts.CI {
	case CIUnknown, CIPending:
		return Decision{TargetState: StateVerifying, Reason: "waiting for CI truth"}
	case CIFailing:
		if instance.CIRepairAttempts >= instance.CIRepairLimit {
			return blockedDecision(instance, facts, "ci_repair_budget_exhausted")
		}
		if d, ok := terminalFailureDecision(instance, facts, ActionCIRepair, "ci_repair_failed"); ok {
			return d
		}
		if actionIsActive(facts, ActionCIRepair) {
			return Decision{TargetState: StateVerifying, Reason: "CI repair is active"}
		}
		return Decision{TargetState: StateVerifying, Reason: "CI is failing", Action: jobAction(instance, ActionCIRepair, domain.RoleCIGuardian, StateVerifying, BudgetCIRepair, 150, 2, instance.CIRepairAttempts+1)}
	case CIPassing:
	}
	if !facts.TeamLeadGatePassed {
		if d, ok := terminalFailureDecision(instance, facts, ActionMergeGate, "merge_gate_failed"); ok {
			return d
		}
		if actionIsActive(facts, ActionMergeGate) {
			return Decision{TargetState: StateMergeGating, Reason: "team lead merge gate is active"}
		}
		return Decision{TargetState: StateMergeGating, Reason: "exact-head merge authorization required", Action: jobAction(instance, ActionMergeGate, domain.RoleTeamLead, StateMergeGating, "", 155, 1, 0)}
	}
	metadata := map[string]string{"pull_request_number": fmt.Sprintf("%d", facts.PullRequestNumber), "head_sha": instance.Revision}
	return Decision{TargetState: StateCompleted, Reason: "review, CI and team lead gate passed", Action: mutationAction(instance, ActionMergePullRequest, domain.RoleTeamLead, StateCompleted, metadata)}
}

func blockedDecision(instance Instance, facts Facts, reason string) Decision {
	if actionSucceeded(facts, ActionEscalateBlocker) || actionIsActive(facts, ActionEscalateBlocker) {
		return Decision{TargetState: StateBlocked, BlockedReason: reason, Reason: "workflow remains blocked after escalation"}
	}
	if status, found := actionStatus(facts, ActionEscalateBlocker); found && (status == domain.JobFailed || status == domain.JobCancelled) {
		return Decision{TargetState: StateParked, BlockedReason: reason, Reason: "blocker escalation exhausted its job budget"}
	}
	return Decision{TargetState: StateBlocked, BlockedReason: reason, Reason: "workflow blocked; record escalation without consuming other capacity", Action: jobAction(instance, ActionEscalateBlocker, domain.RoleTeamLead, StateBlocked, "", 80, 1, 0)}
}

func terminalFailureDecision(instance Instance, facts Facts, kind ActionKind, reason string) (Decision, bool) {
	status, found := actionStatus(facts, kind)
	if !found || (status != domain.JobFailed && status != domain.JobCancelled) {
		return Decision{}, false
	}
	return blockedDecision(instance, facts, reason), true
}

func jobAction(instance Instance, kind ActionKind, role domain.Role, target State, budget BudgetKind, priority, maxAttempts, cycle int) *Action {
	key := fmt.Sprintf("%s:%s:%s:%d", instance.ID, kind, instance.Revision, cycle)
	return &Action{Key: key, Kind: kind, Mode: ActionJob, Role: role, TargetState: target, Budget: budget, Priority: priority, MaxAttempts: maxAttempts, Metadata: map[string]string{"revision": instance.Revision}}
}

func mutationAction(instance Instance, kind ActionKind, role domain.Role, target State, metadata map[string]string) *Action {
	return &Action{Key: fmt.Sprintf("%s:%s:%s", instance.ID, kind, instance.Revision), Kind: kind, Mode: ActionMutation, Role: role, TargetState: target, MaxAttempts: 1, Metadata: metadata}
}

func actionStatus(facts Facts, kind ActionKind) (domain.JobStatus, bool) {
	if facts.ActionStatuses == nil {
		return "", false
	}
	status, ok := facts.ActionStatuses[kind]
	return status, ok
}
func actionIsActive(facts Facts, kind ActionKind) bool {
	status, ok := actionStatus(facts, kind)
	return ok && actionActive(status)
}
func actionActive(status domain.JobStatus) bool {
	return status == domain.JobQueued || status == domain.JobLeased || status == domain.JobRunning || status == domain.JobRetryWait
}
func actionSucceeded(facts Facts, kind ActionKind) bool {
	status, ok := actionStatus(facts, kind)
	return ok && status == domain.JobSucceeded
}
