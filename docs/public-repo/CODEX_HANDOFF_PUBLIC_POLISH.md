# VenueWire Public Repository Polish Handoff

Date: 2026-09-12

The public-repository polish scope is implemented and pushed to `master` without changing trading behavior or performing external venue, deployment, release, or GitHub-settings operations.

## Changed files

### Public documentation

- `README.md` - redesigned first screen, badges, sanitized console screenshot, architecture, engineering rationale, capability summary, demo flow, safety boundary, build/run instructions, validation links, repository map, and limitations.
- `docs/STATUS.md` - authoritative evidence-backed current status.
- `docs/ARCHITECTURE.md` - system topology, trade identity/recovery flow, state-consistency rules, deployment boundary, and package map.
- `docs/CAPABILITIES.md` - venue/protocol/browser/FIX capability matrix and explicit non-capabilities.
- `docs/SECURITY.md` - Testnet scope, credential handling, repository protections, sanitization, dependency findings, and vulnerability reporting.
- `docs/assets/venuewire-console.png` - actual Vue console rendered with deterministic sanitized Testnet fixtures.
- `docs/public-repo/BASELINE.md` - pre-edit commit, toolchain, clean state, tests, build results, and pre-existing dependency warnings.
- `docs/public-repo/GITHUB_SETTINGS.md` - manual metadata, security, Actions, branch-protection, and release checklist.
- `docs/public-repo/RELEASE_NOTES_DRAFT.md` - un-published v0.1.0 draft with exact safety and validation boundaries.
- `IMPLEMENTATION_STATUS.md` - links historical phase status to the authoritative current status.

### Build, CI, and security

- `.github/workflows/ci.yml` - Go tests/vet/race, frontend typecheck/lint/unit tests/build/Playwright, and the full embedded build.
- `.github/workflows/secret-scan.yml` - full-history checkout and Gitleaks scan.
- `.gitignore` - adds generic log, Playwright result, and report exclusions while preserving `.env.example`.
- `Makefile` - adds `format`, non-mutating `format-check`, `test-race`, browser targets, and comprehensive non-mutating `verify`.
- `web/package.json` and `web/package-lock.json` - add ESLint and Playwright tooling and scripts.
- `web/eslint.config.js` - one flat ESLint configuration for TypeScript, Vue, and browser tooling.
- `web/playwright.config.ts` and `web/e2e/public-console.spec.ts` - deterministic browser fixtures for the public screenshot, venue switching and protected confirmation, `OUTCOME_UNKNOWN`, stale/recovering state, and session expiry.
- `web/vite.config.ts` - keeps Vitest unit discovery under `src/**/*.test.ts` so Playwright specs are not executed by Vitest.
- `web/go.mod` - marks the frontend as a nested non-application Go boundary so root `go test ./...` cannot traverse Go source embedded in `node_modules` dependencies.

### Canonical module migration

- `go.mod` now declares `module github.com/herefindalex/venuewire`.
- All 203 internal imports across 78 Go files under `cmd/` and `internal/` now use `github.com/herefindalex/venuewire/internal/...`.
- `go mod tidy` promoted the directly imported `github.com/joho/godotenv` requirement from indirect to direct.
- No command, API, data format, trading state, exchange request, or strategy behavior changed.

## Corrected status discrepancies

- Mock and deterministic local tests are labeled `LOCAL_TESTED`, not Testnet verified.
- Bybit and Deribit REST/WebSocket and browser Spot flows retain their documented Testnet evidence.
- Deribit FIX is recorded as a real Testnet order flow, not merely a logon.
- Bybit FIX is recorded as locally tested with live Testnet verification `BLOCKED` by the external whitelist/RSA gate.
- Public HTTPS/WSS and the split-host Nginx path are Testnet verified.
- The Go-host firewall ACL remains `NOT_RUN` because privileged firewall state and direct-port denial were not directly inspected.
- Workflow files are `IMPLEMENTED`; hosted CI and Gitleaks passed on `master`, while repository-level security settings remain unverified.

## CI jobs

`CI` contains three jobs:

1. `Go tests and static analysis`: `go test ./...`, `go vet ./...`, and `go test -race ./...` using the version in `go.mod`.
2. `Frontend verification`: locked npm install, typecheck, ESLint, Vitest, production build, Chromium installation, and four mocked Playwright scenarios using Node.js 24.
3. `Full embedded build`: `make build` after the Go and frontend jobs pass.

`Secret scan` checks out full available history and runs Gitleaks on pushes, pull requests, and manual dispatch.

None of the workflows require exchange credentials or enable Testnet trading gates.

On a new local checkout, run `npm --prefix web ci` and `(cd web && npx playwright install chromium)` before `make verify`. Dependency and browser installation are explicit setup steps; `make verify` itself does not rewrite source files.

## Verification performed

### Pre-edit baseline

| Command | Result |
|---|---|
| `git status --short` | PASS - clean worktree |
| `go test ./...` | PASS - 419 tests, 23 packages |
| `go vet ./...` | PASS |
| `go test -race ./...` | PASS - 419 tests, 23 packages |
| `npm --prefix web ci` | PASS |
| `npm --prefix web run typecheck` | PASS |
| `npm --prefix web test` | PASS - 13 tests |
| `npm --prefix web run build` | PASS |

### Final integrated checks

| Command | Result |
|---|---|
| `go mod tidy` | PASS |
| `make verify` | PASS - format check, vet, 419 Go tests, race suite, frontend typecheck/lint, 13 Vitest tests, frontend build, and 4 Chromium Playwright tests |
| `make build` | PASS - `bin/venuewire` built with embedded Web UI |
| `./bin/venuewire help` | PASS |
| `actionlint` against `.github/workflows/*.yml` | PASS |
| `git diff --check` | PASS |
| Relative documentation-link existence check | PASS |
| Gitleaks Git history scan | PASS - 58 commits, no leaks |
| Gitleaks publishable working-tree scan | PASS - tracked and non-ignored files, no leaks |
| `npm audit --omit=dev` | PASS - zero production dependency vulnerabilities |

### Hosted GitHub Actions

- [CI run 34677964751](https://github.com/herefindalex/venuewire/actions/runs/34677964751) passed Go tests/vet/race, frontend typecheck/lint/Vitest/build, four Playwright scenarios, and the full embedded build on `master`.
- [Secret scan run 34677964735](https://github.com/herefindalex/venuewire/actions/runs/34677964735) passed the full-history Gitleaks scan on `master`.
- Both runs completed on commit `8c03731986b0095c88d152a1362f45ef5f6399b9`.

The ignored local `.env` is outside the publishable working tree. Its values were not read or printed. The full npm audit still reports two moderate-severity development-dependency findings and recommends a potentially breaking forced upgrade; that upgrade was not applied.

## Screenshot

`docs/assets/venuewire-console.png` is a 1440-pixel-wide full-page capture of the actual Vue application. It uses deterministic sanitized fixtures and shows the `TESTNET` badge, Bybit venue selector, account balances, System Status, and a filled recent trade. It contains no credentials, session tokens, live usernames, private account/order identifiers, hostnames, or IP addresses.

## Manual GitHub actions still required

- Select MIT or Apache-2.0 and add the standard license text.
- Configure required status checks and branch protection.
- Verify the dependency graph, Dependabot alerts, secret scanning, push protection, and private vulnerability reporting in GitHub.
- Apply the About description and reviewed repository topics.
- Decide whether to publish a stable demo URL or retain “Demo access available on request.”
- Review and publish the prepared release only after license selection and the repository-settings review are complete.

## Remaining blockers and concerns

- License selection is intentionally pending owner choice.
- Bybit live FIX remains blocked by the external whitelist/RSA gate.
- The Go-host firewall ACL still requires privileged operator inspection.
- Branch protection, push protection, Dependabot, private vulnerability reporting, and other repository settings remain unverified.
- npm reports two moderate development-dependency findings; production dependencies report zero vulnerabilities.
- Vite reports a non-failing JavaScript chunk-size warning for the existing Ant Design bundle. Code splitting was not introduced because it is outside this repository-polish scope.

The v0.1.0 release is prepared as a draft but was not published. Seven public-polish commits were pushed to `master`; no GitHub repository setting was changed.
