package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

func TestRequestBuilderPropagatesWorkflowAndTaskIdentity(t *testing.T) {
	job := domain.Job{
		ID:      "job_123",
		Kind:    "implement",
		Role:    domain.RoleDev,
		Subject: "35",
		Metadata: json.RawMessage(`{
			"workflow_id":"workflow_abc"
		}`),
	}
	repository := postgres.Repository{
		ID:            "repo_1",
		FullName:      "owner/repo",
		DefaultBranch: "develop",
		Config:        json.RawMessage(`{"local_path":"/tmp/repo"}`),
	}

	request, err := (RequestBuilder{}).Build(context.Background(), job, repository)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := request.Metadata["workflow_id"]; got != "workflow_abc" {
		t.Fatalf("workflow_id = %q, want workflow_abc", got)
	}
	if got := request.Metadata["task_id"]; got != "35" {
		t.Fatalf("task_id = %q, want 35", got)
	}
	if got := request.Metadata["job_id"]; got != "job_123" {
		t.Fatalf("job_id = %q, want job_123", got)
	}
}

func TestRequestBuilderPrefersExplicitTaskIdentity(t *testing.T) {
	job := domain.Job{
		ID:      "job_456",
		Kind:    "review",
		Role:    domain.RoleReviewer,
		Subject: "47",
		Metadata: json.RawMessage(`{
			"workflow_id":"workflow_xyz",
			"task_id":"task_35"
		}`),
	}
	repository := postgres.Repository{
		ID:            "repo_1",
		FullName:      "owner/repo",
		DefaultBranch: "develop",
		Config:        json.RawMessage(`{"local_path":"/tmp/repo"}`),
	}

	request, err := (RequestBuilder{}).Build(context.Background(), job, repository)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := request.Metadata["task_id"]; got != "task_35" {
		t.Fatalf("task_id = %q, want task_35", got)
	}
}
