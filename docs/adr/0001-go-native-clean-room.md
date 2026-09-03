# ADR 0001: Go-native clean implementation

- Status: Accepted
- Date: 2026-09-03

## Context

SynFactory is informed by several open-source autonomous software-factory projects implemented in different languages and frameworks. A mechanical port would inherit their framework constraints, deployment assumptions, data models and accidental complexity.

The product also has stricter requirements than any one upstream: persistent 24/7 operation, event-driven GitHub handling plus periodic reconciliation, multi-repository control, hard permission boundaries, pluggable CLI/model runtimes, bounded failure handling, and deployment on a plain EC2/VPS environment.

## Decision

Build SynFactory as a clean Go implementation around explicit domain contracts.

- Go owns backend services, orchestration, state machines, workers, integrations and the operator CLI.
- PostgreSQL is the durable state store and initial queue/lease mechanism.
- Vue 3 is allowed only for a later optional web dashboard.
- Upstream repositories are cloned under ignored `upstreams/` directories for research only.
- Capabilities are ported intentionally from documented behavior and tests, not translated file-by-file.
- Any direct source reuse requires a license review and attribution before merge.
- Runtime integrations (Codex, Cursor, Antigravity, Claude Code, OpenCode, custom endpoints) are adapters and cannot leak into core domain types.

## Consequences

### Positive

- one coherent architecture instead of a cross-language bundle;
- small deployable Go services and simple EC2 operation;
- easier deterministic concurrency, leases and reconciliation;
- runtime/model vendors can be replaced independently;
- licensing provenance remains tractable;
- Vue can evolve independently after API contracts stabilize.

### Negative

- initial implementation takes longer than forking one upstream;
- upstream fixes are not automatically inherited;
- capability parity must be verified with SynFactory-owned tests and acceptance criteria.

## Rejected alternatives

### Fork Paddock and replace Next.js incrementally

Rejected because the existing application/database shape would dictate the migration and produce a long mixed-stack transition.

### Fork Vanguard and add a dashboard/control plane

Rejected because Vanguard is strongest as an execution pipeline/watch loop, not as the complete multi-project governance/control-plane domain SynFactory needs.

### Use OpenHands as the entire product

Rejected because OpenHands is valuable as an execution/automation reference or optional runtime, but SynFactory must not make its product state and governance dependent on one agent platform.
