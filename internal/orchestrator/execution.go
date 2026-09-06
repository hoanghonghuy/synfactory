package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
	factoryruntime "github.com/hoanghonghuy/synfactory/internal/runtime"
	"github.com/hoanghonghuy/synfactory/internal/verifier"
	"github.com/hoanghonghuy/synfactory/internal/workflow"
)

type ExecutionConfig struct {
	RepositoryRoot string
}

type RequestBuilder struct {
	Config ExecutionConfig
}

type repositoryExecutionConfig struct {
	LocalPath       string             `json:"local_path"`
	WorkspaceMode   string             `json:"workspace_mode"`
	ContainerImage  string             `json:"container_image"`
	NetworkAllowed  bool               `json:"network_allowed"`
	ContainerMemory string             `json:"container_memory"`
	ContainerCPUs   string             `json:"container_cpus"`
	Verification    []verificationJSON `json:"verification"`
}

type verificationJSON struct {
	Name     string   `json:"name"`
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	Timeout  string   `json:"timeout"`
	Required *bool    `json:"required"`
}

func (b RequestBuilder) Build(_ context.Context, job domain.Job, repository postgres.Repository) (factoryruntime.Request, error) {
	cfg, err := decodeExecutionConfig(repository.Config)
	if err != nil {
		return factoryruntime.Request{}, err
	}
	workspacePath := strings.TrimSpace(cfg.LocalPath)
	if workspacePath == "" && b.Config.RepositoryRoot != "" {
		workspacePath = filepath.Join(b.Config.RepositoryRoot, filepath.FromSlash(repository.FullName))
	}
	if workspacePath == "" {
		return factoryruntime.Request{}, fmt.Errorf("repository %s requires config.local_path or SYNFACTORY_REPOSITORY_ROOT", repository.FullName)
	}
	action := workflow.ActionKind(job.Kind)
	request := factoryruntime.Request{
		Repository:  repository.FullName,
		Workspace:   workspacePath,
		Role:        string(job.Role),
		Prompt:      rolePrompt(job, repository, action),
		Permissions: workflow.PermissionsForRole(job.Role),
		Metadata: map[string]string{
			"workflow_action":  actionString(action),
			"workflow_id":      metadataString(job.Metadata, "workflow_id"),
			"task_id":          defaultString(metadataString(job.Metadata, "task_id"), job.Subject),
			"job_id":           job.ID,
			"repository_id":    repository.ID,
			"subject":          job.Subject,
			"workspace_mode":   defaultString(cfg.WorkspaceMode, "worktree"),
			"container_image":  cfg.ContainerImage,
			"network_allowed":  fmt.Sprintf("%t", cfg.NetworkAllowed),
			"container_memory": cfg.ContainerMemory,
			"container_cpus":   cfg.ContainerCPUs,
		},
	}
	return request, nil
}

func (RequestBuilder) Plan(_ context.Context, job domain.Job, repository postgres.Repository, _ factoryruntime.Request) (verifier.Plan, error) {
	cfg, err := decodeExecutionConfig(repository.Config)
	if err != nil {
		return verifier.Plan{}, err
	}
	plan := verifier.Plan{}
	for _, raw := range cfg.Verification {
		if strings.TrimSpace(raw.Name) == "" || strings.TrimSpace(raw.Command) == "" {
			return verifier.Plan{}, fmt.Errorf("repository %s has invalid verification check", repository.FullName)
		}
		required := true
		if raw.Required != nil {
			required = *raw.Required
		}
		timeout := 15 * time.Minute
		if raw.Timeout != "" {
			parsed, err := time.ParseDuration(raw.Timeout)
			if err != nil || parsed <= 0 {
				return verifier.Plan{}, fmt.Errorf("verification %s has invalid timeout %q", raw.Name, raw.Timeout)
			}
			timeout = parsed
		}
		plan.Checks = append(plan.Checks, verifier.Check{Name: raw.Name, Command: raw.Command, Args: raw.Args, Required: required, Timeout: timeout})
	}
	return plan, nil
}

func decodeExecutionConfig(raw json.RawMessage) (repositoryExecutionConfig, error) {
	if len(raw) == 0 {
		return repositoryExecutionConfig{}, nil
	}
	var cfg repositoryExecutionConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return repositoryExecutionConfig{}, fmt.Errorf("decode repository execution config: %w", err)
	}
	return cfg, nil
}

func rolePrompt(job domain.Job, repository postgres.Repository, action workflow.ActionKind) string {
	contract := roleContract(job.Role)
	return fmt.Sprintf(`You are the SynFactory %s agent executing workflow action %q for %s subject #%s at revision %s.

Repository contract:
- integration branch is %s;
- inspect current remote truth before acting;
- never weaken CI, branch protection, tests, acceptance criteria, or permission boundaries to make a task pass;
- do not wait indefinitely on CI/review/dependencies: perform only the action assigned here and report blockers precisely;
- do not perform actions reserved to another role;
- do not merge a pull request from this runtime. Merge is owned by the SynFactory control-plane gate.

Role contract:
%s

Action-specific contract:
%s

For governance actions, finish with exactly one machine-readable line:
SYNFACTORY_DECISION: APPROVE
or
SYNFACTORY_DECISION: REQUEST_CHANGES
or
SYNFACTORY_DECISION: DONE
or
SYNFACTORY_DECISION: BLOCKED

Do not claim success unless the repository/GitHub state actually reflects the work.`, job.Role, action, repository.FullName, job.Subject, job.Revision, repository.DefaultBranch, contract, actionGuidance(action))
}

func actionGuidance(action workflow.ActionKind) string {
	switch action {
	case workflow.ActionBacklogRefill:
		return `Inspect the repository, existing open/closed issues, recent work and product gaps. Propose only meaningful feature-sized work that is not equivalent to existing work. For each new task, emit exactly one single-line JSON marker before the final decision:
SYNFACTORY_TASK: {"title":"...","capability":"...","scope":"...","body":"...","ready":true}
Use capability + scope as a stable semantic identity. Avoid micro-tasks or one-line cleanup issues. Emit at most 10 proposals. If no worthwhile new task exists, emit no task markers and finish with SYNFACTORY_DECISION: DONE.`
	case workflow.ActionPMTriage:
		return `Refine whether the existing issue is sufficiently scoped and actionable. DONE means SynFactory may treat it as implementation-ready. BLOCKED means product intent or a dependency is materially missing.`
	case workflow.ActionReview:
		return `Review the exact revision shown above independently and read-only. APPROVE means this exact head passes your review; REQUEST_CHANGES means developer repair is required. The control plane records this handoff durably.`
	case workflow.ActionMergeGate:
		return `This is an authorization gate, not implementation. APPROVE only if the exact head, independent review and required checks are safe. Any other decision prevents merge.`
	case workflow.ActionEscalateBlocker:
		return `Inspect the recorded blocker and repository truth. DONE means the escalation note is complete; BLOCKED means the workflow must remain parked pending an external/material change.`
	default:
		return "Perform only this action and leave unrelated work untouched."
	}
}

func roleContract(role domain.Role) string {
	switch role {
	case domain.RolePM:
		return "Inspect backlog/spec/product intent; deduplicate work; refine acceptance criteria and meaningful feature-sized tasks. Do not implement product code."
	case domain.RoleTeamLead:
		return "Review architecture, code/spec alignment, dependencies and exact PR head. For merge_gate, APPROVE only if the exact head is safe, independently reviewed and CI-valid. Do not edit implementation code and do not merge."
	case domain.RoleDev:
		return "Implement only the authorized task on an isolated task branch, run verification, push/update the PR, and address actionable review feedback. Never self-merge or self-approve."
	case domain.RoleReviewer:
		return "Independently review the exact PR head read-only. Submit a GitHub review when possible. Never modify implementation files."
	case domain.RoleCIGuardian:
		return "Diagnose and repair CI only within the assigned bounded cycle. Do not broaden product scope or weaken checks."
	case domain.RoleQA:
		return "Verify acceptance criteria independently and report reproducible evidence. Do not modify implementation files."
	case domain.RoleRelease:
		return "Verify release readiness and operational gates. Do not bypass required checks."
	default:
		return "Perform only the explicitly authorized action."
	}
}

func actionString(action workflow.ActionKind) string {
	if action == "" {
		return "unknown"
	}
	return string(action)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
