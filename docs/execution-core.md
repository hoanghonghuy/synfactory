# Durable execution core

SynFactory uses PostgreSQL as both durable execution state and its initial work queue. This keeps the first deployment small while preserving the semantics needed to split API, scheduler and workers across hosts later.

## Job lifecycle

`queued -> leased -> running -> succeeded`

A failed running attempt becomes `retry_wait` while `attempt < max_attempts`; after the budget is exhausted it becomes terminal `failed`. `available_at` gates both queued and retry work, so delayed retries are never claimed early.

## Claiming and leases

Workers claim one job with a PostgreSQL transaction using `FOR UPDATE SKIP LOCKED`. Claiming changes the job to `leased` and records `lease_owner` plus `lease_until`. Starting the job validates that the same worker still owns an unexpired lease and only then increments the attempt counter.

Workers must renew active leases before expiry. Success/failure transitions require a live matching lease, preventing a worker that lost ownership from overwriting newer state.

## Crash recovery

Periodic recovery handles expired leases:

- `leased` jobs return to `queued` without consuming an attempt because execution never started;
- `running` jobs mark their active run `timed_out` and move to `retry_wait` when budget remains;
- a running job whose attempt budget is exhausted becomes terminal `failed`;
- all recovered jobs clear lease ownership so capacity is released.

This recovery is process-independent. Restarting API/scheduler/worker processes does not erase queued work.

## Idempotency

`event_inbox.dedupe_key` and `jobs.dedupe_key` are unique. Duplicate webhook deliveries, reconciliation-generated equivalents and repeated routing attempts resolve the existing durable record rather than creating duplicate work.

## Worker presence

The `workers` table stores capacity, draining state and last heartbeat for operational visibility. Job leases remain authoritative for ownership; heartbeat is not a substitute for lease expiry.

## Migrations

SQL migrations are embedded in the Go binary and applied transactionally at startup. Applied migration filenames are recorded in `schema_migrations`.
