# GitHub App authentication

SynFactory supports two explicit GitHub credential modes:

- `app` — recommended for production. Each managed repository is resolved to the GitHub App installation that owns access to that repository, and SynFactory mints short-lived installation tokens in memory.
- `pat` — compatibility mode for simple or self-hosted deployments. SynFactory uses `SYNFACTORY_GITHUB_TOKEN` exactly as before.

The modes are intentionally exclusive. SynFactory does not silently fall back from a failing App installation to a PAT because that would hide installation or permission drift.

## Configuration

For GitHub App mode set:

```text
SYNFACTORY_GITHUB_AUTH_MODE=app
SYNFACTORY_GITHUB_APP_ID=<numeric app id>
SYNFACTORY_GITHUB_APP_PRIVATE_KEY_HOST=./secrets/synfactory-github-app.pem
SYNFACTORY_GITHUB_APP_PRIVATE_KEY_FILE=/run/secrets/synfactory-github-app.pem
SYNFACTORY_GITHUB_API_URL=https://api.github.com
```

`SYNFACTORY_GITHUB_APP_PRIVATE_KEY_HOST` is the host-side PEM source. The stock Compose App override mounts that file read-only at the fixed in-container path `/run/secrets/synfactory-github-app.pem`, which must match `SYNFACTORY_GITHUB_APP_PRIVATE_KEY_FILE`. Protect the host PEM with restrictive permissions and never put its contents in environment variables, repository configuration, PostgreSQL, logs or support evidence.

Validate configuration before starting:

```bash
bash scripts/preflight.sh
```

Then launch App mode with both Compose files:

```bash
docker compose -f compose.yaml -f compose.github-app.yaml --profile local-db build
docker compose -f compose.yaml -f compose.github-app.yaml --profile local-db up -d
```

For an external PostgreSQL service, omit `--profile local-db`. API, scheduler and worker all receive the same read-only App private-key mount; web and Caddy do not receive it.

For PAT compatibility mode set:

```text
SYNFACTORY_GITHUB_AUTH_MODE=pat
SYNFACTORY_GITHUB_TOKEN=<fine-grained token>
```

PAT mode uses the normal `compose.yaml` stack and does not mount an App private key. A PAT-less `pat` scheduler leaves GitHub reconciliation/workflow coordination disabled. Workers fail fast without GitHub authentication because governance mutations require authenticated GitHub access.

## Installation selection and rotation

For every repository-scoped request SynFactory resolves `GET /repos/{owner}/{repo}/installation` with an App JWT. The resulting installation ID is cached as non-secret metadata. Installation access tokens are never persisted and are refreshed five minutes before expiry.

Concurrent token callers share the same refresh path. If GitHub returns `401` for a repository API request, SynFactory invalidates the cached repository installation/token and performs exactly one rediscovery + token refresh + request retry. A second authentication failure is returned to the workflow instead of looping.

`404` and permission-denied installation discovery/token-mint failures are classified as permanent installation/configuration failures. Operators should verify that the App remains installed on the repository owner and that the repository is included in the installation's selected repositories.

## Minimum permissions and events

Grant only the permissions SynFactory uses. The current production workflow needs repository metadata read access plus the capabilities represented by SynFactory's managed actions: repository contents for branch/source inspection, issues read/write, pull requests read/write, checks/statuses read, and Actions/workflow-run read where CI state is inspected. If SynFactory is configured to update workflow files, GitHub may require the corresponding workflows permission as well.

Webhook delivery should include the event families consumed by SynFactory's event router and reconciliation model: issues, issue comments, pull requests, pull-request reviews/review comments, checks/check suites, statuses and repository lifecycle changes. Keep the existing webhook signature secret and validation enabled; App authentication does not replace webhook HMAC verification.

Review permissions against the actual enabled SynFactory capabilities before production rollout. Do not grant organization administration or unrelated write permissions.

## Migration from PAT to GitHub App

1. Create the GitHub App with the minimum permissions/events above and install it on the repositories SynFactory manages.
2. Generate a private key, store it as a host/secret-manager file, set `SYNFACTORY_GITHUB_APP_PRIVATE_KEY_HOST`, and restrict its permissions.
3. Set App ID, the fixed in-container private-key path and `SYNFACTORY_GITHUB_AUTH_MODE=app`; keep the PAT configured only outside the running process if rollback is desired.
4. Run `bash scripts/preflight.sh`, then restart API/scheduler/worker using `compose.yaml` plus `compose.github-app.yaml`.
5. Validate repository activation/onboarding. Missing installations fail with repository-scoped, non-secret errors.
6. Confirm reconciliation, workflow reads/mutations and governance actions operate on repositories belonging to different installations.
7. Remove the long-lived PAT from the deployment once App mode is verified.

Rollback is explicit: set `SYNFACTORY_GITHUB_AUTH_MODE=pat`, restore the PAT secret and restart with normal `compose.yaml`. Do not configure an automatic fallback path.

## Security properties

- App private keys are read from a read-only file mount and are never returned by operator APIs.
- Installation access tokens exist in memory only and are not stored in PostgreSQL or evidence.
- Installation IDs may be cached because they are identifiers, not credentials.
- GitHub error reporting is reduced to status/repository/actionable message; SynFactory must never log Authorization headers or token response bodies.
- Authentication refresh is bounded to one retry per failed repository request.
