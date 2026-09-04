# SynFactory product roadmap

## Product direction

SynFactory is a self-hosted, always-on software factory for operating an autonomous software department across many repositories. The long-term target is not merely to run coding agents: it is a durable control plane that can plan work, execute it safely, review and verify it independently, continue useful work when another stream is blocked, expose strong operator controls, manage cost and infrastructure, and scale from one EC2/VPS to a multi-worker software-factory fleet.

This roadmap is the product-level planning source for major capability work. GitHub issues remain the executable work items and source of truth for implementation state. Roadmap items should become feature-sized issues with explicit acceptance criteria, dependencies and non-goals; avoid micro-issues that only move a few lines unless they represent a concrete defect or regression.

## Product invariants

- Go owns control-plane authority, workflow state, authorization, budgets, merge gates and durable policy.
- Vue is an optional operator presentation surface and must not become orchestration authority.
- The web experience is mobile-first and adaptive from ~360px phones to tablets, desktops and wide operations screens.
- GitHub is the initial product-work truth; PostgreSQL is execution/workflow truth.
- Agent runtimes and models remain replaceable behind SynFactory-owned interfaces.
- Agent output cannot self-authorize completion, review, merge, permissions or release.
- Verification, security and release identities are deterministic and tied to exact revisions.
- CI/review/provider/network failures use bounded budgets and classification; no failure mode may create an infinite repair loop.
- Blocked work releases capacity and must not prevent independent useful work from progressing.
- Self-hosted EC2/VPS remains a first-class deployment target even as the platform scales out.
- Secrets, credentials and raw interactive terminal contents must not leak into normal evidence/logging paths.

## Delivered foundation

The following capability groups are implemented on `develop` unless explicitly marked otherwise.

### Durable execution core

- PostgreSQL migrations and durable repository/event/job/run/evidence state.
- Transactional job claiming with `FOR UPDATE SKIP LOCKED`.
- Worker leases, renewal, stale-lease recovery and bounded retry/attempt state.
- Durable idempotency and event/job dedupe across restart and concurrency.
- Worker heartbeat/capacity/draining state.

### GitHub event and reconciliation engine

- Signed GitHub webhook ingestion with durable acknowledgement.
- Logical event normalization and dedupe.
- Periodic reconciliation as correctness/recovery path for missed events.
- Issue/PR/review/check/push/workflow projections.
- Rate-limit/backoff handling and synthesized missed events.

### Pluggable agent runtime engine

- Runtime registry and role-specific fallback chains.
- Codex, Cursor, Antigravity, Claude Code, OpenCode and OpenAI-compatible adapters.
- Timeout/cancellation, process-tree cleanup, redaction and normalized runtime results.
- Session resume where provider support exists.

### Isolation and verification

- Git worktree and Docker execution modes.
- Host-owned role permission policy.
- Read-only reviewer enforcement and mutation detection.
- Host-owned deterministic test/lint/build verification.
- Verification evidence linked to exact runtime run/revision.

### Autonomous software-department workflow

- Durable PM, Team Lead, Developer, Reviewer/QA and CI Guardian workflow roles.
- Backlog refill, task dedupe, WIP policy and dependency handling.
- Exact-head review and merge gating.
- Bounded CI/review repair cycles.
- Blocker parking/recovery and continuation onto independent useful work.
- Explicit issue lifecycle reconciliation for the `develop` integration flow.

### Repository management and GitHub authentication

- Managed repository registration, validation, enable/disable and configuration audit.
- GitHub App JWT and per-repository installation-token resolution.
- Expiration-aware token caching, bounded refresh and explicit PAT compatibility mode.

### 24/7 self-hosted operations

- API, scheduler/reconciler and worker process modes.
- Docker/Compose deployment for EC2/VPS and external PostgreSQL support.
- Health/readiness, Prometheus metrics and structured logs.
- Backup/restore, graceful shutdown, migration locking and worker recovery.
- Launch preflight, optimized build context and Compose-safe runtime defaults.

### Operator control center baseline

- Authenticated Vue 3 + TypeScript control center.
- Overview, repositories, workflows, jobs/runs, workers/runtimes and audit/evidence surfaces.
- Repository onboarding/lifecycle controls.
- Headless operation remains supported without Vue.

### Release and supply-chain foundation

- Go and frontend dependency vulnerability gates.
- Trivy scans for control/worker/web images.
- CycloneDX SBOMs and exact-head release evidence.
- Immutable GitHub Action pins and bounded scanner/network failure handling.
- Immutable OCI release publishing, source/evidence binding, digest-only promotion and rollback.

## Current active work

### P0 — Secure operator terminal (#29)

Goal: provide an EC2/SSH/9Remote-like operational terminal without bypassing SynFactory authority boundaries.

Target capabilities:

- Go-owned terminal session lifecycle.
- Local PTY and SSH-backed remote targets behind one contract.
- Authenticated WebSocket I/O, resize, reconnect/close and process cleanup.
- Strict host-key verification and secret-safe credential handling.
- Session capacity, idle timeout and maximum lifetime.
- Mobile-first xterm-compatible Vue experience including virtual-keyboard/orientation behavior.
- Session metadata audit without durable raw keystroke/output storage by default.

### P0 — Mobile-first adaptive control center (#31)

Goal: make phone operation a first-class use case rather than a desktop dashboard merely shrunk to mobile.

Target capabilities:

- Mobile-first application shell/navigation from ~360px upward.
- Adaptive cards/rows/drill-down on narrow screens and dense tables where useful on desktop.
- Touch-safe controls, drawers/sheets and safe-area/dynamic viewport support.
- Attention-first presentation for blocked/failing/stale work.
- No page-level horizontal scrolling for normal operator flows.
- Maintainable component/view boundaries as the product expands.

## Roadmap phases

Roadmap priorities are product priorities, not permission to bypass dependencies or gates. PM may refill backlog from the highest-priority eligible phase when existing READY work is exhausted.

## Phase A — Production autonomy validation

Priority: **P0**

Purpose: prove that the autonomous loop remains useful and recoverable under long-running real repository operation rather than treating unit/CI coverage as sufficient evidence of autonomy.

### A1. Autonomous dogfooding and soak testing

Add a long-running autonomy validation program that operates one or more real repositories and records objective health metrics.

Scope:

- multi-day PM → Dev → Review → CI → merge loops;
- webhook loss and reconciliation recovery;
- scheduler/API/worker restart recovery;
- provider outage and fallback behavior;
- repeated CI failure and repair-budget exhaustion;
- stale issue/PR/head reconciliation;
- worker crash/lease recovery;
- blocked-work switching and non-starvation;
- duplicate task detection quality;
- human-escalation correctness.

Add an **Autonomy Health** read model/dashboard covering useful-work ratio, autonomous merge success, duplicate tasks prevented/created, stuck workflows, repair cycles, escalation count, idle time and recovery outcomes.

Exit criteria: sustained autonomous operation with no known infinite loop, no duplicate-work amplification, deterministic recovery from injected failures and actionable health reporting.

## Phase B — Human control and collaboration

Priority: **P1**

Purpose: evolve from a single-operator self-host tool into a safely shared operational system.

### B1. Multi-user authentication, OIDC and RBAC

Add user/session identity with external OIDC/OAuth support and Go-owned role-based authorization.

Minimum product roles:

- administrator;
- operator;
- reviewer/approver;
- read-only observer.

Permissions must be independently scoped for sensitive capabilities such as repository mutation, release promotion and terminal access. Repository-scoped authorization should be possible without duplicating workflow logic in Vue.

### B2. Notification and escalation center

Add a provider-neutral notification subsystem for durable, deduplicated operator alerts.

Initial providers may include generic webhook plus practical adapters such as email, Slack, Discord, Telegram or Teams, but core policy must be provider-neutral.

Events should include permanent credential failures, exhausted repair budgets, worker-fleet outage, release/security blockers and true product decisions requiring human input.

### B3. Operator inbox / needs-attention workflow

Create one durable control-center queue for human attention items with acknowledge, resolve, delegate and snooze semantics where safe.

Examples: product decision required, security exception required, GitHub App access lost, release approval required, worker unavailable or terminal anomaly. Human actions must map back to Go-owned contracts rather than directly editing workflow truth in Vue.

Exit criteria for Phase B: multiple users can safely operate the same deployment with explicit permissions, and true escalation reaches the right person without alert spam.

## Phase C — Runtime economics and model governance

Priority: **P1**

Purpose: make always-on AI operation economically sustainable and measurable.

### C1. Usage, token and cost accounting

Record normalized usage by provider/model/runtime/role/repository/workflow/task/run, including token/request/runtime metrics when providers expose them and estimated cost using versioned pricing configuration.

### C2. Budget policies

Support repository/day, role/day, provider/day and workflow maximum budgets. Exhaustion must cause deterministic routing downgrade, parking or escalation rather than uncontrolled spend.

### C3. Dynamic runtime/model routing

Route work using task complexity, provider health, capability, latency, historical success and configured cost policy. Keep deterministic policy boundaries in Go and never let a model self-select a more privileged runtime.

### C4. Runtime performance scoreboard

Measure success rate, CI/review rework, latency, cost and failure classification by runtime/model/role. Use this data as routing input while preserving operator overrides and auditability.

Exit criteria: operators can explain where AI spend went, enforce hard budgets and compare runtime effectiveness using retained evidence.

## Phase D — Distributed worker fleet

Priority: **P1**

Purpose: scale execution beyond one server while retaining the same durable job/workflow semantics.

### D1. First-class worker capability inventory

Workers advertise OS, architecture, CPU, memory, GPU, Docker, available agent runtimes, labels, region/network class and capacity. Scheduler placement should match job requirements to worker capability.

### D2. Outbound worker agent transport

Introduce a worker transport where remote workers establish an authenticated outbound connection to the control plane, receive leased work and upload evidence. This avoids requiring inbound SSH and supports NAT/home/GPU/remote nodes.

Reuse the same transport contract for future remote terminal attachment where possible:

`Browser -> Control Plane -> Worker Agent -> PTY`

rather than inventing a second remote-host control system.

### D3. Fleet lifecycle and maintenance

Add drain, pause, maintenance/cordon, graceful removal, lost-worker recovery and capacity-aware scheduling.

### D4. Optional infrastructure autoscaling

Behind provider interfaces, allow measured queue pressure to start/stop compute such as EC2 workers. Autoscaling must remain optional and must not couple core workflow logic to AWS.

Exit criteria: workers can be added/removed across hosts without changing repository/workflow contracts, and lost nodes do not strand work.

## Phase E — Secrets and security operations

Priority: **P1**

Purpose: strengthen long-running production operation beyond local environment/file secrets.

### E1. SecretProvider abstraction

Support environment/file as baseline plus optional providers such as Docker secrets, AWS Secrets Manager, HashiCorp Vault or 1Password Connect. Workflow code requests logical secret names and never depends on a backend.

### E2. Credential health and rotation

Track non-secret metadata such as credential age, expiry, last successful use, owner and rotation state. Alert before expiration and support safe staged rotation.

### E3. Expanded audit and retention policy

Unify operator, workflow, agent, release, security and terminal session metadata into a searchable audit trail with configurable retention/export while still excluding raw secrets and terminal I/O.

Exit criteria: production credentials can be rotated without downtime or secret leakage, and security-sensitive actions remain attributable.

## Phase F — Portfolio / multi-project IT department

Priority: **P2**

Purpose: operate a software portfolio rather than treating every repository as an unrelated island.

### F1. Projects and repository groups

Allow repositories to belong to products/projects/platform groups with shared context, policies and health summaries.

### F2. Cross-repository dependency graph

Represent feature/task dependencies across repositories and wake dependent workflows automatically when blocking facts change.

### F3. Portfolio roadmap and milestones

Add Now/Next/Later and milestone/release planning at project/portfolio level. PM agents should reason over feature completeness, dependencies and release readiness across repositories while GitHub issues/PRs remain execution truth.

Exit criteria: a PM/operator can see and manage one product spanning many repositories without manually correlating every issue.

## Phase G — Engineering knowledge and architecture intelligence

Priority: **P2**

Purpose: improve agent decisions with durable, reviewable repository knowledge instead of hidden conversation memory.

### G1. Versioned repository knowledge base

Index relevant ADRs, specs, service boundaries, database/API contracts and historical decisions. Retrieval must be revision-aware and scoped to the task.

### G2. Durable repository memory

Store explicit reviewable facts such as architectural conventions, integration branch, security constraints and module ownership with provenance/versioning. Hidden LLM memory is not authoritative product policy.

### G3. Automated architecture review

Detect dependency cycles, boundary violations, duplicated modules, risky migrations, API compatibility breaks and other structural drift. Findings should dedupe against existing work and create feature-sized remediation issues rather than micro-task spam.

Exit criteria: agents receive relevant project knowledge automatically and architectural drift becomes measurable/actionable.

## Phase H — Quality and performance intelligence

Priority: **P2**

Purpose: make verification risk-aware and learn from historical CI behavior.

### H1. Test intelligence

Use changed files, dependency graph and historical failures to choose fast targeted checks before required full gates, without weakening the final merge contract.

### H2. Flaky-test intelligence

Track historical test instability and distinguish likely infrastructure/flaky failures from deterministic regressions. CI Guardian still must not silently ignore a required failing gate.

### H3. Performance/regression baselines

Track selected benchmarks such as build/bundle/image size, startup time, resource consumption, query latency or repository-specific benchmarks. Surface meaningful regressions in review/evidence.

Exit criteria: CI repair spends less effort on known noise while required quality gates remain deterministic.

## Phase I — Forge/provider expansion

Priority: **P3 / Future**

Purpose: remove GitHub as a permanent implementation dependency after the GitHub product loop is proven stable.

Define a SynFactory-owned forge/SCM contract for issues, merge requests/PRs, reviews, checks, branches, comments and webhooks. Potential adapters: GitLab, Gitea, Forgejo and Bitbucket.

Do not start this phase while GitHub autonomy, worker fleet and human-control foundations still have critical product gaps.

## Phase J — Plugin and extension ecosystem

Priority: **P3 / Future**

Purpose: allow integrations to grow without turning the control plane into a vendor-specific monolith.

Potential extension categories:

- runtime/model adapters;
- notification providers;
- secret providers;
- forge providers;
- verifiers;
- worker transports/backends;
- deployment/infrastructure providers.

Plugins must not bypass core Go authorization, workflow invariants or evidence boundaries.

## Roadmap dependency order

```text
Current P0
  #29 secure terminal
  #31 mobile-first UI
       |
       v
Phase A autonomy dogfood/soak
       |
       +------------------------+
       v                        v
Phase B human control       Phase C cost/model governance
       |                        |
       +-------------+----------+
                     v
              Phase D worker fleet
                     |
                     v
              Phase E secrets/security ops
                     |
          +----------+----------+
          v                     v
Phase F portfolio        Phase G knowledge/architecture
          \                     /
           +---------+---------+
                     v
             Phase H quality intelligence
                     |
                     v
           Phase I/J ecosystem expansion
```

Some P1 workstreams may proceed in parallel when independent capacity exists. Dependencies exist to avoid premature complexity, not to force global serialization.

## PM backlog-refill policy

When no higher-priority READY implementation exists, PM should inspect this roadmap together with current GitHub issues, merged work and code before creating new tasks.

1. Dedupe against open, closed and recently merged equivalent work.
2. Prefer one coherent feature/workstream issue over many tiny issues.
3. Include goal, product intent, scope, acceptance criteria, dependencies and non-goals.
4. Reference existing architecture/docs or high-quality upstream implementations when that materially helps the Developer.
5. Never create a task solely because a roadmap heading exists; inspect current code and select the next concrete capability gap.
6. Do not start P2/P3 novelty while unresolved P0/P1 production gaps have higher value unless those gaps are genuinely blocked and independent work is useful.
7. Audit/optimization findings should become deduped issues before implementation unless they are deterministic repair work already authorized by an active task.

## Definition of product maturity

### Foundation complete

Core durable execution, GitHub integration, pluggable runtimes, isolation/verification, autonomous roles, operations, control center, repository onboarding, GitHub App auth and release security/publishing are implemented.

### Single-operator production factory

Foundation plus secure operator terminal, mobile-first control center and successful long-running autonomy soak tests.

### Multi-user autonomous IT platform

Single-operator production factory plus RBAC/OIDC, notifications/inbox, cost governance, distributed workers and production secret management.

### Portfolio-scale software factory

Multi-user platform plus cross-repository planning, durable engineering knowledge and quality/performance intelligence.

### Extensible ecosystem

Portfolio-scale platform plus additional forge providers and a governed plugin/extension model.

The project must not call itself "finished" simply because the current issue queue reaches zero. An empty queue means PM must reconcile delivered capability against this roadmap and either prove the next phase is intentionally deferred or create the next highest-value feature-sized issue.
