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
