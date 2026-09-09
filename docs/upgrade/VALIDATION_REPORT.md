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
