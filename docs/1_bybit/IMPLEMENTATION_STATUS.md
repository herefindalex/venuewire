# Implementation Status

Last updated: 2026-09-09

| Phase | Status | Verification |
|---|---|---|
| 0 — Repository / Safety Scaffold | Complete | `go test ./...` (15 tests), `go vet ./...`, smoke build, and scoped credential/PEM scan pass |
| 1 — REST V5 | Complete | REST fixture/unit tests pass; live authenticated Testnet metadata, place, query, cancel, terminal-state, execution, and position lifecycle passed on 2026-09-09; IDs and rate-limit metadata were observed |
| 2 — Public WebSocket | Complete | Five-minute live Testnet soak for trades + orderbook passed 2026-09-09; forced EOF, resubscribe, stale connection, bounded queue, cancellation, and race tests pass |
| 3 — Private WebSocket + Order State | Complete | Private auth/subscription fixtures, typed events, state transitions, execution deduplication, cancel/fill race, overflow recovery, persistence/reload, and race tests pass; a live REST-created Testnet order advanced to `Cancelled` through private events and persisted state on 2026-09-09 |
| 4 — Reconciliation | Complete | All eight fixture scenarios plus ambiguous state, errors, idempotency, and persisted restart repair pass; live Testnet stop → external cancel → restart reconciliation repaired `New` to `Cancelled` on 2026-09-09 |
| 5 — FIX Codec | Complete | Known fixture round-trip, strict/lenient parsing, BodyLength, CheckSum, header order, duplicate/unknown tags, fragmented/concatenated framing, size bounds, and fuzz target pass |
| 6 — FIX Session | Complete | TLS 1.2+ transport, RSA-SHA256, Logon/Logout, idle Heartbeat, TestRequest echo, sequence anomalies, access denial, and race tests pass |
| 7 — FIX Orders / Execution Reports | Complete | Current Bybit D/F/XAR and 8/XCA/XAA flows; accept/reject, partial/full fill, cancel, fill race, amend, correlation, and local mock tests pass |
| 8 — FIX Recovery / Bybit Behavior | Complete | Disconnect-after-New creates Session 2 at seq 1, invokes REST-style reconciliation after Logon ACK, repairs Filled state, and uses no resend/gap-fill |
| 9 — Documentation / Interview Demo | Complete | README, implemented architecture, current protocol notes, six-part demo transcript, security/troubleshooting guidance, and CLI mock demo verified |

## Specification Deviations

- Bybit's current FIX order-amend flow uses `AtomicReplaceRequest(XAR)` and `OrderAmendAck(XAA)`, not standard FIX `OrderCancelReplaceRequest(G)`. Phase 7 follows the current official Bybit protocol and documents the difference in `docs/bybit/protocol-notes.md`.
- Read-only `account info` and `account balances` commands were added after Phase 9 at user request. They are additive to the mandatory scope and use authenticated V5 `/v5/account/info` and `/v5/account/wallet-balance` against the fixed Testnet host.

## External Acceptance Evidence

Public REST server-time and instrument requests passed against Bybit Testnet on 2026-09-09. An authenticated current-orders query also passed on 2026-09-09. The required five-minute public WebSocket soak passed for both trades and orderbook, including clean shutdown.

Fresh Testnet HMAC credentials were recreated locally on 2026-09-09 in a gitignored `../../.env` restricted to mode `0600`; values are never printed or copied into the repository. After funding the associated Unified account, the mandatory authenticated Linear order lifecycle passed: metadata-derived placement, query, cancellation, terminal-state query, executions, and positions. A concurrent private WebSocket processed the corresponding order lifecycle and persisted the final `Cancelled` state. The live offline-change/restart scenario also passed: an isolated snapshot recorded `New`, the order was cancelled while its stream was stopped, and restart reconciliation reported one discrepancy and repaired it to `Cancelled`. Separate Spot IOC demonstrations completed through `venuewire`, including BTC→USDT→ETH with actual execution-fee reporting. Credentials previously shared in chat remain explicitly disallowed and were not used.

Live FIX is optional and remains unverified because Bybit requires separate RSA credentials and whitelist access. All mandatory FIX behavior passes against the injectable local mock.

## Latest Local Gate

- `go test ./...`: 114 passed across 11 packages.
- `go test -race ./...`: 114 passed across 11 packages.
- `go vet ./...`: passed.
- `go build -o /tmp/venuewire ./cmd/venuewire`: passed.
- `go test ./internal/fix -run '^$' -fuzz FuzzFIXParser -fuzztime 3s -parallel 2`: passed.
- Automated credential/private-key scan: passed.
- `venuewire fix mock-demo`: reached Filled with two distinct persisted executions.
