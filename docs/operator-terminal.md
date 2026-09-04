# Operator terminal

SynFactory's operator terminal is an explicit manual-operations escape hatch for authenticated operators. It is not an agent runtime and it is not a second workflow-control path.

## Security boundary

Terminal access is disabled by default and stays on the same private operator surface as the control center. The public Caddy/webhook hostname must not expose terminal endpoints. Opening a shell requires the existing operator bearer authentication plus explicit terminal enablement.

The default local target runs as the SynFactory service user, never as root. Remote targets use the host OpenSSH client with `StrictHostKeyChecking=yes`, an explicit `UserKnownHostsFile`, an explicit identity file, `IdentitiesOnly=yes`, and password/keyboard-interactive authentication disabled. Private keys, passwords, terminal input and terminal output are not persisted as workflow evidence or normal application logs.

The browser first creates a session through the authenticated REST API. The response contains a cryptographically random, one-time stream ticket valid for 30 seconds. Browser WebSocket attachment sends that ticket in the `Sec-WebSocket-Protocol` header as `synfactory-terminal.<ticket>` rather than placing the operator token or stream ticket in the URL, reducing credential exposure through normal URL/access logging. The ticket is bound to one session and consumed once.

SynFactory currently keeps session state in memory and may expose only non-secret metadata needed for operation/capacity: session ID, target ID/kind and start/last-I/O timestamps. Raw terminal I/O is intentionally excluded from durable run/evidence storage.

## Session/API contract

The Go control plane owns create, attach, input/output, resize and close semantics. Session policy enforces both deployment-wide and per-target capacity, idle timeout and maximum lifetime. Disconnect, timeout and shutdown paths terminate the owned PTY/process tree and release capacity.

Private operator endpoints:

- `GET /api/v1/terminal/targets` — safe configured target projection; credential-file paths are not returned;
- `GET /api/v1/terminal/sessions` — active non-secret session metadata;
- `POST /api/v1/terminal/sessions` — open a session and issue a one-time stream ticket;
- `DELETE /api/v1/terminal/sessions/{id}` — explicitly close the owned session;
- `GET /api/v1/terminal/sessions/{id}/stream` — WebSocket upgrade using the one-time subprotocol ticket.

WebSocket binary frames are terminal input; text control messages support `input` and `resize`; PTY output is streamed as binary frames. Ping/pong and disconnect cleanup are handled by the Go endpoint.

Backends implement one contract:

- `local`: PTY on the current API/control host;
- `ssh`: PTY through an explicitly configured remote SSH target with strict host-key verification.

A future worker-side outbound transport may implement the same backend/session contract for networks where inbound SSH is not possible.

## Target configuration

Copy `config/terminal-targets.example.json` to an operator-owned file such as `config/terminal-targets.json`. Local targets may specify only an absolute working directory and absolute shell path. SSH targets require host/user plus absolute in-container paths for the identity file and known-hosts file; unknown fields and duplicate target IDs are rejected.

For Docker Compose, keep the normal stack unchanged while terminal mode is disabled. When terminal access is deliberately enabled, use the opt-in overlay:

```sh
docker compose -f compose.yaml -f compose.terminal.yaml --profile local-db up -d --build
```

The overlay mounts `${SYNFACTORY_TERMINAL_TARGETS_HOST}` read-only at `/etc/synfactory/terminal-targets.json` and `${SYNFACTORY_TERMINAL_SSH_DIR_HOST}` read-only under `/run/secrets/synfactory-terminal`. Ensure key/known-hosts files are readable by the non-root SynFactory control UID (`10001`) without making private keys broadly writable/readable. The terminal-capable control image includes `openssh-client`; the worker image inherits it.

## Web control center

The Vue control center will use an xterm-compatible presentation layer and the authenticated WebSocket stream. Vue does not decide target authorization, shell identity, session limits or privilege level. It only requests and renders sessions authorized by Go.

The UI must follow the mobile-first layout contract in `docs/control-center-layout.md`: phone operation is first-class, session controls remain touch-safe, terminal space fits the dynamic visual viewport, and virtual-keyboard/orientation changes resize the PTY without losing the session.

## Operational defaults

Recommended production defaults:

- disabled until explicitly configured;
- 2 concurrent sessions deployment-wide;
- 1 concurrent session per target;
- 15 minute idle timeout;
- 2 hour maximum session lifetime;
- local shell working directory restricted to configured operator-safe roots;
- no root shell and no insecure SSH host-key bypass.

Configuration is explicit through `SYNFACTORY_TERMINAL_*`; disabling terminal mode opens no PTY and does not require terminal target/SSH mount files.
