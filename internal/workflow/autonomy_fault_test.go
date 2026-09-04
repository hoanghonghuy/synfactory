package workflow

import (
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

func TestAutonomyFaultMatrixPreservesBoundedProgress(t *testing.T) {
	base := NewInstance("repo", KindIssue, "42", "head-2", 100)
	base.State = StateReviewing
	base.CIRepairLimit = 2
	base.ReviewRepairLimit = 2

	tests := []struct {
		name          string
		instance      Instance
		facts         Facts
		wantState     State
		wantAction    ActionKind
		wantRole      domain.Role
		wantBlockedBy string
	}{
		{
			name:     "stale review head forces fresh independent review",
			instance: base,
			facts: Facts{
				IssueOpen: true, IssueReady: true, HasImplementationPR: true,
				PullRequestNumber: 7, HeadSHA: "head-2", Review: ReviewApproved,
				ReviewedHeadSHA: "head-1", CI: CIPassing, DependenciesSatisfied: true,
			},
			wantState: StateReviewing, wantAction: ActionReview, wantRole: domain.RoleReviewer,
		},
		{
			name: "failing ci consumes bounded guardian capacity",
			instance: func() Instance {
				i := base
				i.CIRepairAttempts = 1
				return i
			}(),
			facts: Facts{
				IssueOpen: true, IssueReady: true, HasImplementationPR: true,
				PullRequestNumber: 7, HeadSHA: "head-2", Review: ReviewApproved,
				ReviewedHeadSHA: "head-2", CI: CIFailing, DependenciesSatisfied: true,
			},
			wantState: StateVerifying, wantAction: ActionCIRepair, wantRole: domain.RoleCIGuardian,
		},
		{
			name: "exhausted ci repair escalates instead of looping",
			instance: func() Instance {
				i := base
				i.CIRepairAttempts = i.CIRepairLimit
				return i
			}(),
			facts: Facts{
				IssueOpen: true, IssueReady: true, HasImplementationPR: true,
				PullRequestNumber: 7, HeadSHA: "head-2", Review: ReviewApproved,
				ReviewedHeadSHA: "head-2", CI: CIFailing, DependenciesSatisfied: true,
			},
			wantState: StateBlocked, wantAction: ActionEscalateBlocker, wantRole: domain.RoleTeamLead, wantBlockedBy: "ci_repair_budget_exhausted",
		},
		{
			name: "material dependency parks work without consuming developer capacity",
			instance: base,
			facts: Facts{
				IssueOpen: true, IssueReady: true, HasImplementationPR: true,
				PullRequestNumber: 7, HeadSHA: "head-2", DependenciesSatisfied: false,
			},
			wantState: StateBlocked, wantBlockedBy: "dependency_blocked",
		},
		{
			name:     "green exact head still requires team lead gate",
			instance: base,
			facts: Facts{
				IssueOpen: true, IssueReady: true, HasImplementationPR: true,
				PullRequestNumber: 7, HeadSHA: "head-2", Review: ReviewApproved,
				ReviewedHeadSHA: "head-2", CI: CIPassing, DependenciesSatisfied: true,
			},
			wantState: StateMergeGating, wantAction: ActionMergeGate, wantRole: domain.RoleTeamLead,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := (Policy{}).Decide(tt.instance, tt.facts)
			if decision.TargetState != tt.wantState {
				t.Fatalf("state = %s, want %s: %+v", decision.TargetState, tt.wantState, decision)
			}
			if decision.BlockedReason != tt.wantBlockedBy {
				t.Fatalf("blocked reason = %q, want %q: %+v", decision.BlockedReason, tt.wantBlockedBy, decision)
			}
			if tt.wantAction == "" {
				if decision.Action != nil {
					t.Fatalf("unexpected action: %+v", decision.Action)
				}
				return
			}
			if decision.Action == nil || decision.Action.Kind != tt.wantAction || decision.Action.Role != tt.wantRole {
				t.Fatalf("action = %+v, want kind=%s role=%s", decision.Action, tt.wantAction, tt.wantRole)
			}
		})
	}
}

func TestAutonomySelectionKeepsIndependentRoleCapacityUseful(t *testing.T) {
	developer := Candidate{Instance: Instance{Priority: 300}, Decision: Decision{Action: &Action{Mode: ActionJob, Role: domain.RoleDev}}}
	ciGuardian := Candidate{Instance: Instance{Priority: 250}, Decision: Decision{Action: &Action{Mode: ActionJob, Role: domain.RoleCIGuardian}}}
	reviewer := Candidate{Instance: Instance{Priority: 200}, Decision: Decision{Action: &Action{Mode: ActionJob, Role: domain.RoleReviewer}}}

	selected := SelectRunnable(
		[]Candidate{developer, ciGuardian, reviewer},
		map[domain.Role]int{domain.RoleDev: 1, domain.RoleCIGuardian: 1},
		WIPLimits{domain.RoleDev: 1, domain.RoleCIGuardian: 1, domain.RoleReviewer: 1},
	)
	if len(selected) != 1 || selected[0].Decision.Action == nil || selected[0].Decision.Action.Role != domain.RoleReviewer {
		t.Fatalf("independent reviewer capacity was starved: %+v", selected)
	}
}
