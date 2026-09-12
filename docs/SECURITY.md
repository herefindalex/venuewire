# VenueWire Security Guidance

VenueWire is a Testnet-only prototype. Its security controls reduce demo and development risk, but they do not make the project suitable for Mainnet funds or production trading.

## Safety scope

- Use Testnet accounts and exact documented Testnet endpoints only.
- Do not grant withdrawal or transfer permissions to VenueWire credentials.
- Begin with all write gates disabled, including `WEB_TRADING_ENABLED=false`.
- Enable a bounded Testnet write only after reviewing the account, open orders, limits, and cleanup plan.
- VenueWire does not provide Mainnet, transfer, withdrawal, or arbitrary-symbol browser commands.

## Credential handling

- Copy `.env.example` to an ignored local file and fill values locally.
- Prefer an operator-owned mode-0600 file outside the repository for deployed demos.
- Never commit API keys, secrets, passwords, session secrets, private keys, cookies, authorization headers, or complete authentication payloads.
- Never paste credentials into issues, pull requests, logs, screenshots, recordings, or test fixtures.
- Rotate any credential that was exposed, even if it was later removed from the current branch. Git history and caches may retain it.
- Use separate Testnet credentials with the minimum permissions needed for the validation being performed.

The application redacts known sensitive fields from structured logs. Browser DTOs omit exchange credentials and internal session identity. These protections supplement, rather than replace, careful credential handling.

## Repository protections

The repository ignores local dotenv files, private-key extensions, logs, state, secrets, build output, coverage output, and frontend dependencies. `.env.example` is intentionally committed with empty secret values.

GitHub workflow `secret-scan.yml` performs a full-history checkout and runs Gitleaks on pushes and pull requests. Its hosted [full-history run](https://github.com/herefindalex/venuewire/actions/runs/34677964735) passed on `master` at commit `8c03731986b0095c88d152a1362f45ef5f6399b9` on 2026-09-12. False-positive allowlists, if ever required, must be narrow, value-specific, reviewed, and documented. Broad directory exemptions are not acceptable.

GitHub push protection, secret scanning, Dependabot alerts, the dependency graph, and private vulnerability reporting are repository settings. Their desired state is documented in [GitHub settings](public-repo/GITHUB_SETTINGS.md); the repository does not claim they are enabled until verified in GitHub.

## Screenshot and demo sanitization

Before publishing a screenshot or recording, remove or replace:

- API keys, secrets, cookies, and session tokens;
- usernames and private account identifiers;
- client and venue order IDs that identify a private account history;
- internal hostnames, private IP addresses, and private service URLs;
- request authorization data and FIX authentication fields.

Published screenshots should visibly say `TESTNET` and use only deliberately sanitized demo labels and values.

## Dependency findings

The 2026-09-12 baseline `npm ci` reported two moderate-severity dependency vulnerabilities. They require review through `npm audit`; do not apply `npm audit fix --force` without assessing the breaking changes and rerunning the full verification suite.

## Reporting a vulnerability

Do not open a public issue containing exploit details, credentials, or sensitive deployment information. Prefer GitHub private vulnerability reporting when enabled. If it is not enabled, contact the repository owner privately and provide the smallest reproducible description that does not expose secrets.
