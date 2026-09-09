# Multi-Venue Implementation Status

Specification: `docs/2_deribit/DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md`

| Phase | Status | Evidence |
|---|---|---|
| 0 — Baseline audit and safety scaffold | Complete | `docs/upgrade/BASELINE_AUDIT.md`; 115 tests pass normally and with `-race`; vet/build pass |
| 1 — Venue model, configuration, and migration | Complete | Compound identity/collision tests; exact Deribit Testnet guards; nonzero risk validation; explicit dry-run/backup/restore v1→v2 migration; 138 tests pass |
| 2 — Deribit JSON-RPC, auth, metadata, and account reads | Complete | Strict envelope/ID and typed-error tests; synchronized token refresh; metadata/account/position CLI; 14 account currencies confirmed on Testnet |
| 3 — Deribit public/private WS, book, and heartbeat | Not started | — |
| 4 — HTTP/WS plan→execute order lifecycle | Not started | — |
| 5 — Intent persistence, recovery, pagination, and reconciliation | Not started | — |
| 6 — Multi-venue routing, read aggregation, and CLI E2E | Not started | — |
| 7 — Failure isolation, observability, and shutdown | Not started | — |
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
| Deribit R1 Testnet order lifecycle | NOT_RUN |
| Deribit FIX implemented | NOT_RUN |
| Deribit FIX local mock | NOT_RUN |
| Deribit FIX Testnet logon | NOT_RUN |
| Deribit FIX Testnet order flow | NOT_RUN |

## Deviations

The former `RUN_BYBIT_INTEGRATION` and `RUN_BYBIT_WS_INTEGRATION` test gates were intentionally removed without compatibility aliases. Use the symmetric read/trading/FIX gates documented in `.env.example`. User-approved choices are recorded in `docs/upgrade/BASELINE_AUDIT.md`.
