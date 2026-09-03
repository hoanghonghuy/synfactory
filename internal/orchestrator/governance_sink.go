package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type GovernanceStore interface{ workflow.TaskRegistry; RecordWorkflowHandoff(context.Context,string,string,json.RawMessage,time.Time)error }
type IssueCreator interface{ CreateIssue(context.Context,string,string,string,[]string)(githubfactory.CreatedIssue,error); FindIssueByFingerprint(context.Context,string,string)(githubfactory.CreatedIssue,bool,error) }
type GovernanceSink struct{ store GovernanceStore; github IssueCreator; guard *workflow.TaskGuard; now func()time.Time }
func NewGovernanceSink(store GovernanceStore,github IssueCreator,reservationTTL time.Duration)*GovernanceSink{return &GovernanceSink{store:store,github:github,guard:workflow.NewTaskGuard(store,reservationTTL),now:func()time.Time{return time.Now().UTC()}}}
func (s *GovernanceSink) Handle(ctx context.Context,request factoryruntime.Request,handoff workflow.Handoff)error{if s==nil||s.store==nil{return fmt.Errorf("governance store is required")};jobID:=request.Metadata["job_id"];if jobID==""{return fmt.Errorf("job_id metadata is required for governance handoff")};if handoff.Action==workflow.ActionBacklogRefill&&handoff.Decision=="DONE"{if s.github==nil{return fmt.Errorf("github issue creator is required for backlog refill")};repositoryID:=request.Metadata["repository_id"];if repositoryID==""||strings.TrimSpace(request.Repository)==""{return fmt.Errorf("repository_id and repository are required for backlog refill")};for _,proposal:=range handoff.Tasks{fingerprint,reserved,err:=s.guard.Reserve(ctx,repositoryID,request.Repository,proposal.Capability,proposal.Scope,jobID,s.now());if err!=nil{return err};if !reserved{continue};created,found,err:=s.github.FindIssueByFingerprint(ctx,request.Repository,fingerprint);if err!=nil{return err};if !found{body:=proposal.Body+"\n\n---\nCreated by SynFactory PM backlog refill.\n\n<!-- synfactory-task-fingerprint:"+fingerprint+" -->\n";created,err=s.github.CreateIssue(ctx,request.Repository,proposal.Title,body,nil);if err!=nil{return err}};state:="open";if proposal.Ready{state="ready"};if err:=s.guard.Bind(ctx,repositoryID,fingerprint,jobID,created.Number,state,s.now());err!=nil{return err}}};metadata,_:=json.Marshal(map[string]any{"decision":handoff.Decision,"task_count":len(handoff.Tasks)});return s.store.RecordWorkflowHandoff(ctx,jobID,handoff.Decision,metadata,s.now())}
