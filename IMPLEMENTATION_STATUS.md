# Multi-venue implementation status

Last updated: 2026-09-10

The archived Bybit-only phase history remains in `docs/1_bybit/IMPLEMENTATION_STATUS.md`. This file tracks the incremental Bybit + Deribit upgrade defined by `docs/2_deribit/DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md`.

## V3 / V3.1 pre-implementation status

The V2 phase history below remains historical evidence. V3/V3.1 runtime implementation has not started.

| Work | Status | Evidence |
|---|---|---|
| V3 Phase 0 baseline inspection | Complete; baseline failure recorded | `docs/v3/BASELINE_AUDIT.md`: 238 pass, 1 security-example failure, 2 skipped; same failure under race detector |
| V3.1 gap analysis and product decisions | Documented | `docs/v3/V3_1_GAP_ANALYSIS.md`; `docs/3_web/VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md` section 26 |
| Canonical configuration examples | Updated specification only | V3 names, `QUOTE_TTL=5s`, V3.1 Demo caps, existing Deribit credential names and VenueWire commands |
| Web / Spot / Demo hardening | Not implemented | Auth, frontend, browser intents/quotas, Spot validation and UI observability remain pending |
| V3/V3.1 frontend and external validation | Not run | No frontend exists yet; no new Testnet orders or deployment operations performed |

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
