# Autonomy soak and fault validation

Issue #32 treats long-running autonomy as a release property, not a model claim. Soak evidence must come from durable SynFactory state plus explicit fault records while normal review, CI, authorization and repair-budget gates remain enabled.

## Observe-only run

The runner is non-destructive by default. It samples the Go-owned `/ops` read model and writes JSONL evidence under `./data/autonomy-soak`:

```sh
SYNFACTORY_SOAK_SAMPLES=2880 \
SYNFACTORY_SOAK_INTERVAL_SECONDS=30 \
bash scripts/autonomy-soak.sh run
```

That example covers 24 hours. A multi-day qualification run increases `SYNFACTORY_SOAK_SAMPLES`; do not shorten normal workflow retry/review limits merely to make the soak finish sooner.

Each JSONL record has an observation timestamp, HTTP status and the exact operational/autonomy stats returned by the control plane. Transport failures are recorded with `stats:null` instead of being silently dropped.

## Controlled restart faults

Fault injection is opt-in. A single restart can be injected explicitly:

```sh
bash scripts/autonomy-soak.sh restart worker
bash scripts/autonomy-soak.sh restart scheduler
bash scripts/autonomy-soak.sh restart api
```

For a repeatable restart sequence during the sampling run:

```sh
SYNFACTORY_SOAK_FAULT_EVERY=20 \
SYNFACTORY_SOAK_FAULT_SEQUENCE=worker,scheduler,api \
bash scripts/autonomy-soak.sh run
```

Every injected restart is appended to a sibling `*.faults.log` with its UTC timestamp. The default Compose invocation is `docker compose -f compose.yaml`; override `SYNFACTORY_SOAK_COMPOSE_ARGS` when the deployment uses explicit overlays/profiles.

The runner deliberately does not kill PostgreSQL, delete volumes, mutate GitHub issues/PRs, weaken CI, or alter repair budgets. Destructive datastore/repository-loss drills require a separate operator-approved environment and must never run against the only production copy.

## What a healthy run demonstrates

Use the Autonomy Health panel, `/ops`, Prometheus metrics and the JSONL/fault log together. A healthy run should show:

- useful work continuing across the observation window rather than permanent idle capacity;
- explicitly blocked workflows remaining distinguishable from truly stuck runnable work;
- bounded CI/review repair followed by recovery or park/escalation, never an infinite retry loop;
- stale leases/workers recovering after controlled restarts;
- recovery transitions after blocked/fault conditions where the underlying fact changes;
- exact-head review/CI/merge gates still required throughout the run.

Transport failures during an intentional API restart are expected transient evidence. Repeated failures after the service is back, continuously increasing stuck workflows, repeated repair exhaustion for unchanged failure classes, duplicate task creation, or capacity starvation are release blockers until explained and fixed.

## Release evidence

Keep the JSONL and fault log for the exact build/deployment under qualification. Record the deployment commit SHA and relevant runtime configuration beside the evidence; never include operator tokens, GitHub credentials, SSH private keys, raw terminal input/output or other secrets. Multi-day dogfood results should be summarized in the release decision rather than committed as large generated evidence files.
