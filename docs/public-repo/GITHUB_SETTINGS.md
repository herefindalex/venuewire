# GitHub Repository Settings Checklist

This checklist records the desired public-repository metadata and protections. It does not claim that account-level or repository-level settings are enabled. Verify each item directly in GitHub after the branch containing the workflow files is available remotely.

## About metadata

Suggested description:

```text
Multi-venue trading connectivity and execution prototype - Bybit and Deribit, Go, WebSocket, FIX, reconciliation.
```

Suggested topics:

```text
go
trading-systems
trading-infrastructure
fix-protocol
websocket
bybit
deribit
reconciliation
vue3
```

Do not add `hft`, `production-trading`, `mainnet`, or other topics that imply unsupported performance or production scope.

## General settings

- [ ] Confirm the repository description matches the About text above.
- [ ] Add the reviewed topics above.
- [ ] Confirm the default branch and branch-protection target.
- [ ] Confirm the README screenshot and workflow badges render after merge.
- [ ] Add a license only after the owner explicitly selects MIT or Apache-2.0.

## Security and analysis

- [ ] Dependency graph enabled.
- [ ] Dependabot alerts enabled.
- [ ] Dependabot security updates reviewed for suitability.
- [ ] Secret scanning enabled where available for the account/repository.
- [ ] Push protection enabled where available.
- [ ] Private vulnerability reporting enabled.
- [ ] `Secret scan` workflow has completed successfully against full available history.
- [ ] Any Gitleaks allowlist is narrow, reviewed, and documented.

## Actions and branch protection

- [ ] GitHub Actions enabled for the repository.
- [ ] `CI / Go tests and static analysis` required before merge.
- [ ] `CI / Frontend verification` required before merge.
- [ ] `CI / Full embedded build` required before merge.
- [ ] `Secret scan / Gitleaks repository and history scan` required before merge.
- [ ] Workflow permissions remain read-only unless a specific job requires more.

## Release and demo

- [ ] Review [`RELEASE_NOTES_DRAFT.md`](RELEASE_NOTES_DRAFT.md).
- [ ] Select and add the license before publishing the first release.
- [ ] Confirm the release is clearly labeled Testnet-only.
- [ ] Confirm no credentials, private account identifiers, private hosts, or internal IP addresses appear in screenshots or release text.
- [ ] Use “Demo access available on request” unless a stable public URL is intentionally maintained.

## Manual verification record

Record the date, operator, and observed result beside each completed item. Do not convert this checklist into a claim that a setting changed unless the GitHub UI or API was actually inspected.
