# SynFactory architecture

## System objective

SynFactory is a durable control plane for autonomous software work. It reacts to repository events immediately, periodically reconciles source-of-truth state, dispatches bounded work to isolated agent runtimes, verifies results deterministically, and continues useful work when one workflow becomes blocked.

## Core service boundaries

### API / webhook service

Responsibilities:

- operator API;
- GitHub webhook signature validation;
- event normalization;
- durable insertion into `event_inbox`;
- health/readiness endpoints.

The webhook handler must acknowledge only after the event is durably stored. It must not run an LLM inline.

### Reconciler

Default schedule: hourly, configurable per installation/repository.

Responsibilities:

- query GitHub current state;
- compare it with SynFactory projections;
- recreate missed logical events;
- detect stale leases and abandoned runs;
- detect PR/CI/review changes missed by webhooks;
- refill work when a repository has no actionable queued jobs.

Webhook = latency optimization. Reconciliation = correctness mechanism.

### Dispatcher

Responsibilities:

- claim jobs transactionally;
- enforce WIP and per-repository concurrency;
- acquire/renew leases;
- apply role/runtime/policy routing;
- release workers after success, bounded failure or blocker;
- prevent a failing CI repair loop from monopolizing the factory.

Initial implementation should use PostgreSQL transactions and `FOR UPDATE SKIP LOCKED`; do not add Redis/Kafka until measured load requires it.

### Worker

Responsibilities:

- create/attach an isolated workspace;
- resolve the configured runtime adapter;
- invoke one bounded run;
- stream/store evidence;
- execute host-owned verification commands;
- publish outcome back to the dispatcher/state machine.

Workers do not decide global product priority and do not self-authorize extra permissions.

### Runtime adapters

All coding/agent systems implement one SynFactory-owned interface.

Initial adapter targets:

1. Codex CLI;
2. Cursor CLI;
3. Antigravity (`agy`) headless CLI;
4. OpenCode;
5. Claude Code;
6. OpenAI-compatible/custom endpoint adapter where direct API execution is appropriate.

A role selects a runtime through policy/config, for example PM on Antigravity and Team Lead on Codex. Fallback chains are allowed.

### Workspace adapters

Two planned modes:

- git worktree: cheap and fast for trusted/local workloads;
- Docker sandbox: stronger isolation for implementation/runtime execution.

Permission policy must be enforced outside the model. A reviewer configured as read-only must not be able to mutate the target checkout even if prompted to do so.

## Durable state

GitHub remains source of truth for product work (issues/PRs/reviews/checks). PostgreSQL is source of truth for factory execution state.

Minimum durable records:

- repositories;
- event inbox and dedupe identity;
- jobs and retry/lease state;
- attempts/runs and runtime sessions;
- evidence/artifacts;
- reconciliation watermarks;
- later: workflow contracts, policies, budgets, agent/runtime configs and audit log.

## Event identity

Logical event identity should contain at least:

```text
provider + repository + kind + subject + revision
```

Example:

```text
github + hoanghonghuy/synvideo + pull_request.synchronize + 55 + <head-sha>
```

Webhook delivery IDs are recorded for audit but are not sufficient as the only dedupe key because reconciliation can synthesize the same logical event without the original delivery ID.

## Failure model

A failed run consumes a bounded attempt. After the configured retry budget:

- preserve error/evidence;
- transition the job to a terminal failed/escalated state;
- release the worker;
- allow dispatcher to select other useful work;
- let a later repository event or explicit policy create a new job when conditions materially change.

No workflow owns a worker indefinitely.

## Deployment evolution

### V0 / single EC2

```text
Caddy
  |
Go API + scheduler + dispatcher
  |
PostgreSQL
  |
1-N worker processes -> Docker/worktrees -> CLI runtimes
```

### Scale-out

```text
EC2/API            EC2/workers A..N
    \                 /
     \               /
       PostgreSQL/RDS
```

Service contracts remain the same; only process placement changes.

## Web UI

No frontend is required for the control plane to operate. A Vue 3 + TypeScript dashboard is introduced only after API/domain stability. It should remain an operator surface, not a place where orchestration rules live.
