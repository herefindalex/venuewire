# Multi-venue implementation status

Last updated: 2026-09-10

The archived Bybit-only phase history remains in `docs/1_bybit/IMPLEMENTATION_STATUS.md`. This file tracks the incremental Bybit + Deribit upgrade defined by `docs/2_deribit/DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md`.

## V3 / V3.1 implementation status

The V2 phase history below remains historical evidence. V3/V3.1 implementation is active; completed local phases and remaining external verification are recorded below.

| Work | Status | Evidence |
|---|---|---|
| V3 Phase 0 baseline inspection | Complete; baseline failure recorded | `docs/v3/BASELINE_AUDIT.md`: 238 pass, 1 security-example failure, 2 skipped; same failure under race detector |
| V3.1 gap analysis and product decisions | Documented | `docs/v3/V3_1_GAP_ANALYSIS.md`; `docs/3_web/VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md` section 26 |
| Canonical configuration examples | Implemented and documented | V3 names, `QUOTE_TTL=5s`, V3.1 Demo caps, existing Deribit credential names and VenueWire commands |
| Web / Spot / Demo hardening | In progress | Auth, frontend, durable browser intents/quotas, Spot adapters, reconciliation, runtime WS delivery, live valuation and runtime metrics are locally verified; final failure fixtures remain |
| V3/V3.1 frontend and external validation | Local checks pass; external not run | Vue typecheck/tests/build pass; no new Testnet orders or deployment operations performed |

### V3.1 Phase A — configuration and public Web boundary

Status: `LOCAL_VERIFIED` (trade quota enforcement continues with the persistent intent work in Phase B).

- Added dotenv loading with `--env-file`; existing OS values, including explicitly empty values, take precedence.
- Added strict Web configuration for private bind, HTTPS origin, trusted single-IP proxies, session credentials, enabled venues, 5-second quotes, exact decimal caps and Demo limits.
- Added a separate allowlisted Bybit Spot public WebSocket endpoint while retaining the existing linear endpoint used by CLI flows.
- Added the `web` command and backend boundary with fixed-env login, constant-time credential comparison, server-side signed opaque sessions, absolute expiry, Secure/HttpOnly/SameSite cookie, CSRF, exact Origin/Host checks, trusted forwarding headers, safe correlated errors and login rate limiting.
- Protected venue discovery and Browser WebSocket upgrade with the same session/proxy boundary; logout revokes the session and closes its active WebSockets.
- Demo limit configuration is validated. Durable global/session counting and unresolved-trade occupancy must be committed atomically with Phase B intents and are not yet marked implemented.

Verification after Phase A:

| Check | Result |
|---|---|
| `go test ./... -count=1 -timeout=90s` | PASS — 273 tests, 17 packages |
| `go test -race ./... -count=1 -timeout=120s` | PASS — 273 tests, 17 packages |
| `go vet ./...` | PASS |
| `go build -o /tmp/venuewire-phase-a ./cmd/venuewire` | PASS |
| Frontend test/build | NOT_APPLICABLE — frontend has not been created |
| External Testnet/deployment operations | NOT_RUN |

### V3.1 Phase B — Quick Trade domain and browser UI foundation

Status: `LOCAL_VERIFIED_FOUNDATION`; real venue adapters, account runtime, reconciliation orchestration and end-to-end Demo behavior remain pending.

- Extended the existing locked intent snapshot additively with confirmed Quick Trade snapshots, request/quote indexes, lifecycle, fee/result fields and restart-safe Demo accounting. Existing V2 plans and snapshot version remain readable.
- Confirm acceptance, quote consumption, session/global rolling quotas and active-trade occupancy share one flock-protected atomic update. `Unknown` remains active across restart; terminal evidence releases concurrency but still counts toward session/hour quotas.
- Added four fixed Spot route definitions and an exact `math/big.Rat` quote engine covering source-asset budgets, V3/V3.1 caps, tick/step/minimum validation, Limit IOC 50-bps protection, visible-depth estimates, fees, third-asset reserves, capacity and 5-second Confirm revalidation.
- Added browser quote/confirm/history/detail/Recheck API boundaries. Public DTOs exclude session IDs and internal identities. A detached submission context prevents browser disconnect from cancelling persisted execution work.
- Added Vue 3, TypeScript, Vite and Ant Design Vue UI for login, venue switch, account assets, protected Review/Confirm, visible TESTNET state, runtime status, shared Recent Trades lifecycle, Recheck and About/architecture.
- Added build-tagged frontend embedding. Ordinary Go/CLI tests do not require generated frontend output; `make build` creates the frontend and a `webui` binary.
- Actual Bybit/Deribit providers and submitters are not wired into the `web` command yet. The UI compiles and its contracts are tested, but account/status/quote execution endpoints remain unavailable in a running binary until the next phase.

Verification after this foundation:

| Check | Result |
|---|---|
| `go test ./... -count=1 -timeout=120s` | PASS — 303 tests, 19 packages |
| `go test -race ./... -count=1 -timeout=120s` | PASS — 303 tests, 19 packages |
| `go vet ./...` | PASS |
| `npm --prefix web run typecheck` | PASS |
| `npm --prefix web test` | PASS — 2 tests |
| `npm --prefix web run build` | PASS |
| `go build -tags webui -o /tmp/venuewire-web ./cmd/venuewire` | PASS |
| External Testnet/deployment operations | NOT_RUN |

### V3.1 Phase C1 — cached account runtime and browser status API

Status: `LOCAL_VERIFIED_FOUNDATION`; authoritative Testnet account reads are wired, while private/public WebSocket fusion, live valuation and real Quick Trade venue submission remain pending.

- Added normalized Bybit UNIFIED wallet and Deribit account-summary snapshots backed by a central refresh manager. Browser GET requests only read cached snapshots; background and user-triggered refreshes are coalesced per venue.
- Missing, malformed or negative source fields remain `unknown`. Bybit derived capacity uses exact decimal subtraction and never treats an empty field as zero.
- Added authenticated account/status endpoints and CSRF-protected controlled refresh. Failures keep the last known snapshot, expose stale/error health without private diagnostics, and never return exchange credentials.
- Added runtime-derived System Status data, snapshot/market ages, request metrics, safe build timestamp/Git commit and uptime. The UI refresh action now calls the controlled endpoint and About displays backend build metadata.
- Bybit/Deribit account clients are created once by the Web process and refreshed on the configured `ACCOUNT_RECONCILE_INTERVAL`; account data becomes stale after two missed intervals.

| Check | Result |
|---|---|
| `go test -race ./internal/accountstate ./internal/webconsole ./cmd/venuewire` | PASS — 66 tests, 3 packages |
| `npm --prefix web run typecheck` | PASS |
| `npm --prefix web test -- --run` | PASS — 3 tests |
| External Testnet account/API operations | `NOT_RUN` |

### V3.1 Phase C2 — real Spot quote and submission adapters

Status: `LOCAL_VERIFIED_FOUNDATION`; Web Quick Trade now uses real adapter implementations, while reconciliation-backed terminal lifecycle and startup recovery remain pending.

- Added exact Bybit Spot instrument, depth, account fee-rate and no-borrow capacity reads. Orders use `category=spot`, Limit IOC and explicit `isLeverage=0` with the durable VenueWire client order ID.
- Added exact Deribit `ETH_BTC` Spot instrument/order-book reads, metadata taker commission and conservative account capacity. Orders use `private/buy` or `private/sell` with `immediate_or_cancel` and the durable VenueWire label.
- Metadata is cached with a bounded TTL and hashed revision. Missing/invalid precision, fee, capacity, instrument scope, book scope or timestamp fails closed.
- Explicit venue/API rejections become `Rejected`; transport uncertainty and acknowledgements without an order ID remain `Unknown`. Neither adapter retries submissions.
- Web startup now constructs the real four-route quote service, durable application, V3/V3.1 caps and global/session/concurrent Demo limits.
- Public error codes now align to `STALE_MARKET_DATA`, `INSUFFICIENT_SPOT_BALANCE`, `QUOTE_CHANGED` and `ACCOUNT_BUSY`; private provider causes remain unwrap-able for diagnostics but are not sent to the Browser.

| Check | Result |
|---|---|
| `go test ./...` | PASS — 340 tests, 21 packages |
| `go vet ./...` | PASS |
| `go build -tags webui -o /tmp/venuewire-web-spot ./cmd/venuewire` | PASS |
| External Testnet market/account/order operations | `NOT_RUN` |

### V3.1 Phase C3 — reconciliation-backed lifecycle and startup recovery

Status: `LOCAL_VERIFIED`; private WebSocket event fusion remains pending, but every accepted Browser intent now has polling/manual recovery without resubmission.

- Added centralized Bybit/Deribit Spot order and execution reconciliation using venue order ID plus durable client ID/label. Browser Recheck and a single periodic startup/runtime recovery loop share the same serialized reconciler.
- Recovery never resubmits. A recovered `Created` intent with no send attempt becomes locally `Rejected`; attempted intents without conclusive evidence remain `Unknown` and retain the global concurrent slot.
- Bybit and Deribit fills/fees are aggregated with exact rational arithmetic. Source-asset fees increase actual debit; destination-asset fees reduce net received.
- IOC zero-fill resolves to `CANCELLED_NO_FILL`; IOC partial-fill resolves terminal `Cancelled` with `PARTIALLY_FILLED_CANCELLED` plus fill details. Later authoritative full-fill evidence can supersede a cancellation race.
- Terminal reconciliation triggers a coalesced account refresh and records `SYNCED` or `STALE` balance state. Venue lookup failures return safe `VENUE_RECOVERING` without mutating the intent.
- Recheck validates session/CSRF, existence and per-intent rate limits. All terminal/unknown state remains durable across restart.

| Check | Result |
|---|---|
| `go test ./...` | PASS — 346 tests, 22 packages |
| `go test -race ./internal/tradereconcile ./internal/intent ./internal/webconsole ./cmd/venuewire` | PASS — 74 tests, 4 packages |
| `go vet ./...` | PASS |
| `go build -tags webui -o /tmp/venuewire-web-reconcile ./cmd/venuewire` | PASS |
| External Testnet reconciliation/order operations | `NOT_RUN` |

### V3.1 Phase C4 — exchange streams and Browser WebSocket state contract

Status: `LOCAL_VERIFIED`; live valuation and the remaining observability/failure-fixture work continue in later phases.

- Added one shared Bybit Spot public order-book stream, Bybit wallet/order/execution private streams, Deribit `ETH_BTC` public book and Deribit portfolio/order/trade private streams. Browser sessions do not create exchange connections.
- Private reconnect and relevant private events trigger coalesced authoritative account refresh or trade reconciliation. They never submit or resubmit orders.
- Added runtime-derived public/private stream state, event timestamps and independent reconnect counters. Initial connection, live, stale and reconnecting states are covered without treating TCP connected as data freshness.
- Added a non-blocking ordered runtime broker. Slow subscribers receive `resync.required`; exchange event loops do not wait for Browser clients.
- Browser WS registers before snapshot creation, sends a normalized initial `snapshot`, then continuous per-connection `seq` envelopes with stable process `instanceId`, `stateRevision`, venue and account alias. Bounded `subscribe`, `unsubscribe`, `resync` and `ping` controls never accept orders.
- Browser clients detect sequence/instance discontinuity, request a new snapshot and reject older account revisions. Session expiration remains absolute; session logout closes its sockets. Limits are five sockets per session and 500 globally.
- Bybit wallet events are decoded only as safe refresh triggers; raw private payloads and credentials are never forwarded to Browser clients.

| Check | Result |
|---|---|
| `go test -race ./internal/runtimeevent ./internal/accountstate ./internal/ws ./internal/deribit ./internal/tradereconcile ./internal/webconsole ./cmd/venuewire` | PASS — targeted packages |
| `go test ./...` | PASS — 362 tests, 23 packages |
| `go vet ./...` | PASS |
| `npm --prefix web run typecheck` | PASS |
| `npm --prefix web test` | PASS — 3 tests |
| `npm --prefix web run build` | PASS |
| External Testnet WebSocket/order/deployment operations | `NOT_RUN` |

### V3.1 Phase C5 — live valuation and runtime trade metrics

Status: `LOCAL_VERIFIED`; deterministic failure-scenario coverage, Browser E2E and final handoff artifacts remain pending.

- Added a central exact-decimal USD valuation engine driven independently from account quantities. Public price updates never mutate exchange-reported balance, equity or valuation quantity, and out-of-order prices cannot replace newer marks.
- Preserved exchange-reported per-asset and account USD values separately from local marks. A local total is exposed only with complete price coverage; partial coverage exposes only a priced subtotal and names every unpriced asset.
- Added Deribit Testnet `btc_usd` and `eth_usd` public index subscriptions, configured price expiry and `WEB_PUSH_INTERVAL` coalescing that retains only the newest update per asset. USDT is not assumed to equal one USD.
- Price expiry is observable without a new market message and emits one revision/event per valuation transition. Browser `valuation.updated` events carry the normalized account snapshot so the shared account store updates without a follow-up REST fetch.
- Split public/private receive times from meaningful-event times and exposed per-stream reconnect counters. Private stream liveness uses received heartbeat/message freshness, so a quiet account is not treated as offline merely because it has no order or wallet event.
- Added REST order RTT, ACK-to-first-order-event, ACK-to-first-execution-event, order request error, rate-limit, reconciliation-status and discrepancy metrics. Private events that arrive before the submission callback are retained briefly and correlated by client or venue order ID.
- System Status displays the normalized stream, latency, rate-limit and reconciliation metrics. Account assets distinguish local USD marks from exchange-reported USD values, and Recent Trades shows persisted lifecycle elapsed time.

| Check | Result |
|---|---|
| `go test -race ./internal/accountstate ./internal/ws ./internal/deribit ./internal/quicktrade ./internal/tradereconcile ./internal/webconsole ./cmd/venuewire -count=1 -timeout=180s` | PASS — 174 tests, 7 packages |
| `go test ./... -count=1 -timeout=180s` | PASS — 374 tests, 23 packages |
| `go vet ./...` | PASS |
| `npm --prefix web run typecheck` | PASS |
| `npm --prefix web test` | PASS — 3 tests |
| `npm --prefix web run build` | PASS |
| `go build -tags webui -o /tmp/venuewire-web-c5 ./cmd/venuewire` | PASS |
| External Testnet valuation/order/deployment operations | `NOT_RUN` |

## Phase status

| Phase | Status | Evidence |
|---|---|---|
| 0 — Baseline audit | Complete | `docs/upgrade/BASELINE_AUDIT.md`; `a52e00f` |
| 1 — Routing, identity, config, migration | Complete | compound identity and reversible v1→v2 migration; `2657a27` |
| 2 — Deribit HTTP/auth/account | Complete | strict JSON-RPC, token/rate-limit behavior, Testnet reads; `64769d4` |
| 3 — Deribit WebSocket | Complete | heartbeat, gap/reconnect, public/private recovery; `112b422` |
| 4 — Planned HTTP/WS orders | Complete | plan→confirm and independent read verification; `6409ca9` |
| 5 — Persistence and recovery | Complete | ambiguity, pagination, restart reconciliation; `b332b9c` |
| 6 — Multi-venue CLI/E2E | Complete | failure-isolated aggregation and command runner; `3e1d93b` |
| 7 — R1 acceptance/hardening | Complete | real HTTP/WS minimum-size lifecycles and cleanup; `6e785ea` |
| 8 — Deribit FIX dialect/mock | Complete | auth/session/recovery/SecurityList/D/F/G/8/9 and local mock; `22f6044` |
| 9 — FIX Testnet validation | Complete | real Logon and metadata-minimum D/G/F lifecycle with JSON-RPC verification; `735a85a` |
| 10 — Final hardening/docs/handoff | Complete | COD, segmented ticks, private reducer, full WS edit/cancel, every-command E2E, redirect/allowlist hardening, root docs; implementation through `9de4c45`, Phase 10 docs `39044a9` |

## Final automated verification

Run after the final implementation changes:

| Check | Result |
|---|---|
| `go test ./... -count=1 -timeout=90s` | PASS — 239 tests, 16 packages |
| `go test -race ./... -count=1 -timeout=120s` | PASS — 239 tests, 16 packages |
| `go vet ./...` | PASS |
| `go build -o ./bin/venuewire ./cmd/venuewire` | PASS |
| `bash -n scripts/e2e/run_multi_venue_e2e.sh` | PASS |
| ShellCheck | `BLOCKED_TOOLING` — executable not installed |
| repository secret/env checks | PASS — `.env` and key material ignored/untracked |

The test count includes bounded fuzz seeds for FIX framing/parser and Deribit JSON-RPC envelope decoding. Repeated targeted runs also covered local FIX cancellation/state isolation, reordered WS responses, early private events, COD, segmented ticks, and HTTP/WS JSON-number encoding.

## Final external Testnet verification

The final complete run was 2026-09-09T19:37Z–19:39Z using code commit `d7f0031`; its migration assertion correction is commit `d65c1cc`.

| Surface | Result |
|---|---|
| Bybit REST and public/private WebSocket | PASS |
| Bybit minimum-size ETHUSDT fill/fee/reduce-only cleanup | PASS |
| Deribit HTTP JSON-RPC and public/private WebSocket | PASS |
| Deribit HTTP create/edit/cancel | PASS |
| Deribit WS create/edit/cancel, with HTTP independent reads | PASS |
| Multi-venue aggregation/failure isolation | PASS |
| v1 migration dry-run/apply/restore in isolated state | PASS |
| Deribit connection COD enable plus same-connection query | PASS |
| Deribit FIX `LOCAL_TESTED` | PASS |
| Deribit FIX `TESTNET_LOGON` | PASS |
| Deribit FIX `TESTNET_ORDER_FLOW` | PASS |
| Bybit live FIX | `BLOCKED_GATE` — separate RSA/whitelist access not enabled |

Runner result:

```json
{"result":"PASS","independentReads":true,"privateEvents":true,"positionsZero":true,"fix":{"bybit":"BLOCKED_GATE","deribit":"PASS"}}
```

Latest minimum-fill fees:

| Venue | Instrument | Entry / cleanup | Unit/collateral | Entry fee | Cleanup fee | Final position |
|---|---|---:|---|---:|---:|---:|
| Bybit Testnet | ETHUSDT linear | 0.01 / 0.01 | ETH, USDT collateral | 0.01367245 USDT | 0.01367234 USDT | 0 |
| Deribit Testnet | BTC-PERPETUAL | 10 / 10 | USD notional, BTC settlement | 0.00000006 BTC | 0.00000006 BTC | 0 (`direction=zero`) |

Every write was followed by a distinct read. WS amend independently read `open` at the new price; WS cancel independently read `cancelled`. The private stream observed 10 notifications, reported connection COD enabled, and persisted canonical orders/trades through the reconciliation reducer. A final read after the runner found zero open orders and zero nonzero positions on both venues.

## Intentional deviations and explicit limitations

- Per user direction, credentials are named `DERIBIT_API_KEY` / `DERIBIT_API_SECRET`, rather than the specification draft's `DERIBIT_CLIENT_ID` / `DERIBIT_CLIENT_SECRET` examples.
- Per user direction, old `RUN_BYBIT_INTEGRATION` / `RUN_BYBIT_WS_INTEGRATION` aliases were removed without compatibility behavior. Symmetric venue read/trading/FIX gates are authoritative.
- Bybit live FIX remains externally blocked and is never reported as PASS. Deribit R2 is independently complete at `TESTNET_ORDER_FLOW`.
- No dashboard existed at baseline, so the preserved/extended user surface is the CLI.
- The supported Deribit product scope remains BTC/ETH perpetuals with native collateral. Options, dated futures, Mainnet, transfers, withdrawals, and smart routing remain out of scope.

Detailed evidence and the mandatory test-matrix audit are in `docs/upgrade/VALIDATION_REPORT.md`.
