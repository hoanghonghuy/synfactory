package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	githubfactory "github.com/hoanghonghuy/synfactory/internal/github"
	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/securityaudit"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type governanceAuditTestStore struct {
	audits       []securityaudit.Event
	auditErr     error
	handoffCount int
	bound        map[string]bool
}

func (s *governanceAuditTestStore) ReserveTask(_ context.Context, _, fingerprint, _ string, _ time.Time, _ time.Duration) (bool, error) {
	if s.bound == nil {
		s.bound = map[string]bool{}
	}
	if s.bound[fingerprint] {
		return false, nil
	}
	return true, nil
}

func (s *governanceAuditTestStore) BindTask(_ context.Context, _, fingerprint, _ string, _ int64, _ string, _ time.Time) error {
	if s.bound == nil {
		s.bound = map[string]bool{}
	}
	s.bound[fingerprint] = true
	return nil
}

func (s *governanceAuditTestStore) RecordWorkflowHandoff(_ context.Context, _, _ string, _ json.RawMessage, _ time.Time) error {
	s.handoffCount++
	return nil
}

func (s *governanceAuditTestStore) AppendSecurityAudit(_ context.Context, event securityaudit.Event) error {
	if s.auditErr != nil {
		return s.auditErr
	}
	if err := event.Validate(); err != nil {
		return err
	}
	s.audits = append(s.audits, event)
	return nil
}

type governanceAuditTestIssues struct {
	createCount int
}

func (g *governanceAuditTestIssues) CreateIssue(_ context.Context, _, _, _ string, _ []string) (githubfactory.CreatedIssue, error) {
	g.createCount++
	return githubfactory.CreatedIssue{Number: 77, URL: "https://example.test/issues/77"}, nil
}

func (g *governanceAuditTestIssues) FindIssueByFingerprint(_ context.Context, _, _ string) (githubfactory.CreatedIssue, bool, error) {
	return githubfactory.CreatedIssue{}, false, nil
}

func TestGovernanceSinkWritesValueFreeAttributableAudit(t *testing.T) {
	store := &governanceAuditTestStore{}
	sink := NewGovernanceSink(store, nil, time.Minute)
	now := time.Date(2026, 9, 8, 1, 2, 3, 0, time.UTC)
	sink.now = func() time.Time { return now }

	request := factoryruntime.Request{
		Repository: "acme/widget",
		Role:       "reviewer",
		Metadata: map[string]string{
			"job_id":      "job-7",
			"workflow_id": "workflow-3",
			"task_id":     "42",
		},
	}
	handoff := workflow.Handoff{Action: workflow.ActionReview, Decision: "APPROVE"}
	if err := sink.Handle(context.Background(), request, handoff); err != nil {
		t.Fatalf("handle governance handoff: %v", err)
	}
	if len(store.audits) != 1 {
		t.Fatalf("expected one audit event, got %d", len(store.audits))
	}
	event := store.audits[0]
	if event.ActorType != "workflow_agent" || event.ActorID != "reviewer" {
		t.Fatalf("unexpected actor attribution: %#v", event)
	}
	if event.Action != "workflow.governance.review" || event.ResourceType != "workflow" || event.ResourceID != "workflow-3" {
		t.Fatalf("unexpected audit contract: %#v", event)
	}
	if event.Outcome != "approve" || event.RequestID != "job-7" || !event.OccurredAt.Equal(now) {
		t.Fatalf("unexpected audit correlation: %#v", event)
	}
	var metadata map[string]any
	if err := json.Unmarshal(event.Metadata, &metadata); err != nil {
		t.Fatalf("decode audit metadata: %v", err)
	}
	for key, want := range map[string]string{
		"decision": "APPROVE", "job_id": "job-7", "repository": "acme/widget", "task_id": "42", "workflow_id": "workflow-3",
	} {
		if got, _ := metadata[key].(string); got != want {
			t.Fatalf("metadata %s = %q, want %q", key, got, want)
		}
	}
	if raw := string(event.Metadata); strings.Contains(raw, request.Prompt) && request.Prompt != "" {
		t.Fatalf("audit metadata persisted runtime prompt: %s", raw)
	}
}

func TestGovernanceSinkFailsClosedWhenAuditPersistenceFails(t *testing.T) {
	store := &governanceAuditTestStore{auditErr: errors.New("audit unavailable")}
	sink := NewGovernanceSink(store, nil, time.Minute)
	request := factoryruntime.Request{Role: "team_lead", Metadata: map[string]string{"job_id": "job-8"}}
	handoff := workflow.Handoff{Action: workflow.ActionMergeGate, Decision: "APPROVE"}

	err := sink.Handle(context.Background(), request, handoff)
	if err == nil || !strings.Contains(err.Error(), "append governance security audit") {
		t.Fatalf("expected fail-closed audit error, got %v", err)
	}
	if store.handoffCount != 1 {
		t.Fatalf("handoff persistence count = %d, want 1", store.handoffCount)
	}
}

func TestGovernanceSinkBacklogRetryDoesNotDuplicateIssueAfterAuditFailure(t *testing.T) {
	store := &governanceAuditTestStore{auditErr: errors.New("audit unavailable")}
	issues := &governanceAuditTestIssues{}
	sink := NewGovernanceSink(store, issues, time.Minute)
	request := factoryruntime.Request{
		Repository: "acme/widget",
		Role:       "pm",
		Metadata: map[string]string{
			"job_id":        "job-refill",
			"workflow_id":   "workflow-refill",
			"repository_id": "repo-1",
			"task_id":       "repository",
		},
	}
	handoff := workflow.Handoff{
		Action:   workflow.ActionBacklogRefill,
		Decision: "DONE",
		Tasks: []workflow.TaskProposal{{
			Title: "Add governed capability", Capability: "governance", Scope: "audit", Body: "Deliver the capability.", Ready: true,
		}},
	}

	if err := sink.Handle(context.Background(), request, handoff); err == nil {
		t.Fatal("expected first attempt to fail on audit persistence")
	}
	if issues.createCount != 1 {
		t.Fatalf("created issues after first attempt = %d, want 1", issues.createCount)
	}

	store.auditErr = nil
	if err := sink.Handle(context.Background(), request, handoff); err != nil {
		t.Fatalf("retry governance handoff: %v", err)
	}
	if issues.createCount != 1 {
		t.Fatalf("retry duplicated external issue creation: count=%d", issues.createCount)
	}
	if len(store.audits) != 1 {
		t.Fatalf("successful retry audits = %d, want 1", len(store.audits))
	}
}
