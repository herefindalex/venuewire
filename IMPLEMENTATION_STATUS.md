# Multi-Venue Implementation Status

Specification: `docs/2_deribit/DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md`

| Phase | Status | Evidence |
|---|---|---|
| 0 — Baseline audit and safety scaffold | Complete | `docs/upgrade/BASELINE_AUDIT.md`; 115 tests pass normally and with `-race`; vet/build pass |
| 1 — Venue model, configuration, and migration | Complete | Compound identity/collision tests; exact Deribit Testnet guards; nonzero risk validation; explicit dry-run/backup/restore v1→v2 migration; 138 tests pass |
| 2 — Deribit JSON-RPC, auth, metadata, and account reads | Complete | Strict envelope/ID and typed-error tests; synchronized token refresh; metadata/account/position CLI; 14 account currencies confirmed on Testnet |
| 3 — Deribit public/private WS, book, and heartbeat | Complete | 153 normal/race tests; reconnect/resubscribe, recovery callback, bounded queues, book gaps; live public events, private ready, and heartbeat test response |
| 4 — HTTP/WS plan→execute order lifecycle | Complete | Persisted 30s plans, metadata/risk revalidation, cross-process claim, HTTP+WS writes, private WS event correlation, independent HTTP verification, minimum-size live cancellations |
| 5 — Intent persistence, recovery, pagination, and reconciliation | Complete | Durable intent states/cursors; zero/one/multiple label recovery; fee/trade-ID dedupe; bounded pagination; startup/private-ready/manual reconciliation; live idempotence |
| 6 — Multi-venue routing, read aggregation, and CLI E2E | Complete (non-FIX) | Shared `--venue` routing, failure-isolated status/portfolio, metadata-minimum HTTP/WS lifecycles, canonical fee checks, private events, reduce-only zero-position cleanup |
| 7 — Failure isolation, observability, and shutdown | Complete | Per-account bounded RPC slots, safe read cooldown, metadata cache, WS metrics/causes, failure-isolated aggregation, signal-bounded stream shutdown, durable in-flight intents |
| 8 — Deribit FIX codec/auth/session | Not started | — |
| 9 — Deribit FIX orders and JSON reconciliation | Not started | — |
| 10 — Final documentation and validation | Not started | — |

## Validation levels

| Surface | Status |
|---|---|
| Existing Bybit regression | PASS |
| Deribit authenticated account-summary probe | PASS — read-only credential/asset check on 2026-09-09 |
| Deribit R1 local tests | IN_PROGRESS — Phase 1 local tests pass |
| Deribit R1 Testnet reads | PASS — HTTP time, metadata, 14-currency account aggregate, and positions on 2026-09-09 |
| Deribit R1 Testnet order lifecycle | PASS — HTTP + WS passive flows and 10 USD live fill/cleanup independently verified |
| Deribit FIX implemented | NOT_RUN |
| Deribit FIX local mock | NOT_RUN |
| Deribit FIX Testnet logon | NOT_RUN |
| Deribit FIX Testnet order flow | NOT_RUN |

## Deviations

The former `RUN_BYBIT_INTEGRATION` and `RUN_BYBIT_WS_INTEGRATION` test gates were intentionally removed without compatibility aliases. Use the symmetric read/trading/FIX gates documented in `.env.example`. User-approved choices are recorded in `docs/upgrade/BASELINE_AUDIT.md`.
