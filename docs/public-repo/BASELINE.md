# Public Repository Polish Baseline

Date: 2026-09-12

This baseline records the repository state before the public-repository polish work. All commands were run without exchange credentials or external trading operations.

## Repository state

| Item | Value |
|---|---|
| Branch | `master` |
| Commit | `b853f92203f70d91011adf16013825ad4426a16c` |
| Uncommitted changes | None |
| Remote | `https://github.com/herefindalex/venuewire` |

## Toolchain

| Tool | Version |
|---|---|
| Go | `go1.27.1 linux/amd64` |
| Node.js | `v24.15.0` |
| npm | `11.12.1` |
| Module-declared Go version | `1.26.3` |

## Baseline verification

| Check | Result |
|---|---|
| `go test ./...` | PASS - 419 tests in 23 packages |
| `go vet ./...` | PASS |
| `go test -race ./...` | PASS - 419 tests in 23 packages |
| `npm --prefix web ci` | PASS - 168 packages installed and audited |
| `npm --prefix web run typecheck` | PASS |
| `npm --prefix web test` | PASS - 13 tests, 0 failures |
| `npm --prefix web run build` | PASS |

## Pre-existing concerns

`npm ci` reported two moderate-severity dependency vulnerabilities and one deprecation warning for `whatwg-encoding@3.1.1`. The audit output recommended `npm audit fix --force`, which may include breaking changes. This polish pass records the issue but does not apply an unreviewed forced dependency upgrade.

No baseline command failed.
