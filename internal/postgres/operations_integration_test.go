package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

func TestConcurrentApplyMigrationsIsSerialized(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.ApplyMigrations(ctx); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent migration failed: %v", err)
	}
}

func TestOperationalStatsExposeQueueLeaseWorkflowAndWorkerHealth(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := store.CreateJob(ctx, NewJob{
		ID: "job-queued", DedupeKey: "job-queued", RepositoryID: repo.ID,
		Kind: "implementation", Role: domain.RoleDev, Subject: "1",
		Priority: 10, MaxAttempts: 2, AvailableAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateJob(ctx, NewJob{
		ID: "job-stale", DedupeKey: "job-stale", RepositoryID: repo.ID,
		Kind: "implementation", Role: domain.RoleDev, Subject: "2",
		Priority: 1000, MaxAttempts: 2, AvailableAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimJob(ctx, "stale-worker", now.Add(-10*time.Minute), time.Minute)
	if err != nil || !ok || claimed.ID != "job-stale" {
		t.Fatalf("claim stale job: job=%+v ok=%v err=%v", claimed, ok, err)
	}

	if _, _, err := store.PutEvent(ctx, InboxEvent{
		DedupeKey: "ops-event", Provider: "github", RepositoryID: repo.ID,
		Kind: "issue.changed", Subject: "3", Revision: "r1",
	}); err != nil {
		t.Fatal(err)
	}

	instance := workflow.NewInstance(repo.ID, workflow.KindIssue, "4", "r1", 100)
	stored, err := store.UpsertWorkflow(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyDecision(ctx, stored.ID, workflow.Decision{
		TargetState:   workflow.StateBlocked,
		BlockedReason: "external_dependency",
		Reason:        "test blocker",
	}, domain.RoleTeamLead, now); err != nil {
		t.Fatal(err)
	}

	if _, err := store.HeartbeatWorker(ctx, Worker{ID: "worker-live", Host: "host-a", Capacity: 1}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.HeartbeatWorker(ctx, Worker{ID: "worker-old", Host: "host-b", Capacity: 1}, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	stats, err := store.OperationalStats(ctx, now, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if stats.QueuedJobs != 1 || stats.ActiveJobs != 1 || stats.StaleJobLeases != 1 {
		t.Fatalf("unexpected job stats: %+v", stats)
	}
	if stats.PendingEvents != 1 || stats.BlockedWorkflows != 1 {
		t.Fatalf("unexpected event/workflow stats: %+v", stats)
	}
	if stats.LiveWorkers != 1 || stats.StaleWorkers != 1 {
		t.Fatalf("unexpected worker stats: %+v", stats)
	}

	_ = fmt.Sprintf("%+v", stats)
}

func TestOperationalStatsExposeDurableAutonomyHealth(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	stuck := workflow.NewInstance(repo.ID, workflow.KindIssue, "health-stuck", "head-a", 100)
	stuck, err := store.UpsertWorkflow(ctx, stuck)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE workflow_instances SET state = 'implementing', updated_at = $2 WHERE id = $1`, stuck.ID, now.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}

	repair := workflow.NewInstance(repo.ID, workflow.KindIssue, "health-repair", "head-b", 100)
	repair, err = store.UpsertWorkflow(ctx, repair)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE workflow_instances SET state = 'verifying', ci_repair_attempts = 1, updated_at = $2 WHERE id = $1`, repair.ID, now); err != nil {
		t.Fatal(err)
	}

	exhausted := workflow.NewInstance(repo.ID, workflow.KindIssue, "health-exhausted", "head-c", 100)
	exhausted, err = store.UpsertWorkflow(ctx, exhausted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE workflow_instances SET state = 'parked', ci_repair_attempts = ci_repair_limit, updated_at = $2 WHERE id = $1`, exhausted.ID, now); err != nil {
		t.Fatal(err)
	}

	completed := workflow.NewInstance(repo.ID, workflow.KindIssue, "health-completed", "head-d", 100)
	completed, err = store.UpsertWorkflow(ctx, completed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO workflow_history (workflow_id, from_state, to_state, actor_role, reason, created_at) VALUES ($1, 'merge_gating', 'completed', 'team_lead', 'merged', $2)`, completed.ID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO workflow_history (workflow_id, from_state, to_state, actor_role, reason, created_at) VALUES ($1, 'blocked', 'ready', 'team_lead', 'dependency changed', $2)`, repair.ID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO workflow_actions (id, workflow_id, action_key, kind, role, mode, target_state, status, created_at, completed_at) VALUES ('health-action-complete', $1, 'health-action-complete', 'verify', 'reviewer', 'job', 'verifying', 'completed', $2, $2), ('health-action-pending', $1, 'health-action-pending', 'review', 'reviewer', 'job', 'reviewing', 'pending', $2, NULL)`, repair.ID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	stats, err := store.OperationalStats(ctx, now, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if stats.StuckWorkflows != 1 || stats.RepairingWorkflows != 1 || stats.ExhaustedRepairBudgets != 1 {
		t.Fatalf("unexpected autonomy workflow health: %+v", stats)
	}
	if stats.CompletedWorkflows24h != 1 || stats.RecoveredWorkflows24h != 1 {
		t.Fatalf("unexpected autonomy transition health: %+v", stats)
	}
	if stats.WorkflowActions24h != 2 || stats.CompletedActions24h != 1 || stats.UsefulWorkRatio24h != 0.5 {
		t.Fatalf("unexpected autonomy useful-work health: %+v", stats)
	}
}
