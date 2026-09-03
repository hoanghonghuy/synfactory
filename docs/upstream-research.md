# Upstream research and capability map

SynFactory is not a fork of one upstream project. It is a Go-native synthesis of several software-factory systems, with upstream source kept outside the product tree under ignored `upstreams/` directories for study and comparison.

## Research policy

1. Treat upstreams as architectural references first.
2. Record the license of any upstream before reusing source-level implementation details.
3. Prefer re-implementing behavior from contracts/tests/docs rather than translating code line-by-line.
4. Keep SynFactory domain types, persistence schema, runtime interfaces and operational model independent from upstream frameworks.
5. Add attribution/NOTICE material whenever an upstream license requires it and source is actually reused.

## Primary upstreams

### Miniforge

Repository: https://github.com/miniforge-ai/miniforge

Use as reference for:

- nested control loops instead of one long agent prompt;
- plan/implement/verify/review/release lifecycle;
- retry and repair budgets;
- PR observation after delivery;
- meta-level monitoring/governance;
- evidence-driven completion;
- bounded failure rather than infinite repair loops.

Migration target in SynFactory:

- `internal/domain` state machines;
- `internal/policy` budgets and terminal/escalation rules;
- `internal/evidence` verification records;
- PR reconciliation workflows.

Current license observed during bootstrap: Apache-2.0. Do not assume the same license for other upstreams.

### Vanguard

Repository: https://github.com/SebaBoler/vanguard

Use as reference for:

- GitHub/GitLab/Linear-style pluggable task sources;
- planner -> implementer -> reviewer -> adversary -> repair pipeline;
- isolated Docker execution;
- proof-of-work verification run by the host;
- always-on watch loop;
- provider/runtime abstraction;
- Codex, Cursor, Claude and OpenRouter/custom-endpoint integration patterns;
- cross-provider implementation/review.

Migration target in SynFactory:

- `internal/runtime` adapter model;
- `internal/workspace` isolation;
- deterministic verification outside the LLM;
- worker lifecycle and task claiming.

### Paddock

Repository: https://github.com/racecraft-lab/Paddock

Use as reference for:

- Facility -> Product Line -> Department hierarchy;
- GitHub issue projection into richer runtime tasks;
- repo-owned workflow contracts;
- isolated sandbox/worktree ownership;
- review packets and human gates;
- agent/session/task/cost/log/health control-plane concepts;
- REST + CLI + MCP operator surfaces;
- resource governance and recovery concepts.

Migration target in SynFactory:

- multi-repository tenancy/domain model;
- workflow contracts;
- task/run/review metadata;
- API and future Vue dashboard information architecture.

Do not carry over the current Next.js + SQLite implementation; SynFactory uses Go services and PostgreSQL.

### Vercel eve Software Factory Template

Repository: https://github.com/vercel-labs/eve-software-factory-template

Use as reference for:

- event-driven work intake;
- classifier -> analyst -> implementer -> independent reviewer separation;
- reviewer isolation from implementer reasoning;
- durable repository/factory memory;
- GitHub issue/comment/PR/check-suite triggers;
- bounded revision cycles.

Migration target in SynFactory:

- GitHub webhook routing;
- role context isolation;
- independent review policy;
- event-to-workflow mapping.

Do not carry over Vercel platform coupling; SynFactory must remain deployable on plain EC2/VPS.

### Super Simple Software Factory

Repository: https://github.com/disler/super-simple-software-factory

Use as reference for:

- deterministic orchestration owns the graph;
- coding agents are bounded nodes, not the authority controlling the system;
- tools and permissions are separate concerns;
- a reviewer can be made physically read-only even if its prompt misbehaves;
- traces/specs/artifacts are first-class outputs.

Migration target in SynFactory:

- `internal/policy` capability enforcement;
- runtime command allowlists;
- post-run mutation checks;
- workflow graph owned by Go code rather than model reasoning.

Observed license during bootstrap research: MIT.

### OpenHands

Core/UI repository: https://github.com/All-Hands-AI/OpenHands
Automation service: https://github.com/OpenHands/automation
Extensions: https://github.com/OpenHands/extensions

Use as reference for:

- separation between automation scheduling/webhooks and agent execution;
- cron plus event/webhook trigger model;
- run history and dispatch;
- custom webhook registration/signature model;
- agent-server/runtime boundary;
- repository cloning into execution environments;
- plugin/skill extension model.

Migration target in SynFactory:

- webhook + periodic reconciliation dual path;
- service boundaries that can later be split across EC2 instances;
- runtime execution contract;
- extension points without coupling the control plane to one agent.

### MetaGPT

Repository: https://github.com/FoundationAgents/MetaGPT

Secondary reference only.

Study role decomposition (PM/architect/engineer/etc.) and artifact handoffs. Do not use its "one requirement -> virtual team -> generated project" flow as the core runtime model because SynFactory is a persistent operations system driven by live repository state.

### ChatDev

Repository: https://github.com/OpenBMB/ChatDev

Secondary reference only.

Study role conversations and software-company abstractions. Avoid conversation-first orchestration as the system of record; SynFactory state must remain deterministic and queryable in PostgreSQL/GitHub.

## SynFactory synthesis

| Capability | Main reference | SynFactory implementation |
| --- | --- | --- |
| Continuous 24/7 operation | Vanguard | Go worker pool + durable jobs |
| Near-real-time repository reaction | eve / OpenHands | GitHub webhook inbox |
| Missed-event recovery | OpenHands + control-plane pattern | hourly repository reconciliation |
| Durable workflow state | Paddock / Miniforge | PostgreSQL state machine |
| Failure budgets | Miniforge | policy-driven attempts/time/cost budgets |
| Runtime/provider abstraction | Vanguard | Go `runtime.Adapter` interface |
| Deterministic permissions | SSSF | role capability enforcement outside prompts |
| Isolated implementation | Vanguard/Paddock | worktree and Docker workspace adapters |
| Independent review | eve | separate role/session/runtime policy |
| Host-owned verification | Vanguard | command verifier + evidence records |
| Multi-project hierarchy | Paddock | facility/product/repository/department model |
| Scheduling + webhooks | OpenHands | scheduler + webhook service |
| Optional dashboard | Paddock/OpenHands | Vue 3 consuming Go API |

## What will *not* be migrated

- upstream UI frameworks merely because an upstream has a UI;
- framework-specific dependency injection/container systems;
- SQLite schemas that encode one upstream's implementation choices;
- vendor-specific deployment assumptions;
- prompts used as security boundaries;
- infinite self-repair loops;
- direct coupling between a role and one model/CLI;
- agent-owned truth that cannot be reconciled with GitHub/PostgreSQL.

## Clone/update upstreams

Run from the SynFactory repository on a machine with outbound GitHub access:

```bash
bash scripts/clone-upstreams.sh
```

The clones are intentionally ignored by Git and must not be committed into SynFactory.
