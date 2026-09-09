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
| 8 — Deribit FIX dialect/mock | Complete | distinct SHA-256 Logon, bounded session recovery and resend journal, SecurityList quantity proof, D/F/G/8/9 parsing, local lifecycle mock; commit `22f6044` |
| 9 — FIX Testnet validation | Complete in current worktree | real Testnet Logon plus metadata-minimum D/G/F lifecycle passed with independent JSON-RPC verification and zero exposure |
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
| Deribit FIX Testnet order flow | PASS (`TESTNET_ORDER_FLOW`) |

## Deviations

The former `RUN_BYBIT_INTEGRATION` and `RUN_BYBIT_WS_INTEGRATION` gates were intentionally removed without compatibility aliases. Use the symmetric read/trading/FIX gates documented in `.env.example`. No Phase 8 deviation from the Deribit FIX specification is currently known.

## Phase 8 verification

- Files: `internal/deribitfix`, `internal/deribitfixmock`, and Deribit FIX CLI routing under `cmd/bybitctl`.
- Full normal and race suites: 203 tests passed across 16 packages in each mode; `go vet ./...` and the CLI build also passed.
- Local CLI: `bybitctl --venue deribit fix mock-demo` reached `LOCAL_TESTED`, exercised Logon, TestRequest/Heartbeat, actual resend with `PossDupFlag(43)` and `OrigSendingTime(122)`, SequenceReset, SecurityList multiplier validation, D/G/F and ExecutionReport(8), ending cancelled.
- Live read-only FIX: `connect-testnet` reached `TESTNET_LOGON` against `fix-test.deribit.com:9883`; it did not submit an order.
- Security: reject/logout diagnostics omit inbound free text; credentials, password digest, nonce and RawData are never logged or persisted.
- Deviation: none. Phase 9 remains incomplete until a true FIX order lifecycle is independently verified through canonical JSON-RPC reads.
- Phase 8 handoff target (gated FIX plan/execute, amend/cancel and zero-exposure Testnet lifecycle) was completed in Phase 9 below.

## Phase 9 verification

- User-visible behavior: `order plan --transport fix`, confirmed `order execute`, and `order amend/cancel --transport fix` use only Deribit FIX for the requested mutation. Every FIX write requires `RUN_MULTI_VENUE_E2E=1`, `RUN_DERIBIT_TRADING_TESTS=1`, and `RUN_DERIBIT_FIX_TESTS=1` plus `DERIBIT_FIX_ENABLED=true`; G/F additionally require a persisted connector-owned label/native-ID match.
- Before every live write, JSON instrument metadata and FIX SecurityList prove the USD-units/contract multiplier conversion. No unclear conversion can reach D or G.
- A real BTC-PERPETUAL 10 USD post-only order completed D→G→F on Testnet. Separate JSON-RPC reads proved create `open`, amended price, terminal `cancelled`, zero trades, zero open orders, and `direction=zero` position.
- Full opt-in E2E passed with Deribit FIX `PASS`; unavailable Bybit live FIX was reported `BLOCKED_GATE`, not skipped. The same run independently verified real minimum-size Bybit/Deribit fills, canonical fees, reduce-only cleanup and zero final positions.
- Full normal and race suites each passed 212 tests across 16 packages; vet, build and shell syntax checks passed. ShellCheck was unavailable in the installed toolchain and is recorded rather than silently skipped.
- Live observations added regression coverage: FIX `SettlCurrency` may express USD quote semantics while JSON settlement/commission remain BTC; cancellation reports may omit optional OrderID/OrderQty or first report pending-cancel. The connector uses the independent pre-read identity and waits for JSON terminal state rather than inventing missing values.
- Next: Phase 10 final documentation, handoff, requirement audit and final verification.
