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
	"syscall"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/config"
	"github.com/hoanghonghuy/synfactory/internal/domain"
	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	"github.com/hoanghonghuy/synfactory/internal/orchestrator"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type healthResponse struct{ Status string `json:"status"` }

func main(){
	cfg,err:=config.Load();if err!=nil{slog.Error("load config","error",err);os.Exit(1)}
	startupCtx,cancelStartup:=context.WithTimeout(context.Background(),15*time.Second);store,err:=postgres.Open(startupCtx,cfg.DatabaseURL,postgres.Options{MaxOpenConns:cfg.DBMaxOpenConns,MaxIdleConns:cfg.DBMaxIdleConns,ConnMaxIdle:cfg.DBConnMaxIdle,ConnMaxLifetime:cfg.DBConnMaxLifetime});cancelStartup();if err!=nil{slog.Error("connect postgres","error",err);os.Exit(1)};defer func(){_ = store.Close()}()
	migrationCtx,cancelMigration:=context.WithTimeout(context.Background(),30*time.Second);err=store.ApplyMigrations(migrationCtx);cancelMigration();if err!=nil{slog.Error("apply migrations","error",err);os.Exit(1)}
	ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop()
	wakeEvents:=make(chan struct{},1);wakeWorkflows:=make(chan struct{},1);wakeEventProcessor:=func(){signalChannel(wakeEvents)};wakeWorkflowCoordinator:=func(){signalChannel(wakeWorkflows)};wakeAll:=func(){wakeEventProcessor();wakeWorkflowCoordinator()}
	hostname,err:=os.Hostname();if err!=nil||hostname==""{hostname="unknown-host"}
	eventStore:=&orchestrator.WorkflowEventStore{Store:store,Wake:wakeWorkflowCoordinator};eventProcessor:=githubfactory.NewEventProcessor(eventStore,"event-router@"+hostname,cfg.EventPollInterval,cfg.EventLeaseDuration,cfg.EventMaxAttempts,wakeEvents);go runComponent(ctx,"event processor",eventProcessor.Run)
	if cfg.GitHubToken!=""{githubClient:=githubfactory.NewClient(cfg.GitHubAPIURL,cfg.GitHubToken,nil);reconciler:=githubfactory.NewReconciler(githubClient,store,cfg.ReconcileInterval,wakeAll);go runComponent(ctx,"github reconciler",reconciler.Run);engine:=workflow.NewEngine(store,githubClient,workflow.Config{WIPLimits:workflow.WIPLimits{domain.RolePM:cfg.WorkflowPMWIP,domain.RoleTeamLead:cfg.WorkflowTeamLeadWIP,domain.RoleDev:cfg.WorkflowDevWIP,domain.RoleReviewer:cfg.WorkflowReviewerWIP,domain.RoleCIGuardian:cfg.WorkflowCIGuardianWIP}});source:=orchestrator.NewGitHubSnapshotSource(store,githubClient);refiller:=orchestrator.NewRepositoryRefiller(store,engine);coordinator:=workflow.NewCoordinator(source,engine,refiller,cfg.WorkflowInterval).WithWake(wakeWorkflows);go runComponent(ctx,"workflow coordinator",coordinator.Run)}else{slog.Warn("github reconciliation and workflow coordination disabled because SYNFACTORY_GITHUB_TOKEN is empty")}
	go runComponent(ctx,"lease recovery",func(ctx context.Context)error{return runLeaseRecovery(ctx,store,cfg.LeaseRecoveryInterval)})
	mux:=http.NewServeMux();mux.Handle("/webhooks/github",githubfactory.NewWebhookHandler(cfg.GitHubWebhookSecret,store,wakeAll));mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,http.StatusOK,healthResponse{Status:"ok"})});mux.HandleFunc("GET /readyz",func(w http.ResponseWriter,r *http.Request){checkCtx,cancel:=context.WithTimeout(r.Context(),2*time.Second);defer cancel();if err:=store.Ping(checkCtx);err!=nil{writeJSON(w,http.StatusServiceUnavailable,healthResponse{Status:"not_ready"});return};writeJSON(w,http.StatusOK,healthResponse{Status:"ready"})})
	server:=&http.Server{Addr:cfg.Addr,Handler:mux,ReadHeaderTimeout:5*time.Second,ReadTimeout:30*time.Second,WriteTimeout:30*time.Second,IdleTimeout:120*time.Second};go func(){slog.Info("synfactory api listening","addr",cfg.Addr);if err:=server.ListenAndServe();err!=nil&&!errors.Is(err,http.ErrServerClosed){slog.Error("api server failed","error",err);stop()}}();<-ctx.Done();shutdownCtx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();if err:=server.Shutdown(shutdownCtx);err!=nil{slog.Error("api shutdown failed","error",err)}
}

func signalChannel(channel chan<- struct{}){select{case channel<-struct{}{}:default:}}
type leaseRecoveryStore interface{RecoverExpiredLeases(context.Context,time.Time)(int64,error)}
func runLeaseRecovery(ctx context.Context,store leaseRecoveryStore,interval time.Duration)error{if interval<=0{interval=30*time.Second};for{recovered,err:=store.RecoverExpiredLeases(ctx,time.Now().UTC());if err!=nil{if errors.Is(err,context.Canceled){return ctx.Err()};slog.Error("lease recovery failed","error",err)}else if recovered>0{slog.Warn("recovered expired job leases","count",recovered)};timer:=time.NewTimer(interval);select{case<-ctx.Done():if !timer.Stop(){<-timer.C};return ctx.Err();case<-timer.C:}}}
func runComponent(ctx context.Context,name string,run func(context.Context)error){err:=run(ctx);if err!=nil&&!errors.Is(err,context.Canceled){slog.Error(fmt.Sprintf("%s stopped",name),"error",err)}}
func writeJSON(w http.ResponseWriter,status int,value any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(value)}
