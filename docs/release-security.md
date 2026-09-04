# Release security gates

SynFactory treats dependency and image security as release gates, not advisory-only checks. Security gates supplement and never replace format, vet, test, Compose, or production image build verification.

## Required gate policy

- Go: run the pinned `govulncheck` scanner against `./...`; reachable known vulnerabilities fail the gate.
- Web: verify the committed npm lockfile with `npm ci`, then scan `web/package-lock.json` with pinned Trivy. High or critical production-dependency findings fail release eligibility; development dependencies are not part of the shipped static web runtime.
- Containers: scan the final control, worker, and web images with pinned Trivy. `high` or `critical` OS/package findings fail release eligibility unless a reviewed, time-bounded exception exists.
- SBOM: every releasable image receives a CycloneDX SBOM generated from the exact locally built image.
- Release evidence: CI retains the three SBOMs plus a manifest binding the exact source SHA to each immutable local image content ID. When registry publishing is enabled, registry digests and attestations extend this manifest rather than replacing it.
- GitHub Actions used by required workflows are pinned to immutable commit SHAs. Keep the intended upstream release in a trailing comment (for example `# v7`) so updates remain reviewable.

## Failure classification

A positive vulnerability finding is a product/release blocker. Fix the affected dependency/image or record a reviewed exception before release.

Scanner installation failures, vulnerability-database outages, registry failures, GitHub artifact-service failures, and other external network/tooling faults are infrastructure blockers. CI Guardian may make a bounded retry when fresh evidence indicates a transient fault, but must not weaken or skip the gate to obtain green CI. Repeated infrastructure failures are parked/escalated with the failing tool, exact commit, run URL, and last error.

Required CI is time-bounded, and a new PR head cancels an obsolete in-progress run for the same PR. This prevents a superseded commit or external security service from monopolizing CI capacity while preserving a required green run on the exact head that is authorized for merge.

## Exceptions

Security exceptions are explicit and temporary. Each exception must record:

1. vulnerability/advisory identifier;
2. affected component/image;
3. why the vulnerable path is not currently exploitable or why immediate remediation is riskier;
4. compensating control;
5. owner;
6. expiry date;
7. remediation issue.

Do not add blanket ignore files, severity downgrades, `continue-on-error`, or shell `|| true` to required security gates. An expired exception is a failing release gate.

## Updating pinned tools and Actions

For each update:

1. verify the release/tag from the upstream project;
2. resolve the tag to its immutable commit SHA;
3. update the SHA and readable version comment together;
4. review the upstream release notes for permission/input/output changes;
5. run the full PR CI suite on the exact resulting head;
6. do not merge when the head changes after review without repeating the exact-head gate.

Tool versions installed inside CI should also be explicit rather than `@latest` so a release does not silently change the scanner used to authorize it.

## Release evidence and publishing

PR CI builds `synfactory-control:ci`, `synfactory-worker:ci`, and `synfactory-web:ci`, scans those exact images, generates CycloneDX SBOMs, and uploads a `release-evidence-<source-sha>` artifact containing the SBOMs and manifest. This evidence is portable and does not depend on a particular registry or paid attestation feature.

For a later registry-publishing workflow, use immutable tags derived from the source commit or an explicit release version, resolve the pushed registry digest, and record that digest in provenance/attestation evidence. Never authorize deployment from a mutable tag alone, and never claim a local Docker image ID is a registry digest.
