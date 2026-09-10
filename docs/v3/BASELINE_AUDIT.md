# VenueWire V3 baseline audit

Audit date: 2026-09-10

## Repository state

- Baseline commit: `d2679b5` (`WIP: clarify venue syntax in CLI help`).
- The user-provided `docs/3_web/` specification bundle is present as staged
  and untracked work and is treated as read-only source material.
- Toolchain: Go 1.26.3, Node 24.15.0, npm 11.12.1.
- Main package and binary: `./cmd/venuewire`, `./bin/venuewire`.
- There is no existing HTTP server, browser API, frontend package, session
  middleware, browser WebSocket hub, CORS policy, or embedded UI.

## Existing capabilities

| Area | Observed baseline |
|---|---|
| Bybit | REST V5 public/private reads and writes, public/private WebSocket, order/execution reconciliation, FIX mock/live-gated client. REST request types already accept `category=spot`; the configured public WebSocket endpoint is currently linear-only. |
| Deribit | HTTP and WebSocket JSON-RPC, account summaries, instrument metadata, public/private events, HTTP/WS order writes, reconciliation, and an independently implemented FIX dialect. Existing validated trading scope is inverse perpetuals. |
| Identity | Orders and executions are scoped by venue, Testnet environment, account alias, namespace, and native ID. |
| Persistence | `internal/intent.Store` and `internal/orderstate.FileStore` use an adjacent `flock` lock file, read-modify-write under the lock, mode-0600 temporary files, atomic replacement, and directory sync. No database is present. |
| Configuration | Process environment only. No dotenv loader or `--env-file` exists. Existing Deribit credentials are `DERIBIT_API_KEY` and `DERIBIT_API_SECRET`. |
| CLI | Bybit remains the legacy default when `--venue` is omitted. Current help presents explicit `--venue bybit|deribit|all`. The browser-facing V3 must not depend on interviewers using the CLI. |

## Baseline verification

| Check | Result |
|---|---|
| `go list ./...` | PASS, 16 packages |
| `go vet ./...` | PASS |
| `go test ./... -count=1 -timeout=90s` | 238 PASS, 1 FAIL, 2 SKIP |
| `go test -race ./... -count=1 -timeout=120s` | 238 PASS, 1 FAIL, 2 SKIP |

The single baseline failure is
`internal/security.TestEnvironmentExampleIsEmptyAndSecretsAreIgnored`:
`.env.example` contains `BYBIT_ENABLED=false`, but the test's explicit safe
key/value allowlist does not yet include it. The two skipped tests are opt-in
external Testnet tests. No exchange request or account mutation was performed
during this audit.

## V3 implementation decisions

- VenueWire is the canonical project, module, command, and binary name. V3
  examples using the former `bybitctl` name will be translated to `venuewire`.
- Existing exchange credential names and symmetric read/trading/FIX gates stay
  authoritative. Draft `DERIBIT_CLIENT_ID`/`DERIBIT_CLIENT_SECRET` and removed
  integration-gate aliases will not be reintroduced.
- The Web console is the interview product. CLI behavior remains available for
  regression and operations but receives no separate V3 interaction design.
- Reuse the locked JSON stores. A single Web process owns browser sessions,
  quotes, reservations, and submissions. SQLite and multi-instance Web
  coordination are out of scope for this interview build.
- Concurrent Browser confirmations within that process must still be
  idempotent, and confirmed intents must remain recoverable after restart.
- Deployment IPs, public origin, ACLs, Nginx installation, and authorized live
  Testnet orders remain environment-specific verification gates. Missing access
  is reported as `NOT_RUN` or `BLOCKED`, never as passing.
