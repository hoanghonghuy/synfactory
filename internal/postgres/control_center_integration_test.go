package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

func TestControlCenterReadModels(t *testing.T) {
	store := openTestStore(t)
	repo := seedRepository(t, store)
	ctx := context.Background()
	now := time.Now().UTC()

	job, created, err := store.CreateJob(ctx, NewJob{
		ID: "control-center-job", DedupeKey: "control-center-job", RepositoryID: repo.ID,
		Kind: "implement", Role: domain.RoleDev, Subject: "8", Revision: "head-1",
		Priority: 120, MaxAttempts: 3, AvailableAt: now,
	})
	if err != nil || !created {
		t.Fatalf("create job: created=%v err=%v", created, err)
	}

	instance, err := store.UpsertWorkflow(ctx, workflow.NewInstance(repo.ID, workflow.KindIssue, "8", "head-1", 120))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyDecision(ctx, instance.ID, workflow.Decision{
		TargetState: workflow.StateBlocked, BlockedReason: "external_dependency", Reason: "waiting for dependency",
	}, domain.RoleTeamLead, now); err != nil {
		t.Fatal(err)
	}

	run, err := store.CreateRun(ctx, Run{
		ID: "control-center-run", JobID: job.ID, Attempt: 1, Sequence: 1,
		Runtime: "codex-primary", Model: "gpt-test", Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddEvidence(ctx, Evidence{RunID: run.ID, Kind: "verification", Name: "go-test", SHA256: "abc123"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.HeartbeatWorker(ctx, Worker{ID: "control-worker", Host: "host-a", Capacity: 2}, now); err != nil {
		t.Fatal(err)
	}

	jobs, err := store.ListJobs(ctx, JobFilter{RepositoryID: repo.ID, Page: Page{Limit: 10}})
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	workflows, err := store.ListWorkflows(ctx, WorkflowFilter{State: workflow.StateBlocked, Page: Page{Limit: 10}})
	if err != nil || len(workflows) != 1 || workflows[0].ID != instance.ID {
		t.Fatalf("workflows=%+v err=%v", workflows, err)
	}
	runs, err := store.ListRuns(ctx, RunFilter{JobID: job.ID, Page: Page{Limit: 10}})
	if err != nil || len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	evidence, err := store.ListEvidence(ctx, run.ID)
	if err != nil || len(evidence) != 1 || evidence[0].SHA256 != "abc123" {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
	workers, err := store.ListWorkers(ctx)
	if err != nil || len(workers) != 1 || workers[0].ID != "control-worker" {
		t.Fatalf("workers=%+v err=%v", workers, err)
	}
}

func TestControlCenterPageBounds(t *testing.T) {
	if page := normalizePage(Page{}); page.Limit != 50 || page.Offset != 0 {
		t.Fatalf("unexpected defaults: %+v", page)
	}
	if page := normalizePage(Page{Limit: 1000, Offset: -1}); page.Limit != 200 || page.Offset != 0 {
		t.Fatalf("unexpected bounds: %+v", page)
	}
}
