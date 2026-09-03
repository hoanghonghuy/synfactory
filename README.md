# SynFactory

SynFactory is a self-hosted, always-on software factory: a control plane for running an autonomous IT department across GitHub repositories.

## Core principles

- **Go-native control plane.** Backend, scheduler, dispatcher, workers, policies, state machines, integrations, and CLI are implemented in Go.
- **Vue only for the web UI.** The web dashboard is optional and will be added after the backend contracts stabilize.
- **Hybrid triggers.** GitHub webhooks are the fast path; periodic reconciliation (default: hourly) is the recovery/correctness path.
- **GitHub is the product-work source of truth.** Issues, PRs, reviews, checks, branches, commits and repository policy are reconciled into SynFactory state.
- **Pluggable agent runtimes.** Codex, Cursor, Antigravity, Claude Code, OpenCode and OpenAI-compatible/custom endpoints are adapters behind one runtime interface.
- **Roles are not runtimes.** PM, Team Lead, Developer, Reviewer, QA and Release roles can use different runtimes/models and fallback chains.
- **Failure must not stall the factory.** Every run has retry/budget limits, durable evidence and a terminal/escalated state; workers return to the queue instead of looping forever.
- **Deterministic authority boundaries.** Prompts describe behavior; the control plane enforces permissions (read/write/review/merge/run-command) independently of the model.
- **Event deduplication and leases.** Webhook events, reconciliation and concurrent workers must not execute the same repository revision twice.
- **Self-host first.** Initial target is Docker Compose on EC2/VPS. Services may later be split across hosts without changing domain contracts.

## Target architecture

```text
GitHub webhooks ────────┐
                        v
                   Event Inbox <──────── Hourly Reconciler
                        |
                        v
                    Dispatcher
                        |
        +---------------+----------------+
        |               |                |
        v               v                v
     PM/TL jobs      Dev jobs       Review/QA jobs
        |               |                |
        +---------------+----------------+
                        |
                 Runtime Adapters
        +-------+-------+-------+--------+---------+
        |       |       |       |        |         |
      Codex   Cursor   Agy   Claude   OpenCode   Custom
                        |
                        v
                 Isolated Workspaces
                        |
                        v
                GitHub PR / CI / Review
```

## Repository layout

```text
cmd/
  synfactory/          # operator CLI / API process entrypoint
  worker/              # worker entrypoint
internal/
  domain/              # pure domain types and state machines
  app/                 # use cases / orchestration
  events/              # inbox, dedupe, routing
  scheduler/           # reconciliation and scheduled work
  dispatcher/          # queue/leases/admission/WIP
  github/              # GitHub integration
  runtime/             # agent runtime ports + adapters
  workspace/           # git/worktree/container isolation
  policy/              # authorization, budgets, merge gates
  evidence/            # run artifacts and verification evidence
  persistence/         # PostgreSQL repositories
  observability/       # logs/metrics/tracing
migrations/             # PostgreSQL migrations
configs/                # example runtime/repo/workflow configuration
docs/                   # architecture, ADRs and upstream research
scripts/                # development/research/deployment helpers
web/                    # optional Vue 3 dashboard (later phase)
```

## Upstream research

SynFactory is a clean Go implementation that synthesizes proven ideas rather than mechanically translating another project. Research targets include Miniforge, Vanguard, Paddock, Vercel eve Software Factory, Super Simple Software Factory, OpenHands, MetaGPT and ChatDev. See `docs/upstream-research.md` and `scripts/clone-upstreams.sh`.

## Initial delivery phases

1. **Foundation:** domain model, event inbox, durable jobs, leases, PostgreSQL, config.
2. **GitHub loop:** webhook ingestion + signature validation, event dedupe, hourly reconciliation, issue/PR/check projection.
3. **Runtime system:** common adapter contract; Codex first, then Cursor, Antigravity, OpenCode/custom endpoint.
4. **Workflow engine:** PM/TL/Dev/Reviewer flows, retry budgets, evidence and escalation.
5. **Workspace isolation:** git worktrees/Docker sandboxes and deterministic command verification.
6. **Governance:** role permissions, WIP/admission, merge gates, cost/token/runtime budgets.
7. **Operations:** health, metrics, structured logs, recovery, backups and EC2 Docker Compose deployment.
8. **Web UI (optional):** Vue 3 + TypeScript control dashboard consuming the stable API.

## Status

Architecture bootstrap in progress.
