# Release security gates

SynFactory treats dependency and image security as release gates, not advisory-only checks. Security gates supplement and never replace format, vet, test, Compose, or production image build verification.

## Required gate policy

- Go: run the pinned `govulncheck` scanner against `./...`; reachable known vulnerabilities fail the gate.
- Web: once the committed npm lockfile is present, install with `npm ci` and fail `npm audit` on `high` or `critical` findings affecting the production dependency graph.
- Containers: scan the final control, worker, and web images. `high` or `critical` OS/package findings fail release eligibility unless a reviewed, time-bounded exception exists.
- SBOM: every releasable image must have an SPDX or CycloneDX SBOM associated with the exact image digest and source commit.
- GitHub Actions used by required workflows must be pinned to immutable commit SHAs. Keep the intended upstream release in a trailing comment (for example `# v7`) so updates remain reviewable.

## Failure classification

A positive vulnerability finding is a product/release blocker. Fix the affected dependency/image or record a reviewed exception before release.

Scanner installation failures, vulnerability-database outages, registry failures, GitHub artifact-service failures, and other external network/tooling faults are infrastructure blockers. CI Guardian may make a bounded retry when fresh evidence indicates a transient fault, but must not weaken or skip the gate to obtain green CI. Repeated infrastructure failures are parked/escalated with the failing tool, exact commit, run URL, and last error.

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

## Release evidence

Release evidence must identify the source commit, image name/tag, immutable image digest, scanner/tool version, scan result, and SBOM artifact. When registry publishing/provenance is enabled, attestations must bind back to the same digest rather than to a mutable tag alone.
