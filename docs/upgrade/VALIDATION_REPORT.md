# Multi-venue validation report

No credentials, tokens, signatures, or full authentication payloads are included.

## D-R2-001 — Deribit HTTP reads

- UTC time: 2026-09-09T15:32:22Z
- Build: local `master` worktree after commit `2657a27`
- Venue/environment/account: Deribit/Testnet/`deribit-test`
- Transport: HTTP JSON-RPC
- Commands: `venue deribit time`; `instrument --name BTC-PERPETUAL`; `account balances --currency all`; `positions --currency BTC --kind future`
- Expected: strict JSON-RPC reads succeed; metadata retains amount units; account-scoped aggregate returns the dashboard assets; positions are readable
- Actual: time succeeded; BTC-PERPETUAL active, BTC-settled/USD-quoted, tick 0.5, minimum amount 10, contract size 10; aggregate returned 14 currencies (`BNB,BTC,BUIDL,ETH,EURR,MATIC,PAXG,SOL,STETH,USDC,USDE,USDT,USYC,XRP`); BTC future positions count 0
- Result: PASS
- Evidence: sanitized CLI JSON observed locally; no write gate enabled

## Write/FIX validation

NOT_RUN. A read result does not satisfy any order lifecycle, WebSocket, reconciliation, fee, cleanup, or FIX acceptance item.

## D-R3-001 — Deribit public/private WebSocket reads

- UTC time: 2026-09-09T15:47Z–15:51Z
- Build: local `master` worktree after commit `64769d4`
- Venue/environment/account: Deribit/Testnet/`deribit-test`
- Transport: WebSocket JSON-RPC
- Commands: bounded `public-stream` and `private-stream`
- Expected: public trade/book notifications, authenticated private subscribe readiness, heartbeat test response, no reconnect in healthy window
- Actual: 8-second public run delivered 26 notifications from `trades.BTC-PERPETUAL.100ms` and `book.BTC-PERPETUAL.100ms`; private stream reached one authenticated ready generation; 22-second public run answered one `test_request`; all healthy runs used one connection and zero reconnects
- Result: PASS for read/subscription layer
- Limitation: no private order/execution event was generated in this read-only phase; that evidence remains required in the write E2E

## D-R3-002 — Raw public subscription behavior

- Expected: determine whether unauthenticated `.raw` can be a public default
- Actual: server returned typed code `13778 raw_subscriptions_not_available_for_unauthorized`
- Result: PASS (behavior identified); public defaults changed to `.100ms`, authenticated callers may request `.raw`

## D-R4-001 — Deribit HTTP minimum-size order lifecycle

- UTC time: 2026-09-09T16:03Z–16:06Z
- Venue/environment/account/transport: Deribit/Testnet/`deribit-test`/HTTP write + HTTP read + private WS
- Instrument/amount/unit: `BTC-PERPETUAL`, `10`, USD notional (metadata minimum), BTC settlement
- Expected: plan passes nonzero risk limits; execute requires confirmation; independent HTTP state is open; amend is independently visible; cancel is independently terminal; private events correlate by order ID/label; no residual position
- Actual: two connector-owned post-only orders were created within the 2% price band. Create and amend returned `verified=true`; cancel read returned `cancelled`; private `user.changes.any.any.raw` emitted matching `open` then `cancelled`; trade count and fee were 0; BTC future position count was 0
- Native IDs: sanitized in this report; retained only in local command evidence
- Result: PASS

## D-R4-002 — Deribit WebSocket minimum-size order lifecycle

- UTC time: 2026-09-09T16:12Z
- Venue/environment/account/transport: Deribit/Testnet/`deribit-test`/WebSocket write + canonical HTTP read
- Instrument/amount/unit: `BTC-PERPETUAL`, `10`, USD notional, BTC settlement
- Expected: no transport fallback/retry; HTTP read matches WS order ID and intent label; connector-owned cancel reaches terminal state
- Actual: WebSocket create was independently HTTP-verified, then HTTP cancel was independently read as `cancelled`; no fill was targeted
- Result: PASS

Real execution/fee reconciliation remains NOT_RUN here. Zero trades and zero fee are correct for the post-only cancellation lifecycle but do not satisfy the required live-fill evidence.

## D-R5-001 — Migration and reconciliation

- UTC time: 2026-09-09T16:20Z–16:22Z
- Expected: legacy state migrates only after dry-run; exact backup exists; reconciliation is idempotent; private-ready callback can recover
- Actual: dry-run validated 8 Bybit orders and 2 executions; apply created `state/orders.json.v1.bak`; Deribit reconciliation independently found and applied 3 connector-owned terminal orders; two consecutive reports had zero unresolved/multiple matches and zero new trades; private stream reached ready while running reconciliation
- Result: PASS
- Recovery: `./bin/bybitctl state restore-v1` restores the retained backup; it was not invoked because v2 is the active schema

Crash/ambiguity fixture evidence: local tests cover accepted-before-local-ACK recovery, no-match staying `OutcomeUnknown`, multiple label matches becoming `NeedsReview`, duplicate pages, same-millisecond cursor ordering, and pagination no-progress failure. No unresolved intent is automatically resubmitted.
