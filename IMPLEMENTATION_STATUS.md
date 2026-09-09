# Multi-venue implementation status

Last updated: 2026-09-09

The archived Bybit-only phase history remains in `docs/1_bybit/IMPLEMENTATION_STATUS.md`. This file tracks the incremental Bybit + Deribit upgrade defined by `docs/2_deribit/DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md`.

| Phase | Status | Evidence |
|---|---|---|
| 0 — Baseline audit | Complete | `docs/upgrade/BASELINE_AUDIT.md`; commit `a52e00f` |
| 1 — Routing, identity, config, migration | Complete | compound identity and reversible v1→v2 migration; commit `2657a27` |
| 2 — Deribit HTTP/auth/account | Complete | local tests and Testnet reads; commit `64769d4` |
| 3 — Deribit WebSocket | Complete | local gap/reconnect tests and public/private Testnet evidence; commit `112b422` |
| 4 — Planned HTTP/WS orders | Complete | independent read verification after every mutation; commit `6409ca9` |
| 5 — Persistence and recovery | Complete | ambiguity/pagination/restart reconciliation tests; commit `b332b9c` |
| 6 — Multi-venue CLI/E2E | Complete | failure-isolated aggregation and fully verified non-FIX E2E; commit `3e1d93b` |
| 7 — R1 acceptance/hardening | Complete | normal/race/vet/build gates and live minimum-size fills with reduce-only cleanup; commit `6e785ea` |
| 8 — Deribit FIX dialect/mock | Complete in current worktree | distinct SHA-256 Logon, bounded session recovery and resend journal, SecurityList quantity proof, D/F/G/8/9 parsing, local lifecycle mock |
| 9 — FIX Testnet validation | In progress | real Testnet Logon passed; real FIX order flow and independent JSON-RPC reconciliation not yet run |
| 10 — Final docs/handoff | Not started | `README.md`, demo and Traditional Chinese handoff remain required |

## Validation levels

| Surface | Status |
|---|---|
| Existing Bybit regression | PASS |
| Deribit R1 Testnet reads | PASS — HTTP, public/private WS, account, metadata and positions |
| Deribit R1 Testnet order lifecycle | PASS — HTTP + WS minimum-size lifecycles and real fill/cleanup independently verified |
| Deribit FIX implemented | PASS |
| Deribit FIX local mock | PASS (`LOCAL_TESTED`) |
| Deribit FIX Testnet Logon | PASS (`TESTNET_LOGON`) |
| Deribit FIX Testnet order flow | NOT_RUN |

## Deviations

The former `RUN_BYBIT_INTEGRATION` and `RUN_BYBIT_WS_INTEGRATION` gates were intentionally removed without compatibility aliases. Use the symmetric read/trading/FIX gates documented in `.env.example`. No Phase 8 deviation from the Deribit FIX specification is currently known.

## Phase 8 verification

- Files: `internal/deribitfix`, `internal/deribitfixmock`, and Deribit FIX CLI routing under `cmd/bybitctl`.
- Full normal and race suites: 203 tests passed across 16 packages in each mode; `go vet ./...` and the CLI build also passed.
- Local CLI: `bybitctl --venue deribit fix mock-demo` reached `LOCAL_TESTED`, exercised Logon, TestRequest/Heartbeat, actual resend with `PossDupFlag(43)` and `OrigSendingTime(122)`, SequenceReset, SecurityList multiplier validation, D/G/F and ExecutionReport(8), ending cancelled.
- Live read-only FIX: `connect-testnet` reached `TESTNET_LOGON` against `fix-test.deribit.com:9883`; it did not submit an order.
- Security: reject/logout diagnostics omit inbound free text; credentials, password digest, nonce and RawData are never logged or persisted.
- Deviation: none. Phase 9 remains incomplete until a true FIX order lifecycle is independently verified through canonical JSON-RPC reads.
- Next: implement gated FIX plan/execute, amend and cancel, then run the metadata-minimum Testnet lifecycle and zero-exposure cleanup.
