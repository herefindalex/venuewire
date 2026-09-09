# Multi-venue implementation status

Last updated: 2026-09-09

The archived Bybit-only phase history remains in `docs/1_bybit/IMPLEMENTATION_STATUS.md`. This file tracks the incremental Bybit + Deribit upgrade defined by `docs/2_deribit/DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md`.

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
| 10 — Final hardening/docs/handoff | Complete | COD, segmented ticks, private reducer, full WS edit/cancel, every-command E2E, redirect/allowlist hardening, root docs; `7230921`, `d7f0031`, `d65c1cc`, `5719b37`, `a59a0d8`, `93c1c05`, `14dfb89`, `9de4c45`, and the Phase 10 documentation commit |

## Final automated verification

Run after the final implementation changes:

| Check | Result |
|---|---|
| `go test ./... -count=1 -timeout=90s` | PASS — 239 tests, 16 packages |
| `go test -race ./... -count=1 -timeout=120s` | PASS — 239 tests, 16 packages |
| `go vet ./...` | PASS |
| `go build -o ./bin/bybitctl ./cmd/bybitctl` | PASS |
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
