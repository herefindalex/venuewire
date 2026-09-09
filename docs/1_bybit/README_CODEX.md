# Bybit CEX Connector Lab — Codex Entry Point

## Purpose

Build a **real, testable CEX connectivity project in Go** using Bybit Testnet.

The project must demonstrate hands-on experience with:

1. **REST V5** — authenticated order management.
2. **WebSocket V5** — public market data and private order/execution updates.
3. **FIX 4.4** — a minimal but complete client implementation covering codec, session lifecycle, order entry, execution reports, sequence handling, heartbeat, reconnect, and reconciliation.

This is not a UI project and not a trading-strategy project. The goal is exchange connectivity, order lifecycle correctness, resilience, and protocol understanding.

## Read First

Read these files before modifying code:

1. `01_PRODUCT_SPEC.md`
2. `02_ARCHITECTURE.md`
3. `03_PROTOCOL_SPEC.md`
4. `04_IMPLEMENTATION_PLAN.md`
5. `05_TEST_PLAN.md`
6. `06_SECURITY_OPERATIONS.md`

## Hard Rules

- Use **Go**.
- **Testnet only.**
- Never hard-code credentials.
- Never commit `../../.env`, API keys, API secrets, RSA private keys, or generated credentials.
- The API key/secret previously shared in chat must be treated as compromised and **must not be used**.
- REST + WebSocket must run against real Bybit Testnet.
- FIX live connectivity is optional because Bybit requires whitelist access; the FIX implementation itself is mandatory and must be fully testable against a local mock server.
- Bybit FIX currently supports **Spot only**. Do not pretend FIX supports Linear/Perpetual.
- Prefer correctness, observability, and testability over abstraction.
- No automated strategy that places orders based on market signals.
- All order placement must be initiated explicitly by CLI/demo commands.

## Codex Working Style

Work phase-by-phase. After each phase:

1. Run formatting/lint/tests.
2. Update `IMPLEMENTATION_STATUS.md`.
3. Record any deviation from the specification.
4. Do not silently weaken acceptance criteria.

If an API behavior differs from this spec, prefer the latest official Bybit documentation and document the difference.

## Definition of Done

The project is done only when all mandatory acceptance tests in `05_TEST_PLAN.md` pass.

A successful final demo should show:

```text
Public WS trade/orderbook
        |
        v
REST authenticated order submission
        |
        v
REST ACK with orderId/orderLinkId
        |
        v
Private WS order update
        |
        v
Private WS execution/fill update (when applicable)
        |
        v
Local order state
        |
        v
Disconnect / restart
        |
        v
REST reconciliation
```

And separately:

```text
Local FIX mock server
        ^
        |
TLS/FIX session
Logon -> Heartbeat/TestRequest -> NewOrderSingle
        -> ExecutionReport -> Cancel -> Logout
```
