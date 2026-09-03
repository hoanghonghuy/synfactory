# SynFactory Control Center

The control center is an optional Vue 3 operator surface over the Go control plane. It does not own workflow truth, merge authorization, retry budgets, task deduplication, or job state transitions. SynFactory remains fully usable without the web service.

## Security model

The Go operator API is disabled unless `SYNFACTORY_OPERATOR_TOKEN` is configured. Every `/api/v1/*` request requires:

```http
Authorization: Bearer <operator-token>
```

Use a dedicated high-entropy token. Do not reuse the GitHub token, webhook secret, or any coding-agent API key.

API responses use explicit read DTOs and intentionally omit repository execution config, raw event payloads, runtime metadata/session IDs, worker metadata, credential environments, and evidence metadata that may contain normalized runtime output.

The production Compose topology binds the web service to `127.0.0.1:8081` by default. The public Caddy hostname continues to expose only GitHub webhook and health routes. This makes the control center private by default rather than relying only on bearer authentication.

## Access on the host

With the default Compose configuration:

```bash
docker compose up -d api scheduler worker web caddy
curl http://127.0.0.1:8081/
```

Open `http://127.0.0.1:8081` in a browser on the host and enter the configured operator token. The Vue app stores it in `sessionStorage`, so closing the browser session removes it.

## Remote access with SSH

Prefer an SSH tunnel or private VPN instead of exposing the dashboard publicly:

```bash
ssh -L 8081:127.0.0.1:8081 user@factory-host
```

Then open:

```text
http://127.0.0.1:8081
```

If a private reverse proxy/VPN is added later, keep `/api/v1` protected and preserve the Go bearer check as defense in depth.

## Operator views

The initial dashboard exposes read-only operational state:

- Overview: queue depth, active jobs, blocked/parked workflows, event backlog and worker health.
- Workflows: exact revision, state, blocker reason, CI/review repair budgets, actions and state history.
- Jobs: role/action, attempt budget, lease ownership and terminal failure reason.
- Runs & evidence: runtime/model attempt history, normalized summary and deterministic evidence fingerprints.
- Repositories: configured enabled repositories without returning private execution config.
- Workers: host, heartbeat, capacity and draining/stale state.

The UI polls every five seconds only while the document is visible. Polling is a presentation refresh mechanism; PostgreSQL and GitHub reconciliation remain workflow truth.

## Operator API

Versioned endpoints:

```text
GET /api/v1/overview
GET /api/v1/repositories
GET /api/v1/repositories/{id}
GET /api/v1/jobs?limit=50&offset=0&status=&repository_id=
GET /api/v1/jobs/{id}
GET /api/v1/workflows?limit=50&offset=0&state=&repository_id=
GET /api/v1/workflows/{id}
GET /api/v1/runs?limit=50&offset=0&job_id=
GET /api/v1/runs/{id}
GET /api/v1/runs/{id}/evidence
GET /api/v1/workers
```

List limits are clamped to 200. Responses use `Cache-Control: no-store`.

## Token rotation

1. Generate a new random token.
2. Replace `SYNFACTORY_OPERATOR_TOKEN` in the secret-managed `.env`.
3. Restart only the API process:

```bash
docker compose up -d --no-deps --force-recreate api
```

4. Existing browser sessions will receive `401`; the UI locks itself and requires the new token.

No durable SynFactory state is affected by operator-token rotation.

## Headless deployment

The `web` service is optional. These remain valid:

```bash
docker compose up -d api scheduler worker caddy
```

or split-host API/scheduler/worker deployments described in `docs/operations.md`. Removing the Vue service changes only observability UX, not execution behavior.

## Future control actions

If pause/drain/retry/cancel or other operator mutations are added, each must be a versioned Go endpoint that re-validates role/invariant/state constraints transactionally. The Vue application must never update workflow/job tables directly or encode an independent transition table.
