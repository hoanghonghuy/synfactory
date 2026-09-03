package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	runtimefactory "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/verifier"
	"github.com/hoanghonghuy/synfactory/internal/workspace"
)

type Store interface {
	ClaimJob(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) (domain.Job, bool, error)
	StartJob(ctx context.Context, jobID, workerID string, now time.Time) (domain.Job, error)
	RenewLease(ctx context.Context, jobID, workerID string, now time.Time, leaseDuration time.Duration) (domain.Job, error)
	SucceedJob(ctx context.Context, jobID, workerID string, now time.Time) (domain.Job, error)
	FailJob(ctx context.Context, jobID, workerID string, now, retryAt time.Time, message string) (domain.Job, error)
	GetRepository(ctx context.Context, id string) (postgres.Repository, error)
	CreateRun(ctx context.Context, run postgres.Run) (postgres.Run, error)
	FinishRun(ctx context.Context, runID, status string, finishedAt time.Time, exitCode *int, summary, sessionID string) (postgres.Run, error)
	AddEvidence(ctx context.Context, evidence postgres.Evidence) (postgres.Evidence, error)
	HeartbeatWorker(ctx context.Context, worker postgres.Worker, at time.Time) (postgres.Worker, error)
}

type RuntimeEngine interface {
	Execute(ctx context.Context, request runtimefactory.Request, observer runtimefactory.Observer) (runtimefactory.Result, []runtimefactory.Attempt, error)
}

type RequestBuilder interface {
	Build(ctx context.Context, job domain.Job, repository postgres.Repository) (runtimefactory.Request, error)
}

type VerificationPlanner interface {
	Plan(ctx context.Context, job domain.Job, repository postgres.Repository, request runtimefactory.Request) (verifier.Plan, error)
}

type Config struct {
	ID                string
	Host              string
	Capacity          int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	DefaultTimeout    time.Duration
	RetryBase         time.Duration
}

type Worker struct {
	store      Store
	engine     RuntimeEngine
	builder    RequestBuilder
	workspaces workspace.Manager
	verifier   *verifier.Verifier
	planner    VerificationPlanner
	cfg        Config
	now        func() time.Time
}

func New(store Store, engine RuntimeEngine, builder RequestBuilder, cfg Config) *Worker {
	if cfg.ID == "" {
		cfg.ID = "worker"
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 1
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 2 * time.Minute
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = 30 * time.Second
	}
	return &Worker{store: store, engine: engine, builder: builder, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
}

func (w *Worker) WithExecution(workspaces workspace.Manager, verification *verifier.Verifier, planner VerificationPlanner) *Worker {
	w.workspaces = workspaces
	w.verifier = verification
	w.planner = planner
	return w
}

func (w *Worker) Run(ctx context.Context) error {
	if w.store == nil || w.engine == nil || w.builder == nil {
		return errors.New("worker store, runtime engine and request builder are required")
	}
	go w.heartbeatLoop(ctx)
	for {
		worked, err := w.RunOne(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("worker job failed", "worker", w.cfg.ID, "error", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if worked {
			continue
		}
		timer := time.NewTimer(w.cfg.PollInterval)
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

func (w *Worker) RunOne(ctx context.Context) (bool, error) {
	now := w.now()
	claimed, ok, err := w.store.ClaimJob(ctx, w.cfg.ID, now, w.cfg.LeaseDuration)
	if err != nil || !ok {
		return ok, err
	}
	job, err := w.store.StartJob(ctx, claimed.ID, w.cfg.ID, w.now())
	if err != nil {
		return true, fmt.Errorf("start claimed job %s: %w", claimed.ID, err)
	}
	repository, err := w.store.GetRepository(ctx, job.RepositoryID)
	if err != nil {
		return true, w.failJob(ctx, job, fmt.Errorf("load repository: %w", err))
	}
	request, err := w.builder.Build(ctx, job, repository)
	if err != nil {
		return true, w.failJob(ctx, job, fmt.Errorf("build runtime request: %w", err))
	}
	request.RunID = job.ID
	request.Role = string(job.Role)
	request.Repository = repository.FullName
	if request.Timeout <= 0 {
		request.Timeout = w.cfg.DefaultTimeout
	}
	if request.Metadata == nil {
		request.Metadata = map[string]string{}
	}
	request.Metadata["job_id"] = job.ID
	request.Metadata["job_attempt"] = strconv.Itoa(job.Attempt)
	request.Metadata["job_revision"] = job.Revision

	var handle workspace.Handle
	if w.workspaces != nil {
		handle, err = w.workspaces.Acquire(ctx, workspace.Spec{
			ID: job.ID + "-" + strconv.Itoa(job.Attempt), SourcePath: request.Workspace,
			Revision: job.Revision, Branch: request.Metadata["task_branch"],
			Mode: workspaceMode(request.Metadata["workspace_mode"]), Access: workspace.AccessForPermissions(request.Permissions),
			ContainerImage: request.Metadata["container_image"], NetworkAllowed: request.Metadata["network_allowed"] == "true",
			Memory: request.Metadata["container_memory"], CPUs: request.Metadata["container_cpus"],
		})
		if err != nil {
			return true, w.failJob(ctx, job, fmt.Errorf("acquire workspace: %w", err))
		}
		request.Workspace = handle.Path
		request.Sandbox = handle.Sandbox
	}

	runCtx, cancel := context.WithCancel(ctx)
	leaseErr := make(chan error, 1)
	go w.keepLease(runCtx, job.ID, cancel, leaseErr)
	observer := &runObserver{store: w.store, job: job, now: w.now}
	result, attempts, runErr := w.engine.Execute(runCtx, request, observer)
	cancel()
	select {
	case err := <-leaseErr:
		if err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("job lease lost: %w", err))
		}
	default:
	}
	if ctx.Err() != nil {
		if handle.Path != "" {
			_ = w.workspaces.Release(context.Background(), handle)
		}
		return true, ctx.Err()
	}

	if handle.Path != "" {
		if err := w.workspaces.Validate(ctx, handle); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}
	if runErr == nil && result.Outcome == runtimefactory.OutcomeSucceeded && w.verifier != nil && w.planner != nil {
		plan, planErr := w.planner.Plan(ctx, job, repository, request)
		if planErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("build verification plan: %w", planErr))
		} else {
			report, verifyErr := w.verifier.Verify(ctx, handle, plan)
			if len(attempts) > 0 {
				metadata, _ := json.Marshal(report)
				last := attempts[len(attempts)-1]
				_, evidenceErr := w.store.AddEvidence(ctx, postgres.Evidence{RunID: runID(job.ID, job.Attempt, last.Sequence), Kind: "verification", Name: "deterministic-verification", SHA256: report.SHA256, Metadata: metadata})
				if evidenceErr != nil {
					runErr = errors.Join(runErr, fmt.Errorf("persist verification evidence: %w", evidenceErr))
				}
			}
			if verifyErr != nil {
				runErr = errors.Join(runErr, verifyErr)
			}
		}
	}
	if handle.Path != "" {
		if err := w.workspaces.Release(ctx, handle); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("release workspace: %w", err))
		}
	}
	if runErr == nil && result.Outcome == runtimefactory.OutcomeSucceeded {
		if _, err := w.store.SucceedJob(ctx, job.ID, w.cfg.ID, w.now()); err != nil {
			return true, fmt.Errorf("complete job %s: %w", job.ID, err)
		}
		return true, nil
	}
	if runErr == nil {
		runErr = fmt.Errorf("runtime ended with outcome %s", result.Outcome)
	}
	return true, w.failJob(ctx, job, runErr)
}

func workspaceMode(value string) workspace.Mode {
	if workspace.Mode(value) == workspace.ModeDocker {
		return workspace.ModeDocker
	}
	return workspace.ModeWorktree
}

func (w *Worker) keepLease(ctx context.Context, jobID string, cancel context.CancelFunc, result chan<- error) {
	interval := w.cfg.LeaseDuration / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.store.RenewLease(ctx, jobID, w.cfg.ID, w.now(), w.cfg.LeaseDuration); err != nil {
				select {
				case result <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}
func (w *Worker) failJob(ctx context.Context, job domain.Job, runErr error) error {
	now := w.now()
	retryAt := now.Add(retryDelay(w.cfg.RetryBase, job.Attempt))
	_, err := w.store.FailJob(ctx, job.ID, w.cfg.ID, now, retryAt, runErr.Error())
	if err != nil {
		return errors.Join(runErr, fmt.Errorf("persist job failure: %w", err))
	}
	return runErr
}
func (w *Worker) heartbeatLoop(ctx context.Context) {
	worker := postgres.Worker{ID: w.cfg.ID, Host: w.cfg.Host, Capacity: w.cfg.Capacity}
	for {
		if _, err := w.store.HeartbeatWorker(ctx, worker, w.now()); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("worker heartbeat failed", "worker", w.cfg.ID, "error", err)
		}
		timer := time.NewTimer(w.cfg.HeartbeatInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}
func retryDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 30 * time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	return base * time.Duration(1<<shift)
}

type runObserver struct {
	store Store
	job   domain.Job
	now   func() time.Time
}

func (o *runObserver) AttemptStarted(ctx context.Context, attempt runtimefactory.Attempt) error {
	metadata, _ := json.Marshal(map[string]any{"failure_class": attempt.FailureClass})
	_, err := o.store.CreateRun(ctx, postgres.Run{ID: runID(o.job.ID, o.job.Attempt, attempt.Sequence), JobID: o.job.ID, Attempt: o.job.Attempt, Sequence: attempt.Sequence, Runtime: attempt.Runtime, Model: attempt.Model, Status: "running", Metadata: metadata})
	return err
}
func (o *runObserver) AttemptFinished(ctx context.Context, attempt runtimefactory.Attempt) error {
	status := runStatus(attempt.Result.Outcome, attempt.Err)
	exitCode := attempt.Result.ExitCode
	summary := attempt.Result.Summary
	if summary == "" && attempt.Err != nil {
		summary = attempt.Err.Error()
	}
	runID := runID(o.job.ID, o.job.Attempt, attempt.Sequence)
	if _, err := o.store.FinishRun(ctx, runID, status, o.now(), &exitCode, summary, attempt.Result.SessionID); err != nil {
		return err
	}
	evidenceMetadata, _ := json.Marshal(map[string]any{"outcome": attempt.Result.Outcome, "failure_class": attempt.FailureClass, "output": attempt.Result.Output, "diagnostics": attempt.Result.Diagnostics, "events": attempt.Result.Events})
	_, err := o.store.AddEvidence(ctx, postgres.Evidence{RunID: runID, Kind: "runtime", Name: "normalized-runtime-output", Metadata: evidenceMetadata})
	return err
}
func runStatus(outcome runtimefactory.Outcome, err error) string {
	if err != nil {
		switch runtimefactory.ClassifyFailure(err) {
		case runtimefactory.FailureTimeout:
			return "timed_out"
		case runtimefactory.FailureCanceled:
			return "cancelled"
		default:
			return "failed"
		}
	}
	if outcome == runtimefactory.OutcomeSucceeded {
		return "succeeded"
	}
	return "failed"
}
func runID(jobID string, attempt, sequence int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", jobID, attempt, sequence)))
	return "run_" + hex.EncodeToString(sum[:12])
}
