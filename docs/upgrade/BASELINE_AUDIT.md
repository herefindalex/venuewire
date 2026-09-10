# Multi-Venue Upgrade Baseline Audit

Audit date: 2026-09-09

## Scope and repository state

The repository is an existing, working Bybit Testnet connector. The Deribit work must extend it in place. `docs/1_bybit/` is the preserved Bybit documentation archive; shared multi-venue documents live at the repository root and in `docs/upgrade/`.

Baseline commit: `809d2ca feat: implement Bybit Testnet connector lab`.

At audit time, `docs/2_deribit/DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md` was staged and the Deribit environment-template/security-test changes were unstaged. No dashboard or HTTP UI service exists, so R1's user surface is the existing `venuewire` CLI.

## Baseline commands and results

| Command | Result |
|---|---|
| `go list ./...` | 11 packages |
| `go test ./...` | 115 passed after the Deribit environment allowlist fix |
| `go test -race ./...` | 115 passed |
| `go vet ./...` | passed |
| `go build -o ./bin/venuewire ./cmd/venuewire` | passed |
| Platform | Go 1.26.3, linux/amd64 |

The first baseline run exposed a security-test regression: `.env.example` had three Deribit variables, while the exact security allowlist recognized only Bybit variables. The fix did not exempt Deribit. It expanded the exact allowlist, requires Deribit secrets to be empty, fixes the environment to Testnet, disables Deribit/trading/FIX gates by default, and requires nonzero risk limits.

## Existing CLI

The binary is `./bin/venuewire`. Existing Bybit behavior must remain backward compatible when no `--venue` is supplied.

- Public REST: `time`, `instrument`.
- Public WS: `market trades`, `market orderbook`.
- Authenticated REST: `account info`, `account balances`, `order place`, `order cancel`, `order amend`, `order status`, `executions`, `positions`.
- Private state: `private-stream`, `reconcile`.
- FIX: `fix mock-demo`, `fix mock-server`, `fix connect-testnet`.

Authenticated command classification fails closed before command execution when Bybit credentials or exact Testnet endpoints are invalid.

## Existing protocol implementation

### Bybit REST V5

- Exact-byte HMAC signing and typed API errors.
- Server time, instruments, tickers, orders, executions, positions, and Unified account queries.
- Pagination and repeated-cursor protection.
- No blind retry after an uncertain order submission.
- Rate-limit metadata and client statistics.

### Bybit WebSocket V5

- Public trades/orderbook and private order/execution/position decoding.
- Bounded queues, reconnect/backoff/resubscription, stale detection, clean unsubscribe, and context shutdown.
- Private overflow fails the connection rather than silently dropping state.

### Bybit FIX

- Direct SOH framing, BodyLength, CheckSum, strict/lenient parsing, duplicate/unknown-tag preservation, and fuzzing.
- TLS, RSA-SHA256 logon, heartbeat/TestRequest, logout, sequence handling, current Bybit D/F/XAR and 8/XCA/XAA flows.
- Local mock recovery starts a new sequence-1 session and reconciles through REST semantics.

Deribit FIX must use a separate dialect and session policy. Only framing, checksum, TLS primitives, and test infrastructure are candidates for reuse.

## Existing domain and persistence

`internal/domain` stores decimal quantities as strings. Orders and executions currently contain `exchange`, `category`, `symbol`, native IDs, side/type, price/quantity/fill fields, normalized/raw status, and timestamps.

`internal/orderstate` stores JSON snapshot version 1:

- maps of orders and executions;
- atomic temporary-file rename;
- permission-restricted output;
- a sidecar lock with nonblocking cross-process `flock` and context-aware polling;
- transactional read-modify-write and reload.

Version 1 is insufficient for V2 because it lacks explicit environment, stable account alias, market/native ID namespaces, intent/attempt state, native amount units, fee persistence, recovery cursors, venue health, and migration provenance. A versioned, reversible v1→v2 migration is required. Existing data cannot be assumed `linear` when category/account evidence is missing.

## Existing correctness guarantees to preserve

- REST ACK is not treated as a terminal order state.
- `orderId` and `orderLinkId` correlation survives process races.
- Late REST ACK/rejection cannot regress a more advanced private-stream state.
- Executions are deduplicated by exchange execution ID.
- Cancel/fill races preserve a confirmed fill.
- Startup/manual/private-reconnect reconciliation is idempotent.
- Gross execution quantity remains distinct from net wallet change; REST execution output includes fee amount, rate, and currency.
- Testnet endpoint allowlists, log redaction, ignored secrets, and generated-binary/state exclusions are enforced by tests.

## Locked upgrade decisions

- Canonical Deribit credentials remain `DERIBIT_API_KEY` and `DERIBIT_API_SECRET`.
- Account aliases are `bybit-test` and `deribit-test`.
- Default risk limits are parameterized: 100 USD per order, 500 USD aggregate open amount, 2% price deviation, and 5 connector-owned open orders.
- Plans expire after 30 seconds and execution requires both a plan ID and `--confirm`.
- Writes require `RUN_MULTI_VENUE_E2E=1` and the venue-specific trading gate.
- Bybit E2E uses USDT. Deribit V2 uses BTC/ETH perpetuals with native collateral because Testnet exposes no USDT futures.
- One metadata-minimum lifecycle covers related commands. Connector-owned residual positions may be closed using minimum-size reduce-only orders.
- E2E covers bounded commands, timed public/private WS, local FIX, and live FIX when access exists. Unavailable live FIX is `BLOCKED`, not passed.
- `docs/1_bybit/` remains an archive.

## Mandatory independent-read E2E invariant

No write response or ACK is accepted as proof of the resulting business state.

- HTTP writes are verified by private events and a separate order/trade/position read.
- WS writes are verified through HTTP JSON-RPC reads.
- FIX writes are verified through canonical JSON-RPC reads.
- Create, edit, cancel, fill, and reduce-only cleanup each assert the expected exchange state through an independent read path.
- Execution totals and fees are reconciled against trade history; cleanup verifies the position returns to zero.
- Missing, delayed, ambiguous, or contradictory evidence produces `FAIL`, `OutcomeUnknown`, or `NeedsReview`, never `PASS`.

## Phase 0 gap summary

R1 requires new venue-aware capabilities, Deribit JSON-RPC HTTP/WS, token refresh, credit rate limiting, metadata/amount validation, private recovery, order-book sequencing, intent write-ahead/idempotency, multi-venue reads, plan/execute safety, v2 persistence migration, and complete offline/live tests.

R2 requires a distinct Deribit FIX dialect, authentication, sequence/recovery policy, order mapping, local mock, and separately reported live validation levels.

No existing Bybit module should be moved until a concrete shared boundary has two real consumers.
