# Operator terminal

SynFactory's operator terminal is an explicit manual-operations escape hatch for authenticated operators. It is not an agent runtime and it is not a second workflow-control path.

## Security boundary

Terminal access is disabled by default and must stay on the same private operator surface as the control center. The public Caddy/webhook hostname must not expose terminal endpoints. Opening a shell requires the existing operator authentication plus explicit terminal enablement.

The default local target runs as the SynFactory service user, never as root. Remote targets use SSH with strict host-key verification and operator-managed credentials. Private keys, passwords, terminal input and terminal output are not persisted as workflow evidence or normal application logs.

SynFactory may persist only non-sensitive session metadata needed for audit and capacity accounting: session ID, operator identity, target, start/end timestamps and exit/close reason.

## Session contract

The Go control plane owns create, attach, input/output, resize and close semantics. Session policy enforces both deployment-wide and per-target capacity, idle timeout and maximum lifetime. Disconnect and shutdown paths must terminate the owned PTY/process tree before capacity is released.

Backends implement one contract:

- `local`: PTY on the current API/control host;
- `ssh`: PTY over an explicitly configured remote SSH target with host-key verification.

A future worker-side outbound transport may implement the same backend/session contract for networks where inbound SSH is not possible.

## Web control center

The Vue control center will use an xterm-compatible presentation layer and an authenticated WebSocket stream. Vue does not decide target authorization, shell identity, session limits or privilege level. It only requests and renders sessions authorized by Go.

## Operational defaults

Recommended production defaults:

- disabled until explicitly configured;
- 2 concurrent sessions deployment-wide;
- 1 concurrent session per target;
- 15 minute idle timeout;
- 2 hour maximum session lifetime;
- local shell working directory restricted to configured operator-safe roots;
- no root shell and no insecure SSH host-key bypass.

These defaults are policy inputs rather than UI behavior and will be exposed through explicit `SYNFACTORY_TERMINAL_*` configuration as the API/backend implementation is wired.
