package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

func (s *Store) ApplyDecision(ctx context.Context,workflowID string,decision workflow.Decision,actor domain.Role,now time.Time)(workflow.Instance,error){tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return workflow.Instance{},fmt.Errorf("begin workflow decision: %w",err)};defer func(){_ = tx.Rollback()}();current,err:=lockWorkflow(ctx,tx,workflowID);if err!=nil{return workflow.Instance{},err};if !workflow.CanTransition(actor,current.State,decision.TargetState){return workflow.Instance{},workflow.ErrUnauthorizedActor};updated,err:=updateWorkflowState(ctx,tx,current,decision,actor,now,false);if err!=nil{return workflow.Instance{},err};if err:=tx.Commit();err!=nil{return workflow.Instance{},fmt.Errorf("commit workflow decision: %w",err)};return updated,nil}

func (s *Store) DispatchAction(ctx context.Context,workflowID string,decision workflow.Decision,job workflow.JobSpec,actor domain.Role,now time.Time)(workflow.Instance,workflow.DispatchResult,error){
	if decision.Action==nil||decision.Action.Mode!=workflow.ActionJob{return workflow.Instance{},workflow.DispatchResult{},fmt.Errorf("job workflow action is required")}
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return workflow.Instance{},workflow.DispatchResult{},fmt.Errorf("begin workflow dispatch: %w",err)};defer func(){_ = tx.Rollback()}()
	current,err:=lockWorkflow(ctx,tx,workflowID);if err!=nil{return workflow.Instance{},workflow.DispatchResult{},err};if !workflow.CanTransition(actor,current.State,decision.TargetState){return workflow.Instance{},workflow.DispatchResult{},workflow.ErrUnauthorizedActor}
	actionID:=deterministicID("wfa",decision.Action.Key);var existing sql.NullString
	err=tx.QueryRowContext(ctx,`INSERT INTO workflow_actions(id,workflow_id,action_key,kind,role,mode,target_state,revision,budget_kind,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10) ON CONFLICT(action_key) DO NOTHING RETURNING job_id`,actionID,workflowID,decision.Action.Key,decision.Action.Kind,decision.Action.Role,decision.Action.Mode,decision.Action.TargetState,current.Revision,decision.Action.Budget,jsonOrEmptyStringMap(decision.Action.Metadata)).Scan(&existing)
	if err==sql.ErrNoRows{var jobID sql.NullString;if err:=tx.QueryRowContext(ctx,`SELECT job_id FROM workflow_actions WHERE action_key=$1`,decision.Action.Key).Scan(&jobID);err!=nil{return workflow.Instance{},workflow.DispatchResult{},err};if err:=tx.Commit();err!=nil{return workflow.Instance{},workflow.DispatchResult{},err};return current,workflow.DispatchResult{JobID:jobID.String,Dispatched:false},nil}
	if err!=nil{return workflow.Instance{},workflow.DispatchResult{},fmt.Errorf("create workflow action: %w",err)}
	if job.ID==""||job.DedupeKey==""||job.RepositoryID==""||job.Role==""||job.Subject==""{return workflow.Instance{},workflow.DispatchResult{},fmt.Errorf("workflow job id, dedupe key, repository, role and subject are required")};if job.MaxAttempts<=0{job.MaxAttempts=1};if job.AvailableAt.IsZero(){job.AvailableAt=now};if job.Priority==0{job.Priority=100}
	var jobID string;err=tx.QueryRowContext(ctx,`INSERT INTO jobs(id,dedupe_key,repository_id,kind,role,subject,revision,priority,status,max_attempts,available_at,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'queued',$9,$10,$11) ON CONFLICT(dedupe_key) DO UPDATE SET dedupe_key=EXCLUDED.dedupe_key RETURNING id`,job.ID,job.DedupeKey,job.RepositoryID,job.Kind,job.Role,job.Subject,job.Revision,job.Priority,job.MaxAttempts,job.AvailableAt,jsonOrEmpty(job.Metadata)).Scan(&jobID);if err!=nil{return workflow.Instance{},workflow.DispatchResult{},fmt.Errorf("create workflow job: %w",err)}
	if _,err:=tx.ExecContext(ctx,`UPDATE workflow_actions SET status='dispatched',job_id=$2 WHERE action_key=$1`,decision.Action.Key,jobID);err!=nil{return workflow.Instance{},workflow.DispatchResult{},err}
	updated,err:=updateWorkflowState(ctx,tx,current,decision,actor,now,true);if err!=nil{return workflow.Instance{},workflow.DispatchResult{},err};if err:=tx.Commit();err!=nil{return workflow.Instance{},workflow.DispatchResult{},err};return updated,workflow.DispatchResult{JobID:jobID,Dispatched:true},nil
}

func (s *Store) RecordWorkflowHandoff(ctx context.Context,jobID,decision string,metadata json.RawMessage,completedAt time.Time)error{if jobID==""||decision==""{return fmt.Errorf("job id and handoff decision are required")};result,err:=s.db.ExecContext(ctx,`UPDATE workflow_actions SET decision=$2,status='completed',metadata=metadata||$3,completed_at=COALESCE(completed_at,$4) WHERE job_id=$1`,jobID,decision,jsonOrEmpty(metadata),completedAt);if err!=nil{return err};rows,err:=result.RowsAffected();if err!=nil{return err};if rows!=1{return fmt.Errorf("workflow action for job %s not found",jobID)};return nil}

func lockWorkflow(ctx context.Context,tx *sql.Tx,id string)(workflow.Instance,error){item,err:=scanWorkflow(tx.QueryRowContext(ctx,`SELECT `+workflowColumns+` FROM workflow_instances WHERE id=$1 FOR UPDATE`,id));if err==sql.ErrNoRows{return workflow.Instance{},ErrNotFound};if err!=nil{return workflow.Instance{},fmt.Errorf("lock workflow: %w",err)};return item,nil}
func updateWorkflowState(ctx context.Context,tx *sql.Tx,current workflow.Instance,decision workflow.Decision,actor domain.Role,now time.Time,dispatched bool)(workflow.Instance,error){ci,review:=0,0;if dispatched&&decision.Action!=nil{switch decision.Action.Budget{case workflow.BudgetCIRepair:ci=1;case workflow.BudgetReviewRepair:review=1}};row:=tx.QueryRowContext(ctx,`UPDATE workflow_instances SET state=$2,blocked_reason=NULLIF($3,''),ci_repair_attempts=ci_repair_attempts+$4,review_repair_attempts=review_repair_attempts+$5,last_dispatched_at=CASE WHEN $6 THEN $7 ELSE last_dispatched_at END,updated_at=$7 WHERE id=$1 RETURNING `+workflowColumns,current.ID,decision.TargetState,decision.BlockedReason,ci,review,dispatched,now);updated,err:=scanWorkflow(row);if err!=nil{return workflow.Instance{},err};if current.State!=decision.TargetState||decision.Reason!=""{if _,err:=tx.ExecContext(ctx,`INSERT INTO workflow_history(workflow_id,from_state,to_state,actor_role,reason) VALUES($1,$2,$3,$4,NULLIF($5,''))`,current.ID,current.State,decision.TargetState,actor,decision.Reason);err!=nil{return workflow.Instance{},err}};return updated,nil}
func deterministicID(prefix,value string)string{sum:=sha256.Sum256([]byte(value));return prefix+"_"+hex.EncodeToString(sum[:12])}
func jsonOrEmptyStringMap(value map[string]string)json.RawMessage{if len(value)==0{return json.RawMessage(`{}`)};encoded,err:=json.Marshal(value);if err!=nil{return json.RawMessage(`{}`)};return encoded}
