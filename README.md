# SynFactory

SynFactory is a self-hosted, always-on software factory: a Go control plane for running an autonomous IT department across GitHub repositories.

## Core principles

- **Go-native control plane.** API, scheduler, reconciler, dispatcher, workers, workflow state machines and runtime adapters are Go.
- **Vue only for the optional web UI.** The dashboard is an operator surface; workflow authority remains in Go and headless operation remains supported.
- **Hybrid triggers.** GitHub webhooks are the fast path; periodic reconciliation (default: hourly) is the recovery/correctness path.
- **GitHub is product-work truth.** Issues, PRs, reviews, checks, branches and commits are projected into durable workflow state.
- **Pluggable external agent runtimes.** Codex, Cursor, Antigravity, Claude Code, OpenCode and OpenAI-compatible endpoints sit behind one runtime interface.
- **Roles are not runtimes.** PM, Team Lead, Developer, Reviewer, QA, CI Guardian and Release roles may use different runtime/model fallback chains.
- **Failure must not stall the factory.** Job retries, CI/review repair cycles and escalation paths are bounded; blocked work releases capacity.
- **Deterministic authority boundaries.** Prompts describe role behavior; Go code owns permissions, WIP, verification, exact-head merge gates and issue lifecycle.
- **Durable idempotency and leases.** Webhook/reconciliation duplicates, concurrent workers and restarts converge through PostgreSQL state.
- **Self-host first.** Docker Compose on EC2/VPS is the initial operations target; workers can later move to separate hosts without changing domain contracts.
- **Mobile-first operator experience.** Vue surfaces must work from narrow touch devices first and progressively enhance for desktop operations.

## Current architecture

```text
                      +---------------- GitHub hourly reconciliation
                      |
GitHub webhook        v
      |          durable event inbox
      |                 |
      +-----------------+
                        v
                Workflow Coordinator
                        |
       +----------------+--------------------+
       |                |                    |
       v                v                    v
      PM/TL          Developer        Reviewer / CI Guardian
       |                |                    |
       +----------------+--------------------+
                        |
                   durable jobs
                        |
                        v
                  Worker leases
                        |
                        v
                Runtime Registry
      +---------+--------+------+--------+---------+
      |         |        |      |        |         |
    Codex    Cursor    Agy   Claude  OpenCode   OpenAI-compatible
                        |
                        v
             worktree / Docker isolation
                        |
                        v
              deterministic verifier
                        |
                        v
                evidence + GitHub
```

PostgreSQL is the durable source of execution/workflow truth. Agent output cannot authorize its own merge. Team Lead/reviewer handoffs are persisted and the control plane performs GitHub mutations against the expected PR head SHA.

## Process modes

The same `synfactory` binary supports:

```text
synfactory api        # webhook + health/metrics
synfactory scheduler  # inbox + reconcile + workflow + lease recovery
synfactory worker     # runtime execution + isolation + verification
synfactory all        # all components in one process
synfactory migrate    # migrations then exit
synfactory check      # PostgreSQL connectivity then exit
```

Docker Compose runs API, scheduler and worker as separate services so control-plane and execution workloads can scale independently.

## Quick start

```bash
cp .env.example .env
cp config/runtimes.example.json config/runtimes.local.json
chmod 600 .env config/runtimes.local.json

# Edit .env first: database password/URL, operator + webhook secrets,
# GitHub auth mode/credentials, domain, and absolute repository/workspace paths.
# Create the configured repository/workspace directories before launch.
bash scripts/preflight.sh

docker compose --profile local-db build
docker compose --profile local-db up -d

curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/metrics
```

`preflight.sh` rejects example placeholders, invalid GitHub auth configuration, missing/unwritable storage roots, invalid runtime configuration and invalid Compose configuration before a production launch. A host-local OpenAI-compatible service such as 9router must be addressed as `host.docker.internal` from the Compose worker, not `127.0.0.1`.

For RDS/managed PostgreSQL, point `SYNFACTORY_DATABASE_URL` at the external service and start without `--profile local-db`.

See **[`docs/operations.md`](docs/operations.md)** for CLI authentication, private-repository cloning, Caddy/webhook setup, Docker sandbox path requirements, backup/restore, upgrades and split-host deployment. See **[`docs/github-app-auth.md`](docs/github-app-auth.md)** for production GitHub App configuration and PAT migration. See **[`docs/roadmap.md`](docs/roadmap.md)** for the product maturity model, active P0 work and the long-term autonomy/human-control/cost/fleet/portfolio roadmap.

## Repository layout

```text
cmd/synfactory/       process entrypoint and mode wiring
internal/config/      environment/process configuration
internal/domain/      durable job/role domain types
internal/events/      logical event identity
internal/github/      webhook, REST client, reconciliation and mutations
internal/orchestrator GitHub truth projection, runtime request/governance handoff
internal/operations/  operational metrics handlers
internal/postgres/    durable stores, leases, migrations, metrics queries
internal/repository/  worker repository clone/fetch preparation
internal/runtime/     CLI/OpenAI adapters, supervisor, fallback and sandbox command wrapping
internal/verifier/    host-owned deterministic verification
internal/worker/      durable worker executor
internal/workflow/    deterministic software-department workflow engine
internal/workspace/   worktree/Docker isolation
migrations/           embedded PostgreSQL schema migrations
config/               runtime configuration examples
deploy/               reverse-proxy/deployment configuration
docs/                 architecture, ADRs, research, operations and roadmap
scripts/              preflight, backup/restore and helper tooling
web/                  optional Vue 3 operator control center
```

## Runtime configuration

`config/runtimes.example.json` demonstrates role-specific fallback chains. The worker image intentionally does not pin vendor coding-agent versions; install/login the desired CLI in the persistent worker home or mount a standalone binary, then point the runtime config at it.

A host-local OpenAI-compatible endpoint such as 9router should use `host.docker.internal` from a Compose worker, not `127.0.0.1`.

## Operational endpoints

The API exposes:

- `GET /healthz` — liveness;
- `GET /readyz` — PostgreSQL readiness;
- `GET /ops` — JSON queue/lease/workflow/worker state;
- `GET /metrics` — Prometheus-format operational gauges;
- `POST /webhooks/github` — signed GitHub webhook intake;
- versioned authenticated operator APIs used by the Vue control center and managed-repository lifecycle.

Caddy publishes only webhook/health routes by default; operational/operator endpoints remain behind the private/authenticated deployment boundary.

## Backup and restore

```bash
bash scripts/backup.sh
bash scripts/restore.sh backups/synfactory-YYYYMMDDTHHMMSSZ.tar.gz
```

Backups contain PostgreSQL durable state, optional runtime config and SHA-256 manifest. Secrets and CLI login credentials are deliberately excluded and must be protected separately.

## Upstream research

SynFactory is a clean Go implementation that synthesizes proven ideas rather than mechanically translating another project. Research targets include Miniforge, Vanguard, Paddock, Vercel eve Software Factory, Super Simple Software Factory, OpenHands, MetaGPT and ChatDev. See `docs/upstream-research.md` and `scripts/clone-upstreams.sh`.

## Delivery status

- Durable PostgreSQL execution/leases/recovery: **implemented**.
- GitHub webhook + reconciliation correctness loop: **implemented**.
- Pluggable CLI/OpenAI runtime engine: **implemented**.
- Worktree/Docker isolation + deterministic verification: **implemented**.
- PM/TL/Dev/Reviewer/CI Guardian workflow engine: **implemented**.
- 24/7 self-hosted operations/deployment: **implemented**.
- Vue 3 authenticated operator control center baseline: **implemented**.
- Managed repository onboarding/lifecycle: **implemented**.
- Repository-scoped GitHub App authentication with explicit PAT fallback: **implemented**.
- Launch-readiness/preflight hardening: **implemented**.
- Release vulnerability/SBOM/evidence gates: **implemented**.
- Immutable OCI release publishing/promotion/rollback: **implemented**.
- Secure EC2/SSH-style operator terminal: **in progress on issue #29**.
- Mobile-first adaptive control center: **planned on issue #31**.
- Production autonomy soak, multi-user control, cost governance, distributed worker fleet, secrets operations, portfolio planning and later intelligence/provider work: **tracked in `docs/roadmap.md`**.
