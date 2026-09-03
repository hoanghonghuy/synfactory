# Agent runtime engine

SynFactory owns orchestration policy; Codex, Cursor, Antigravity, Claude Code, OpenCode and OpenAI-compatible endpoints are replaceable execution adapters.

## Boundaries

- Workflow/domain code selects a role and permissions. It never emits vendor CLI flags.
- Runtime configuration maps each role to an ordered adapter/model chain.
- The registry probes a candidate before execution and only falls back for configured failure classes.
- A successful result is terminal for that execution. Later candidates are never started after success.
- Each provider fallback within one job attempt has its own durable `runs.sequence`, so audits preserve exactly which runtime was tried and in what order.
- Runtime stdout/stderr is bounded and redacted before it can enter a result, run summary or evidence record.
- Runtime cancellation is keyed by SynFactory run ID. CLI processes execute in their own process group so cancellation/timeout terminates descendants as well as the parent CLI.

## Runtime configuration

Runtime configuration is JSON and is validated with unknown-field rejection. See `config/runtimes.example.json`.

Secrets are referenced by environment-variable name (`api_key_env` / `secret_env`); secret values do not belong in the JSON file. `secret_env` values are used for output redaction. OpenAI-compatible direct HTTP adapters read their bearer token from `api_key_env`.

A role route looks like:

```json
{
  "developer": {
    "chain": [
      {"runtime": "cursor-primary"},
      {"runtime": "codex-primary"}
    ],
    "fallback_on": ["unavailable", "transient", "timeout"]
  }
}
```

Default fallback classes are `unavailable` and `transient`. Permanent failures and explicit cancellation never fall through by default.

## Permission mapping

SynFactory permissions remain provider-independent:

- `repo:read`
- `repo:write`
- `command:run`
- `pr:review`
- `pr:merge`

Adapters translate only the minimum relevant subset. For example, Codex receives a read-only sandbox without `repo:write`, and Cursor uses ask mode for read-only runs. Dangerous vendor auto-approval flags are emitted only when the runtime configuration explicitly sets `auto_approve` and the SynFactory request grants write permission.

This is still secondary containment. Filesystem/Docker isolation is owned by the workspace layer, not by model prompts or vendor permission modes.

## Failure semantics

Normalized failure classes are:

- `unavailable`: binary/auth/provider unavailable; eligible for fallback by default.
- `transient`: rate limit, temporary provider/network failure; eligible for fallback by default.
- `timeout`: SynFactory execution deadline expired.
- `canceled`: owner/operator cancellation; never silently falls through.
- `permanent`: invalid request/provider-specific deterministic failure; no fallback unless policy is deliberately extended in code.

Job retries remain a separate bounded budget in the durable job state machine. Provider fallback does not increment the job attempt; it increments `runs.sequence` inside that attempt.

## Session resume

Where supported, `Request.Metadata["resume_session_id"]` routes the request to the adapter's resume path. Current adapters support session IDs for Codex, Cursor, Antigravity, Claude Code, OpenCode, and Responses-style OpenAI-compatible APIs (using `previous_response_id`).

Session IDs are provider-owned opaque strings. Workflow code must never parse them.

## Worker execution bridge

`internal/worker` provides the durable bridge between a leased job and the runtime registry:

1. claim and start a job;
2. load repository data and build a provider-independent request through `RequestBuilder`;
3. renew the job lease while an agent is running;
4. persist each runtime fallback attempt before execution;
5. finish the run and store normalized runtime evidence;
6. mark the job succeeded or schedule its bounded retry.

The request builder is intentionally an interface. Role-specific prompts/workflow transitions belong to the workflow engine rather than the runtime adapter layer.
