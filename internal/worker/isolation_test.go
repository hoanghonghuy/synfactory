package worker

import (
	"context"
	"testing"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	runtimefactory "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/verifier"
	"github.com/hoanghonghuy/synfactory/internal/workspace"
)

type fakeWorkspaceManager struct {
	path     string
	released bool
	validate error
}
func (m *fakeWorkspaceManager) Acquire(context.Context, workspace.Spec) (workspace.Handle, error) {
	return workspace.Handle{ID: "iso", Path: m.path, Revision: "rev", Access: workspace.AccessReadOnly, Sandbox: runtimefactory.SandboxSpec{Mode: runtimefactory.SandboxHost}}, nil
}
func (m *fakeWorkspaceManager) Validate(context.Context, workspace.Handle) error { return m.validate }
func (m *fakeWorkspaceManager) Release(context.Context, workspace.Handle) error { m.released = true; return nil }

type staticPlanner struct { plan verifier.Plan }
func (p staticPlanner) Plan(context.Context, domain.Job, postgres.Repository, runtimefactory.Request) (verifier.Plan, error) { return p.plan, nil }

func TestVerificationFailurePreventsJobSuccessAndReleasesWorkspace(t *testing.T) {
	store := &fakeStore{job: domain.Job{ID: "job-v", RepositoryID: "repo-1", Role: "developer", Revision: "abc"}, repository: postgres.Repository{ID: "repo-1", FullName: "owner/repo"}}
	attempt := runtimefactory.Attempt{Sequence: 1, Runtime: "codex", Result: runtimefactory.Result{Outcome: runtimefactory.OutcomeSucceeded, ExitCode: 0, Summary: "agent says done"}}
	manager := &fakeWorkspaceManager{path: t.TempDir()}
	verification := &verifier.Verifier{Supervisor: runtimefactory.NewSupervisor()}
	planner := staticPlanner{plan: verifier.Plan{Checks: []verifier.Check{{Name: "required-test", Command: "sh", Args: []string{"-c", "exit 9"}, Required: true, Timeout: time.Second}}}}
	w := New(store, fakeEngine{result: attempt.Result, attempts: []runtimefactory.Attempt{attempt}}, fakeBuilder{}, Config{ID: "w1", LeaseDuration: time.Minute}).WithExecution(manager, verification, planner)
	worked, err := w.RunOne(context.Background())
	if !worked || err == nil { t.Fatalf("expected verification failure, worked=%v err=%v", worked, err) }
	if store.succeeded || !store.failed { t.Fatalf("verification must gate success: success=%v failed=%v", store.succeeded, store.failed) }
	if !manager.released { t.Fatal("workspace was not released") }
	if len(store.evidence) != 2 || store.evidence[1].Kind != "verification" || store.evidence[1].SHA256 == "" { t.Fatalf("verification evidence missing: %+v", store.evidence) }
}
