package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type GovernanceStore interface {
	workflow.TaskRegistry
	RecordWorkflowHandoff(ctx context.Context, jobID, decision string, metadata json.RawMessage, completedAt time.Time) error
}

type governanceAuditWriter interface {
	AppendSecurityAudit(context.Context, securityaudit.Event) error
}

type IssueCreator interface {
	CreateIssue(ctx context.Context, repository, title, body string, labels []string) (githubfactory.CreatedIssue, error)
	FindIssueByFingerprint(ctx context.Context, repository, fingerprint string) (githubfactory.CreatedIssue, bool, error)
}

type GovernanceSink struct {
	store  GovernanceStore
	audit  governanceAuditWriter
	github IssueCreator
	guard  *workflow.TaskGuard
	now    func() time.Time
}

func NewGovernanceSink(store GovernanceStore, github IssueCreator, reservationTTL time.Duration) *GovernanceSink {
	audit, _ := store.(governanceAuditWriter)
	return &GovernanceSink{
		store: store, audit: audit, github: github, guard: workflow.NewTaskGuard(store, reservationTTL),
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *GovernanceSink) Handle(ctx context.Context, request factoryruntime.Request, handoff workflow.Handoff) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("governance store is required")
	}
	if s.audit == nil {
		return fmt.Errorf("governance security audit writer is required")
	}
	jobID := strings.TrimSpace(request.Metadata["job_id"])
	if jobID == "" {
		return fmt.Errorf("job_id metadata is required for governance handoff")
	}
	actorID := strings.TrimSpace(request.Role)
	if actorID == "" {
		return fmt.Errorf("runtime role is required for governance audit attribution")
	}
	if handoff.Action == workflow.ActionBacklogRefill && handoff.Decision == "DONE" {
		if s.github == nil {
			return fmt.Errorf("github issue creator is required for backlog refill")
		}
		repositoryID := request.Metadata["repository_id"]
		if repositoryID == "" || strings.TrimSpace(request.Repository) == "" {
			return fmt.Errorf("repository_id and repository are required for backlog refill")
		}
		for _, proposal := range handoff.Tasks {
			fingerprint, reserved, err := s.guard.Reserve(ctx, repositoryID, request.Repository, proposal.Capability, proposal.Scope, jobID, s.now())
			if err != nil {
				return err
			}
			if !reserved {
				continue
			}
			created, found, err := s.github.FindIssueByFingerprint(ctx, request.Repository, fingerprint)
			if err != nil {
				return err
			}
			if !found {
				body := proposal.Body + "\n\n---\nCreated by SynFactory PM backlog refill.\n\n<!-- synfactory-task-fingerprint:" + fingerprint + " -->\n"
				created, err = s.github.CreateIssue(ctx, request.Repository, proposal.Title, body, nil)
				if err != nil {
					return err
				}
			}
			state := "open"
			if proposal.Ready {
				state = "ready"
			}
			if err := s.guard.Bind(ctx, repositoryID, fingerprint, jobID, created.Number, state, s.now()); err != nil {
				return err
			}
		}
	}
	completedAt := s.now()
	metadata, _ := json.Marshal(map[string]any{
		"decision":   handoff.Decision,
		"task_count": len(handoff.Tasks),
	})
	if err := s.store.RecordWorkflowHandoff(ctx, jobID, handoff.Decision, metadata, completedAt); err != nil {
		return err
	}
	return s.appendGovernanceAudit(ctx, request, handoff, completedAt)
}

func (s *GovernanceSink) appendGovernanceAudit(ctx context.Context, request factoryruntime.Request, handoff workflow.Handoff, occurredAt time.Time) error {
	id, err := newGovernanceAuditID()
	if err != nil {
		return err
	}
	jobID := strings.TrimSpace(request.Metadata["job_id"])
	workflowID := strings.TrimSpace(request.Metadata["workflow_id"])
	resourceID := workflowID
	if resourceID == "" {
		resourceID = jobID
	}
	metadata, err := json.Marshal(map[string]any{
		"decision":    handoff.Decision,
		"job_id":      jobID,
		"repository":  strings.TrimSpace(request.Repository),
		"task_count":  len(handoff.Tasks),
		"task_id":     strings.TrimSpace(request.Metadata["task_id"]),
		"workflow_id": workflowID,
	})
	if err != nil {
		return fmt.Errorf("encode governance audit metadata: %w", err)
	}
	event := securityaudit.Event{
		ID:           id,
		OccurredAt:   occurredAt,
		ActorType:    "workflow_agent",
		ActorID:      strings.TrimSpace(request.Role),
		Action:       "workflow.governance." + string(handoff.Action),
		ResourceType: "workflow",
		ResourceID:   resourceID,
		Outcome:      strings.ToLower(strings.TrimSpace(handoff.Decision)),
		RequestID:    jobID,
		Metadata:     metadata,
	}
	if err := s.audit.AppendSecurityAudit(ctx, event); err != nil {
		return fmt.Errorf("append governance security audit: %w", err)
	}
	return nil
}

func newGovernanceAuditID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate governance audit id: %w", err)
	}
	return "audit-" + hex.EncodeToString(entropy[:]), nil
}
