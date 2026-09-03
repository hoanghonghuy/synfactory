# SynFactory operations and self-hosted deployment

This document describes the production operating model for a single EC2/VPS first and a split control-plane/worker topology later.

## Process model

One `synfactory` binary supports explicit process modes:

| Mode | Responsibility |
| --- | --- |
| `api` | GitHub webhook intake, liveness/readiness, `/ops`, `/metrics` |
| `scheduler` | durable inbox processing, GitHub reconciliation, workflow coordination, stale-lease recovery |
| `worker` | repository preparation, durable job execution, runtime fallback, isolation, governance handoff, verification |
| `all` | API + scheduler + workers in one process; useful for development or a capable single host |
| `migrate` | apply migrations under the PostgreSQL advisory migration lock, then exit |
| `check` | verify PostgreSQL connectivity, then exit |

Docker Compose runs API, scheduler and worker separately. All processes share PostgreSQL; there is no in-memory workflow state required for restart recovery.

## One-box quick start with local PostgreSQL

1. Prepare configuration:

   ```bash
   cp .env.example .env
   cp config/runtimes.example.json config/runtimes.local.json
   chmod 600 .env config/runtimes.local.json
   ```

2. Edit `.env` and set at least:
   - `POSTGRES_PASSWORD` and matching password in `SYNFACTORY_DATABASE_URL`;
   - `SYNFACTORY_DOMAIN`;
   - `SYNFACTORY_GITHUB_TOKEN`;
   - `SYNFACTORY_GITHUB_WEBHOOK_SECRET`;
   - absolute `SYNFACTORY_REPOSITORY_ROOT` and `SYNFACTORY_WORKSPACE_ROOT` paths.

3. Create worker persistence directories. The repository/workspace paths must be the same absolute paths on the host and in the worker container. This is required because Docker sandbox commands are executed through the host Docker socket and the host daemon must resolve the workspace bind path.

   ```bash
   sudo mkdir -p /opt/synfactory/data/repos /opt/synfactory/data/workspaces
   mkdir -p data/agent-home data/agent-bin
   ```

4. Build and start:

   ```bash
   docker compose --profile local-db build
   docker compose --profile local-db up -d
   docker compose --profile local-db ps
   ```

5. Verify:

   ```bash
   curl -fsS http://127.0.0.1:8080/readyz
   curl -fsS http://127.0.0.1:8080/metrics
   ```

Caddy exposes only the webhook and health endpoints on the public hostname. `/metrics` and `/ops` remain on the host-only API listener by default.

## Managed PostgreSQL / RDS

Set `SYNFACTORY_DATABASE_URL` to the managed PostgreSQL endpoint and start without the local-db profile:

```bash
docker compose build
docker compose up -d api scheduler worker caddy
```

The bundled PostgreSQL service is profile-gated and will not start. API, scheduler and workers all support the same external `DATABASE_URL`.

## GitHub webhook

Configure the repository/GitHub App webhook target as:

```text
https://<SYNFACTORY_DOMAIN>/webhooks/github
```

Use the same random secret in GitHub and `SYNFACTORY_GITHUB_WEBHOOK_SECRET`. Caddy terminates HTTPS and proxies the webhook to the API service. If webhooks are unavailable temporarily, the scheduler's GitHub reconciliation path still converges state on `SYNFACTORY_RECONCILE_INTERVAL` (default one hour).

## Worker CLI installation and authentication

The worker image contains Git, GitHub CLI, Docker CLI, Node.js and npm, but deliberately does not pin vendor coding-agent versions. SynFactory invokes coding agents as external CLI processes through runtime adapters.

Two supported installation patterns are:

1. **Persistent CLI home** — install/login inside the worker using a path under `/root`, which is backed by `SYNFACTORY_AGENT_HOME`.
2. **Mounted standalone binaries** — place binaries in `SYNFACTORY_AGENT_BIN`; Compose adds `/opt/synfactory/agent-bin` to `PATH`.

For npm-based CLIs, install them into the persisted home rather than `/usr/local`:

```bash
docker compose exec worker npm config set prefix /root/.local
docker compose exec worker <vendor installation command>
```

`/root/.local/bin` is on the worker `PATH`.

For private GitHub repository cloning, authenticate once and configure Git credentials:

```bash
docker compose exec worker gh auth login
docker compose exec worker gh auth setup-git
```

The `/root` volume preserves the GitHub CLI and agent CLI credential state across worker container restarts.

For Codex, Antigravity, Claude Code, Cursor, OpenCode or future Copilot CLI integrations, use the vendor-supported login/API-key flow and point `config/runtimes.local.json` at the installed binary. SynFactory remains responsible for process lifecycle, permissions, leases, verification, evidence and merge authorization.

### Host services such as 9router

Inside the worker container, `127.0.0.1` refers to the worker itself. Compose maps `host.docker.internal` to the Docker host. A host-local OpenAI-compatible service should therefore use a runtime base URL such as:

```text
http://host.docker.internal:20128/v1
```

## Repository source lifecycle

Before a job is executed, the worker source manager:

- uses `config.local_path` when the repository explicitly supplies a managed local checkout; otherwise
- resolves the path under `SYNFACTORY_REPOSITORY_ROOT`;
- clones missing GitHub repositories with `git clone --filter=blob:none --no-checkout`;
- fetches existing repositories with `git fetch --prune origin`;
- serializes source preparation per repository inside one worker process.

Authentication is delegated to the worker's Git credential configuration; access tokens are never embedded in clone URLs by SynFactory.

## Worktree and Docker sandbox modes

`worktree` is the low-overhead default and executes the CLI on the worker host/container while SynFactory owns the task worktree and mutation checks.

`docker` provides stronger isolation. The repository execution config must provide a `container_image`. Because the coding CLI itself is wrapped by the Docker sandbox, that image must contain the selected CLI and the build/test tools required by the repository. The worker Docker socket is root-equivalent and is intentionally isolated to the worker service; API and scheduler remain non-root and do not mount the socket.

## Health and observability

API endpoints:

- `GET /healthz` — process liveness;
- `GET /readyz` — PostgreSQL readiness;
- `GET /ops` — JSON operational snapshot;
- `GET /metrics` — Prometheus text metrics.

Metrics currently include:

- queued/retry jobs;
- active jobs;
- terminal failed jobs;
- stale job leases;
- pending durable inbox events;
- blocked and parked workflows;
- live and stale/draining workers.

Examples:

```bash
curl -fsS http://127.0.0.1:8080/ops | jq
curl -fsS http://127.0.0.1:8080/metrics
```

All application logs use structured JSON through `slog`. Set `SYNFACTORY_LOG_LEVEL=debug|info|warn|error`.

## Failure and restart behavior

- Jobs/events/workflows are durable in PostgreSQL.
- Worker ownership uses leases and heartbeats.
- Scheduler lease recovery requeues or fails abandoned work according to retry budget.
- Multiple services may start simultaneously: migrations are serialized with a PostgreSQL advisory lock.
- Unexpected scheduler/worker component exit terminates that service process so `restart: unless-stopped` can recover it.
- CI/review repair budgets are workflow state, so process restarts do not reset them.

## Backup

Secrets are intentionally excluded from backup bundles. Keep `.env`, GitHub App credentials, webhook secret and CLI login material in a separate secret-management/host-backup process.

Run:

```bash
bash scripts/backup.sh
```

The resulting `backups/synfactory-<UTC>.tar.gz` contains:

- PostgreSQL custom-format `database.dump`;
- `runtimes.json` when a local runtime config exists;
- SHA-256 manifest;
- recovery notes.

For local Compose PostgreSQL, the script dumps through the running PostgreSQL container. For managed PostgreSQL it uses host `pg_dump` and `SYNFACTORY_DATABASE_URL`.

## Restore

Stop API/scheduler/workers first so no work is mutated during restore:

```bash
docker compose stop api scheduler worker
bash scripts/restore.sh backups/synfactory-YYYYMMDDTHHMMSSZ.tar.gz
docker compose up -d api scheduler worker caddy
```

Restore verifies the SHA-256 manifest before touching the database and uses `pg_restore --clean --if-exists --single-transaction`. A backed-up runtime config is written to `config/runtimes.restored.json` for manual review rather than silently overwriting production configuration.

## Upgrade runbook

1. Create and verify a backup.
2. Pull the target revision/image source.
3. Build the new images.
4. Apply migrations explicitly:

   ```bash
   docker compose run --rm scheduler migrate
   ```

   Migration locking makes this safe even if another SynFactory process starts concurrently.

5. Restart services:

   ```bash
   docker compose up -d --build api scheduler worker caddy
   ```

6. Check `/readyz`, `/ops`, `/metrics`, scheduler logs and worker heartbeats.
7. Keep the previous image/revision available until the new services have processed at least one reconciliation cycle successfully.

## Split-host deployment

For larger workloads:

- host A: Caddy + API + scheduler;
- managed PostgreSQL/RDS: durable state;
- host B/C/...: one or more worker processes.

Every worker points at the same database and has its own `SYNFACTORY_WORKER_ID`, repository/workspace roots, runtime config and CLI credentials. No workflow/domain changes are required when workers move to another host.

Do not run multiple heavy coding-agent workers on a very small control-plane instance. A low-memory VPS can host API/scheduler/PostgreSQL for light use, while build/coding workers should be placed on hosts sized for the repositories and agent CLIs they execute.
