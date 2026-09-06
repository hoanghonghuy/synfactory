package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/attention"
	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/controlcenter"
	"github.com/hoanghonghuy/synfactory/internal/domain"
	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	"github.com/hoanghonghuy/synfactory/internal/onboarding"
	"github.com/hoanghonghuy/synfactory/internal/operations"
	"github.com/hoanghonghuy/synfactory/internal/orchestrator"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	repositoryfactory "github.com/hoanghonghuy/synfactory/internal/repository"
	runtimefactory "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/verifier"
	workerfactory "github.com/hoanghonghuy/synfactory/internal/worker"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
	"github.com/hoanghonghuy/synfactory/internal/workspace"
)

type healthResponse struct {
	Status string `json:"status"`
	Mode   string `json:"mode,omitempty"`
}

type wakeBus struct {
	events    chan struct{}
	workflows chan struct{}
}

func newWakeBus() *wakeBus {
	return &wakeBus{events: make(chan struct{}, 1), workflows: make(chan struct{}, 1)}
}

func (b *wakeBus) event() {
	if b != nil {
		signalChannel(b.events)
	}
}

func (b *wakeBus) workflow() {
	if b != nil {
		signalChannel(b.workflows)
	}
}

func (b *wakeBus) all() {
	b.event()
	b.workflow()
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	configureLogging(cfg.LogLevel)

	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if len(os.Args) > 1 {
		mode = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}
	if !validMode(mode) {
		slog.Error("unsupported process mode", "mode", mode)
		os.Exit(2)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 20*time.Second)
	store, err := postgres.Open(startupCtx, cfg.DatabaseURL, postgres.Options{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxIdle:     cfg.DBConnMaxIdle,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
	})
	cancelStartup()
	if err != nil {
		slog.Error("connect postgres", "error", err)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	if mode == "check" {
		checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.Ping(checkCtx); err != nil {
			slog.Error("database check failed", "error", err)
			os.Exit(1)
		}
		return
	}

	migrationCtx, cancelMigration := context.WithTimeout(context.Background(), 2*time.Minute)
	err = store.ApplyMigrations(migrationCtx)
	cancelMigration()
	if err != nil {
		slog.Error("apply migrations", "error", err)
		os.Exit(1)
	}
	if mode == "migrate" {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bus := newWakeBus()
	var runErr error
	switch mode {
	case "api":
		runErr = runAPI(ctx, cfg, store, nil)
	case "scheduler":
		runErr = runScheduler(ctx, cfg, store, bus)
	case "worker":
		runErr = runWorkers(ctx, cfg, store)
	case "all":
		runErr = runAll(ctx, cfg, store, bus)
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		slog.Error("synfactory process stopped", "mode", mode, "error", runErr)
		os.Exit(1)
	}
}

func validMode(mode string) bool {
	switch mode {
	case "api", "scheduler", "worker", "all", "migrate", "check":
		return true
	default:
		return false
	}
}

func runAll(ctx context.Context, cfg config.Config, store *postgres.Store, bus *wakeBus) error {
	return runComponents(ctx, []namedComponent{
		{name: "api", run: func(ctx context.Context) error { return runAPI(ctx, cfg, store, bus) }},
		{name: "scheduler", run: func(ctx context.Context) error { return runScheduler(ctx, cfg, store, bus) }},
		{name: "workers", run: func(ctx context.Context) error { return runWorkers(ctx, cfg, store) }},
	}, cfg.ShutdownTimeout)
}

func runAPI(ctx context.Context, cfg config.Config, store *postgres.Store, bus *wakeBus) error {
	wake := func() {}
	if bus != nil {
		wake = bus.all
	}
	metrics := operations.Handler{Store: store, WorkerStaleAfter: cfg.WorkerStaleAfter}
	authorizer := authz.HybridAuthorizer{
		Session: authz.SessionAuthorizer{Store: store},
		Legacy:  authz.LegacyTokenAuthorizer{Token: cfg.OperatorToken},
	}
	operatorAPI := controlcenter.AuthorizedHandler{
		Handler:    controlcenter.Handler{Store: store, Token: cfg.OperatorToken, WorkerStaleAfter: cfg.WorkerStaleAfter},
		Authorizer: authorizer,
	}
	attentionAPI := attention.HTTPHandler{
		Service: attention.Service{
			Store:       store,
			Revalidator: attention.WorkflowRevalidator{Store: store},
		},
		Query:      store,
		Token:      cfg.OperatorToken,
		Authorizer: authorizer,
	}
	githubClient, githubEnabled, err := configuredGitHubClient(cfg)
	if err != nil {
		return fmt.Errorf("configure github client for api: %w", err)
	}
	var onboardingGitHub onboarding.GitHub
	if githubEnabled {
		onboardingGitHub = githubClient
	}
	repositoryAPI := onboarding.Handler{Store: store, GitHub: onboardingGitHub, Token: cfg.OperatorToken, Authorizer: authorizer}
	terminalService, err := configureTerminal(cfg, authorizer)
	if err != nil {
		return fmt.Errorf("configure operator terminal: %w", err)
	}
	defer func() {
		if err := terminalService.shutdown(); err != nil {
			slog.Warn("terminal shutdown failed", "error", err)
		}
	}()
	go terminalService.runReaper(ctx)

	mux := http.NewServeMux()
	mux.Handle("/webhooks/github", githubfactory.NewWebhookHandler(cfg.GitHubWebhookSecret, store, wake))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Mode: "api"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := store.Ping(checkCtx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "not_ready", Mode: "api"})
			return
		}
		writeJSON(w, http.StatusOK, healthResponse{Status: "ready", Mode: "api"})
	})
	mux.HandleFunc("GET /ops", metrics.JSON)
	mux.HandleFunc("GET /metrics", metrics.Prometheus)
	operatorAPI.Register(mux)
	registerAuthAPI(mux, store, authorizer, cfg)
	attentionAPI.Register(mux)
	repositoryAPI.Register(mux)
	terminalService.register(mux)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("synfactory api listening", "addr", cfg.Addr)
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("api shutdown: %w", err)
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func runScheduler(ctx context.Context, cfg config.Config, store *postgres.Store, bus *wakeBus) error {
	if bus == nil {
		bus = newWakeBus()
	}
	hostname := hostName()
	eventStore := &orchestrator.WorkflowEventStore{Store: store, Wake: bus.workflow}
	eventProcessor := githubfactory.NewEventProcessor(
		eventStore,
		"event-router@"+hostname,
		cfg.EventPollInterval,
		cfg.EventLeaseDuration,
		cfg.EventMaxAttempts,
		bus.events,
	)

	components := []namedComponent{
		{name: "event processor", run: eventProcessor.Run},
		{name: "lease recovery", run: func(ctx context.Context) error {
			return runLeaseRecovery(ctx, store, cfg.LeaseRecoveryInterval)
		}},
	}
	if delivery, enabled := configuredAttentionDelivery(store); enabled {
		components = append(components, delivery)
	}
	githubClient, githubEnabled, err := configuredGitHubClient(cfg)
	if err != nil {
		return fmt.Errorf("configure github client for scheduler: %w", err)
	}
	if githubEnabled {
		reconciler := githubfactory.NewReconciler(githubClient, store, cfg.ReconcileInterval, bus.all)
		engine := workflow.NewEngine(store, githubClient, workflow.Config{WIPLimits: workflow.WIPLimits{
			domain.RolePM:         cfg.WorkflowPMWIP,
			domain.RoleTeamLead:   cfg.WorkflowTeamLeadWIP,
			domain.RoleDev:        cfg.WorkflowDevWIP,
			domain.RoleReviewer:   cfg.WorkflowReviewerWIP,
			domain.RoleCIGuardian: cfg.WorkflowCIGuardianWIP,
		}})
		source := orchestrator.NewGitHubSnapshotSource(store, githubClient)
		refiller := orchestrator.NewRepositoryRefiller(store, engine)
		coordinator := workflow.NewCoordinator(source, engine, refiller, cfg.WorkflowInterval).WithWake(bus.workflows)
		components = append(components,
			namedComponent{name: "github reconciler", run: reconciler.Run},
			namedComponent{name: "workflow coordinator", run: coordinator.Run},
		)
	} else {
		slog.Warn("github reconciliation and workflow coordination disabled because PAT auth mode has no token")
	}
	return runComponents(ctx, components, cfg.ShutdownTimeout)
}

func runWorkers(ctx context.Context, cfg config.Config, store *postgres.Store) error {
	runtimeConfig, err := runtimefactory.LoadConfigFile(cfg.RuntimeConfigPath)
	if err != nil {
		return fmt.Errorf("load runtime config: %w", err)
	}
	supervisor := runtimefactory.NewSupervisor()
	registry, err := runtimefactory.BuildRegistry(runtimeConfig, supervisor, nil)
	if err != nil {
		return fmt.Errorf("build runtime registry: %w", err)
	}
	githubClient, githubEnabled, err := configuredGitHubClient(cfg)
	if err != nil {
		return fmt.Errorf("configure github client for worker: %w", err)
	}
	if !githubEnabled {
		return errors.New("github authentication is required for worker governance")
	}
	governance := workflow.GovernanceEngine{
		Next: registry,
		Sink: orchestrator.NewGovernanceSink(store, githubClient, cfg.TaskReservationTTL),
	}
	repositoryManager := repositoryfactory.NewManager(cfg.RepositoryRoot)
	baseBuilder := orchestrator.RequestBuilder{Config: orchestrator.ExecutionConfig{RepositoryRoot: cfg.RepositoryRoot}}
	builder := orchestrator.ManagedRequestBuilder{Base: baseBuilder, Repository: repositoryManager}
	workspaceManager := workspace.NewWorktreeManager(cfg.WorkspaceRoot)
	verification := &verifier.Verifier{Supervisor: supervisor}

	baseID := strings.TrimSpace(cfg.WorkerID)
	if baseID == "" {
		baseID = "worker@" + hostName()
	}
	components := make([]namedComponent, 0, cfg.WorkerCapacity)
	workerIDs := make([]string, 0, cfg.WorkerCapacity)
	for i := 0; i < cfg.WorkerCapacity; i++ {
		workerID := baseID
		if cfg.WorkerCapacity > 1 {
			workerID = fmt.Sprintf("%s-%02d", baseID, i+1)
		}
		workerIDs = append(workerIDs, workerID)
		worker := workerfactory.New(store, governance, builder, workerfactory.Config{
			ID:                workerID,
			Host:              hostName(),
			Capacity:          1,
			PollInterval:      cfg.WorkerPollInterval,
			LeaseDuration:     cfg.WorkerLeaseDuration,
			HeartbeatInterval: cfg.WorkerHeartbeat,
			DefaultTimeout:    cfg.WorkerDefaultTimeout,
			RetryBase:         cfg.WorkerRetryBase,
		}).WithExecution(workspaceManager, verification, builder)
		components = append(components, namedComponent{name: workerID, run: worker.Run})
	}

	runErr := runComponents(ctx, components, cfg.ShutdownTimeout)
	drainCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	for _, workerID := range workerIDs {
		if err := store.SetWorkerDraining(drainCtx, workerID, true, time.Now().UTC()); err != nil {
			slog.Warn("mark worker draining failed", "worker", workerID, "error", err)
		}
	}
	return runErr
}

type namedComponent struct {
	name string
	run  func(context.Context) error
}

func runComponents(ctx context.Context, components []namedComponent, shutdownTimeout time.Duration) error {
	if len(components) == 0 {
		return errors.New("no components configured")
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 20 * time.Second
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(components))
	for _, component := range components {
		component := component
		go func() {
			err := component.run(child)
			if err == nil && child.Err() == nil {
				err = fmt.Errorf("%s stopped unexpectedly", component.name)
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				err = fmt.Errorf("%s: %w", component.name, err)
			}
			errCh <- err
		}()
	}

	remaining := len(components)
	var result error
	select {
	case <-ctx.Done():
		result = ctx.Err()
	case err := <-errCh:
		remaining--
		result = err
	}
	cancel()

	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()
	for remaining > 0 {
		select {
		case <-errCh:
			remaining--
		case <-timer.C:
			return errors.Join(result, fmt.Errorf("component shutdown exceeded %s", shutdownTimeout))
		}
	}
	return result
}

func signalChannel(channel chan<- struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

type leaseRecoveryStore interface {
	RecoverExpiredLeases(ctx context.Context, now time.Time) (int64, error)
}

func runLeaseRecovery(ctx context.Context, store leaseRecoveryStore, interval time.Duration) error {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	for {
		recovered, err := store.RecoverExpiredLeases(ctx, time.Now().UTC())
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			slog.Error("lease recovery failed", "error", err)
		} else if recovered > 0 {
			slog.Warn("recovered expired job leases", "count", recovered)
		}

		timer := time.NewTimer(interval)
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

func configureLogging(level string) {
	var slogLevel slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn", "warning":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})))
}

func hostName() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "unknown-host"
	}
	return hostname
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
