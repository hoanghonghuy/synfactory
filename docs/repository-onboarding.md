# Repository onboarding and lifecycle

SynFactory treats repository configuration as managed operator state. Scheduling consumes only enabled repositories; onboarding and lifecycle changes are made through the authenticated operator API and are audit logged with a monotonically increasing `config_version`.

## Register a repository

Use `POST /api/v1/repository-config` with the same bearer token as the control center. `full_name` must be `owner/repository`. `default_branch` defaults to `main` and `integration_branch` defaults to `develop` when omitted. SynFactory validates both branches through the configured GitHub client before an enabled repository is persisted.

Example payload:

```json
{
  "full_name": "acme/service",
  "default_branch": "main",
  "integration_branch": "develop",
  "workspace_policy": "managed",
  "enabled": true
}
```

Registration is safe to retry for the same logical repository. Repository identity is deterministic and `full_name` is immutable after registration so scheduler history and audit rows cannot silently move to a different remote.

## Update, disable and re-enable

Use `PATCH /api/v1/repository-config/{id}`. Branch and workspace settings may be updated. Setting `enabled` to `false` removes the repository from scheduling input without deleting its history or configuration. Re-enabling performs GitHub branch validation again before the mutation is accepted.

Disable a repository before maintenance, credential rotation, repository archival, or any situation where new autonomous work must stop. Disabling does not cancel work already executing; operators must inspect active jobs/workflows in the control center and drain workers when a hard stop is required.

## Audit and recovery

`GET /api/v1/repository-config/{id}/audit` returns the repository configuration mutation history. Use it to identify the last known-good `config_version`, actor and action before recovery. Recovery should be performed by an explicit PATCH restoring the desired configuration; do not edit database rows manually because that bypasses versioning and audit evidence.

When GitHub validation fails, verify token access and that both configured branches exist. Keep the repository disabled until validation succeeds. When a repository is renamed upstream, register the new `owner/repository` as a distinct managed repository rather than mutating `full_name`.

## Operator UI contract

The web control center uses the same endpoints through `OperatorApi.repositoryConfigs`, `registerRepository`, `updateRepository`, and `repositoryAudit`. The UI must surface validation errors returned by the API and must not optimistically show an enable/register mutation as successful before the server response is received.
