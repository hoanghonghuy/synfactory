package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

func TestWorkflowDispatchIsIdempotentAndConsumesBudgetOnce(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	instance := workflow.NewInstance(repo.ID, workflow.KindIssue, "42", "sha-1", 120)
	stored, err := store.UpsertWorkflow(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}
	decision := workflow.Decision{
		TargetState: workflow.StateVerifying,
		Reason:      "CI failed",
		Action: &workflow.Action{
			Key:         stored.ID + ":ci:sha-1",
			Kind:        workflow.ActionCIRepair,
			Mode:        workflow.ActionJob,
			Role:        domain.RoleCIGuardian,
			TargetState: workflow.StateVerifying,
			Budget:      workflow.BudgetCIRepair,
			Priority:    140,
			MaxAttempts: 2,
		},
	}
	job := workflow.JobSpec{ID: "job-workflow-ci", DedupeKey: "workflow-ci-key", RepositoryID: repo.ID, Kind: "ci_repair", Role: domain.RoleCIGuardian, Subject: "42", Revision: "sha-1", Priority: 140, MaxAttempts: 2, AvailableAt: time.Now().Add(-time.Second)}
	first, dispatch, err := store.DispatchAction(ctx, stored.ID, decision, job, domain.RoleCIGuardian, time.Now().UTC())
	if err != nil || !dispatch.Dispatched {
		t.Fatalf("first dispatch: dispatched=%v err=%v", dispatch.Dispatched, err)
	}
	if first.CIRepairAttempts != 1 {
		t.Fatalf("repair attempts=%d", first.CIRepairAttempts)
	}
	second, dispatch, err := store.DispatchAction(ctx, stored.ID, decision, job, domain.RoleCIGuardian, time.Now().UTC())
	if err != nil || dispatch.Dispatched {
		t.Fatalf("duplicate dispatch: dispatched=%v err=%v", dispatch.Dispatched, err)
	}
	if second.CIRepairAttempts != 1 {
		t.Fatalf("duplicate consumed budget: %d", second.CIRepairAttempts)
	}
}

func TestWorkflowDependencyBlocksUntilCompleted(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	parent, err := store.UpsertWorkflow(ctx, workflow.NewInstance(repo.ID, workflow.KindIssue, "10", "a", 100))
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.UpsertWorkflow(ctx, workflow.NewInstance(repo.ID, workflow.KindIssue, "11", "b", 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddWorkflowDependency(ctx, child.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	ready, err := store.DependenciesSatisfied(ctx, child.ID)
	if err != nil || ready {
		t.Fatalf("dependency should block: ready=%v err=%v", ready, err)
	}
	completed, err := store.ApplyDecision(ctx, parent.ID, workflow.Decision{TargetState: workflow.StateCompleted, Reason: "merged"}, domain.RoleTeamLead, time.Now().UTC())
	if err != nil || completed.State != workflow.StateCompleted {
		t.Fatalf("complete parent: %+v err=%v", completed, err)
	}
	ready, err = store.DependenciesSatisfied(ctx, child.ID)
	if err != nil || !ready {
		t.Fatalf("dependency should be satisfied: ready=%v err=%v", ready, err)
	}
}

func TestWorkflowRejectsUnauthorizedCompletion(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	item, err := store.UpsertWorkflow(ctx, workflow.NewInstance(repo.ID, workflow.KindIssue, "12", "a", 100))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ApplyDecision(ctx, item.ID, workflow.Decision{TargetState: workflow.StateCompleted, Reason: "self merge"}, domain.RoleDev, time.Now().UTC())
	if !errors.Is(err, workflow.ErrUnauthorizedActor) {
		t.Fatalf("expected authorization failure, got %v", err)
	}
}

func TestTaskReservationIsExclusiveAndCanRecoverAfterExpiry(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	fingerprint := workflow.TaskFingerprint(repo.FullName, "feature", "exclusive feature scope")
	now := time.Now().UTC()
	reserved, err := store.ReserveTask(ctx, repo.ID, fingerprint, "pm-a", now, time.Minute)
	if err != nil || !reserved {
		t.Fatalf("first reservation: reserved=%v err=%v", reserved, err)
	}
	reserved, err = store.ReserveTask(ctx, repo.ID, fingerprint, "pm-b", now.Add(10*time.Second), time.Minute)
	if err != nil || reserved {
		t.Fatalf("concurrent duplicate reservation should fail: reserved=%v err=%v", reserved, err)
	}
	reserved, err = store.ReserveTask(ctx, repo.ID, fingerprint, "pm-b", now.Add(2*time.Minute), time.Minute)
	if err != nil || !reserved {
		t.Fatalf("expired reservation should recover: reserved=%v err=%v", reserved, err)
	}
	if err := store.BindTask(ctx, repo.ID, fingerprint, "pm-b", 77, "open", now.Add(2*time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	reserved, err = store.ReserveTask(ctx, repo.ID, fingerprint, "pm-c", now.Add(10*time.Minute), time.Minute)
	if err != nil || reserved {
		t.Fatalf("bound task must permanently dedupe equivalent work: reserved=%v err=%v", reserved, err)
	}
}

func TestNewRevisionReopensBlockedWorkflowAndResetsRepairBudgets(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	instance := workflow.NewInstance(repo.ID, workflow.KindIssue, "88", "head-old", 100)
	stored, err := store.UpsertWorkflow(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}
	stored, err = store.ApplyDecision(ctx, stored.ID, workflow.Decision{TargetState: workflow.StateBlocked, BlockedReason: "ci_repair_budget_exhausted", Reason: "budget"}, domain.RoleTeamLead, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE workflow_instances SET ci_repair_attempts = 2, review_repair_attempts = 2 WHERE id = $1`, stored.ID); err != nil {
		t.Fatal(err)
	}
	instance.Revision = "head-new"
	refreshed, err := store.UpsertWorkflow(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.State != workflow.StateDiscovered || refreshed.BlockedReason != "" || refreshed.CIRepairAttempts != 0 || refreshed.ReviewRepairAttempts != 0 {
		t.Fatalf("new revision did not reset blocked workflow: %+v", refreshed)
	}
}
