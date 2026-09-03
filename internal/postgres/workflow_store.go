package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

const workflowColumns = `
id, dedupe_key, repository_id, kind, subject, revision, state, priority,
COALESCE(blocked_reason, ''), ci_repair_attempts, ci_repair_limit,
review_repair_attempts, review_repair_limit, last_dispatched_at,
metadata, created_at, updated_at`

func (s *Store) UpsertWorkflow(ctx context.Context, instance workflow.Instance) (workflow.Instance, error) {
	if instance.ID == "" || instance.DedupeKey == "" || instance.RepositoryID == "" || instance.Kind == "" || instance.Subject == "" { return workflow.Instance{}, fmt.Errorf("workflow id, dedupe key, repository, kind and subject are required") }
	if instance.State == "" { instance.State = workflow.StateDiscovered }
	if instance.Priority == 0 { instance.Priority = 100 }
	if instance.CIRepairLimit <= 0 { instance.CIRepairLimit = 2 }
	if instance.ReviewRepairLimit <= 0 { instance.ReviewRepairLimit = 2 }
	row := s.db.QueryRowContext(ctx, `
INSERT INTO workflow_instances (id,dedupe_key,repository_id,kind,subject,revision,state,priority,ci_repair_limit,review_repair_limit,metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (dedupe_key) DO UPDATE SET
 revision=EXCLUDED.revision, priority=EXCLUDED.priority,
 state=CASE WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision AND (workflow_instances.kind='repository' OR workflow_instances.state='blocked') THEN 'discovered' ELSE workflow_instances.state END,
 blocked_reason=CASE WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision THEN NULL ELSE workflow_instances.blocked_reason END,
 ci_repair_attempts=CASE WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision THEN 0 ELSE workflow_instances.ci_repair_attempts END,
 review_repair_attempts=CASE WHEN workflow_instances.revision IS DISTINCT FROM EXCLUDED.revision THEN 0 ELSE workflow_instances.review_repair_attempts END,
 metadata=workflow_instances.metadata || EXCLUDED.metadata, updated_at=NOW()
RETURNING `+workflowColumns,
		instance.ID,instance.DedupeKey,instance.RepositoryID,instance.Kind,instance.Subject,instance.Revision,instance.State,instance.Priority,instance.CIRepairLimit,instance.ReviewRepairLimit,jsonOrEmpty(instance.Metadata))
	item,err:=scanWorkflow(row); if err!=nil{return workflow.Instance{},fmt.Errorf("upsert workflow: %w",err)}; return item,nil
}

func (s *Store) GetWorkflow(ctx context.Context,id string)(workflow.Instance,error){ item,err:=scanWorkflow(s.db.QueryRowContext(ctx,`SELECT `+workflowColumns+` FROM workflow_instances WHERE id=$1`,id)); if err==sql.ErrNoRows{return workflow.Instance{},ErrNotFound}; if err!=nil{return workflow.Instance{},fmt.Errorf("get workflow: %w",err)}; return item,nil }
func (s *Store) ListActiveWorkflows(ctx context.Context)([]workflow.Instance,error){ rows,err:=s.db.QueryContext(ctx,`SELECT `+workflowColumns+` FROM workflow_instances WHERE state NOT IN ('completed','parked') ORDER BY priority DESC,last_dispatched_at NULLS FIRST,created_at ASC`); if err!=nil{return nil,fmt.Errorf("list active workflows: %w",err)}; defer rows.Close(); var items []workflow.Instance; for rows.Next(){item,err:=scanWorkflow(rows);if err!=nil{return nil,fmt.Errorf("scan active workflow: %w",err)};items=append(items,item)};return items,rows.Err() }
func (s *Store) AddWorkflowDependency(ctx context.Context,workflowID,dependsOnID string)error{ if workflowID==""||dependsOnID==""||workflowID==dependsOnID{return fmt.Errorf("valid distinct workflow ids are required")};_,err:=s.db.ExecContext(ctx,`INSERT INTO workflow_dependencies(workflow_id,depends_on_id) VALUES($1,$2) ON CONFLICT DO NOTHING`,workflowID,dependsOnID);return err }
func (s *Store) DependenciesSatisfied(ctx context.Context,workflowID string)(bool,error){var ok bool;err:=s.db.QueryRowContext(ctx,`SELECT NOT EXISTS(SELECT 1 FROM workflow_dependencies d JOIN workflow_instances w ON w.id=d.depends_on_id WHERE d.workflow_id=$1 AND w.state<>d.required_state)`,workflowID).Scan(&ok);return ok,err}
func (s *Store) ActionSucceeded(ctx context.Context,workflowID string,kind workflow.ActionKind,revision string)(bool,error){var ok bool;err:=s.db.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM workflow_actions a JOIN jobs j ON j.id=a.job_id WHERE a.workflow_id=$1 AND a.kind=$2 AND a.revision=$3 AND j.status='succeeded')`,workflowID,kind,revision).Scan(&ok);return ok,err}
func (s *Store) LatestActionStatus(ctx context.Context,workflowID string,kind workflow.ActionKind,revision string)(domain.JobStatus,bool,error){var status domain.JobStatus;err:=s.db.QueryRowContext(ctx,`SELECT j.status FROM workflow_actions a JOIN jobs j ON j.id=a.job_id WHERE a.workflow_id=$1 AND a.kind=$2 AND a.revision=$3 ORDER BY a.created_at DESC LIMIT 1`,workflowID,kind,revision).Scan(&status);if err==sql.ErrNoRows{return "",false,nil};return status,err==nil,err}
func (s *Store) LatestActionDecision(ctx context.Context,workflowID string,kind workflow.ActionKind,revision string)(string,bool,error){var d string;err:=s.db.QueryRowContext(ctx,`SELECT COALESCE(a.decision,'') FROM workflow_actions a JOIN jobs j ON j.id=a.job_id WHERE a.workflow_id=$1 AND a.kind=$2 AND a.revision=$3 AND j.status='succeeded' AND a.decision IS NOT NULL ORDER BY a.completed_at DESC NULLS LAST,a.created_at DESC LIMIT 1`,workflowID,kind,revision).Scan(&d);if err==sql.ErrNoRows{return "",false,nil};return d,d!="",err}
func (s *Store) ActiveRoleCount(ctx context.Context,role domain.Role)(int,error){var count int;err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM jobs WHERE role=$1 AND status IN ('queued','leased','running','retry_wait')`,role).Scan(&count);return count,err}

func scanWorkflow(row rowScanner)(workflow.Instance,error){var item workflow.Instance;err:=row.Scan(&item.ID,&item.DedupeKey,&item.RepositoryID,&item.Kind,&item.Subject,&item.Revision,&item.State,&item.Priority,&item.BlockedReason,&item.CIRepairAttempts,&item.CIRepairLimit,&item.ReviewRepairAttempts,&item.ReviewRepairLimit,&item.LastDispatchedAt,&item.Metadata,&item.CreatedAt,&item.UpdatedAt);return item,err}
