# GitHub event ingestion and reconciliation

SynFactory uses two independent paths that converge on the same durable logical events.

## Fast path: webhook

`POST /webhooks/github`:

1. bounds the request body;
2. validates `X-Hub-Signature-256` with HMAC-SHA256;
3. normalizes supported GitHub events into SynFactory-owned logical kinds;
4. upserts repository identity;
5. inserts the event into PostgreSQL using a logical dedupe key;
6. acknowledges GitHub only after durable storage.

The webhook handler never invokes an LLM or coding runtime inline.

Supported webhook families currently include issues, issue comments, pull requests,
pull-request reviews/comments, check runs/suites, pushes and workflow runs.

## Correctness path: reconciliation

When `SYNFACTORY_GITHUB_TOKEN` is configured, reconciliation runs immediately at
startup and then every `SYNFACTORY_RECONCILE_INTERVAL` (default one hour).

For each enabled GitHub repository it projects:

- open issues;
- open pull requests and head revisions;
- pull-request reviews;
- check runs for open PR heads;
- the configured default branch revision.

Synthetic events use the same `provider + repository + kind + subject + revision`
dedupe identity as webhook events. A missed webhook therefore creates work on the
next reconciliation, while a webhook already handled is not duplicated by the sweep.

GitHub API `Retry-After` and primary rate-limit reset headers are respected.

## Durable event routing

`event_inbox` is itself a leased queue. Event processors claim rows with
`FOR UPDATE SKIP LOCKED`, which allows multiple API/process instances without
routing the same event concurrently.

Routing is deterministic:

- issue changes -> PM;
- issue comments -> PM, or Developer when the issue is a PR conversation;
- PR changes -> Team Lead;
- PR review/comments -> Developer;
- CI/check/workflow changes -> CI Guardian;
- default/relevant branch changes -> Team Lead.

A routed job has a deterministic dedupe identity derived from the logical event and
target role. Event-router failures retry with bounded exponential backoff. After the
configured attempt budget, the event is dead-lettered by recording `process_error`
and terminal `processed_at`; it no longer monopolizes the processor.

## Failure and restart behavior

Webhook acknowledgement happens only after the event exists in PostgreSQL. If the
process dies after acknowledgement but before routing, another processor can reclaim
the inbox event after its processing lease expires.

The main service also runs job lease recovery periodically. A dead worker therefore
cannot keep a queued/running job forever.

## Authentication

The GitHub client depends on a `TokenSource` abstraction. V0 uses
`SYNFACTORY_GITHUB_TOKEN`; a GitHub App installation-token source can replace it
without changing the reconciler or workflow domain.
