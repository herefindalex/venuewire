# VenueWire V3.1 protocol notes

These notes describe the Web Console behavior layered on the existing V2 Bybit/Deribit connectors. The earlier HTTP/JSON-RPC, WebSocket and FIX dialect evidence remains in `docs/upgrade/PROTOCOL_NOTES.md`.

## Scope and identity

- Browser scope is always Testnet, one configured shared identity and one configured account alias per enabled venue.
- Every trade stores VenueWire intent ID, deterministic client order ID, venue order ID when known, venue, environment, account alias, instrument and route.
- The Browser selects a venue explicitly. Symbols never select a venue, and no order automatically fails over to another venue or transport.
- Browser Web is the only owner of its quote, quota reservation and submission workflow. The lower-usage CLI remains independent; V3.1 deliberately does not add cross-process CLI/Web coordination.

## Account state and valuation

- Bybit uses the UNIFIED wallet snapshot. Deribit uses extended account summaries and preserves native currency scope.
- REST/HTTP is the authoritative startup and recovery snapshot. Private events trigger bounded refresh or reconciliation; they are not assumed to contain a complete initial account.
- Balance, equity, liability, locked amount, verified/derived availability and valuation quantity are separate fields. Missing source values stay unknown, never numeric zero.
- Exchange-reported USD totals and per-asset values remain separate from local marks.
- Local valuation is exact-decimal `valuationQuantity × public USD price`. Price changes never mutate account quantities or snapshot observation time.
- Deribit Testnet `deribit_price_index.btc_usd` and `deribit_price_index.eth_usd` are the current live USD sources. USDT is not hard-coded to one USD. Unsupported or expired price paths remain unpriced.
- A local `totalUsd` exists only when every non-zero valuation quantity has a fresh price. Partial coverage exposes only `pricedSubtotalUsd` and `unpricedAssets`; it must not be presented as complete account equity.
- `VALUATION_PRICE_MAX_AGE` controls expiry. Updates are coalesced per asset at `WEB_PUSH_INTERVAL`, with the newest received price retained. Older prices cannot overwrite newer prices.

## Spot Quick Trade

The four fixed routes are:

| Route | Venue instruction | Source budget |
|---|---|---|
| Bybit USDT→BTC | Buy base BTC on `BTCUSDT` | USDT |
| Bybit BTC→USDT | Sell base BTC on `BTCUSDT` | BTC |
| Deribit BTC→ETH | Buy base ETH on Spot `ETH_BTC` | BTC |
| Deribit ETH→BTC | Sell base ETH on Spot `ETH_BTC` | ETH |

All amounts use `math/big.Rat` planning and decimal strings at protocol boundaries. Metadata supplies tick, quantity step, minimum and maximum constraints. Buy protection rounds the worst price upward without exceeding the 50-bps bound; sell protection rounds downward. Quantity never rounds upward beyond the source budget.

Review freezes route, book metadata revision summary, book quote, protected Limit IOC price, base quantity, fee policy, source-debit upper bound and five-second expiry. Confirm revalidates the frozen values; it does not silently create a different quote.

Bybit submits Spot `isLeverage=0`. Deribit submits Spot `private/buy` or `private/sell` with `immediate_or_cancel`. Neither adapter retries a submission. Explicit native rejections are `Rejected`; write/transport uncertainty without conclusive identity is `Unknown`.

## Durable intent, quotas and recovery

- Quote confirmation atomically consumes the quote and persists the intent, request index, hourly/session accounting and global concurrent reservation before dispatch.
- Duplicate click, retry or another tab using the same quote returns the existing trade. A client request ID reused for another quote is rejected.
- Rejected, zero-fill and Unknown accepted intents count toward session/hour quotas. Validation and expired-quote failures before intent acceptance do not.
- `Unknown` retains the one global concurrent slot across restart. Only durable Filled, Cancelled or Rejected evidence releases it.
- Startup, periodic recovery, private reconnect/event triggers and Browser Recheck use the same serialized reconciler. None resubmits.
- Execution evidence is aggregated by exact quantity/price and fee currency. Source-asset fees increase actual debit; destination-asset fees reduce net received; third-asset fees remain explicit. Gross fill determines full versus partial fill, not net received after fee.
- IOC zero fill resolves to `CANCELLED_NO_FILL`; partial then cancel resolves to `PARTIALLY_FILLED_CANCELLED`. Later fill evidence may supersede an earlier cancel observation.
- Terminal trade state and account balance synchronization are separate. The trade can be terminal while `balanceSyncStatus` remains pending or stale.

## Stream and Browser semantics

- The process owns one public and one private connection per enabled venue; Browser connections do not multiply exchange connections.
- Public market freshness uses meaningful market events. Private transport liveness uses received messages/heartbeats so a quiet account is not considered offline; meaningful private-event age is reported separately. Account snapshot freshness remains an independent state.
- Reconnect and bounded-queue overflow trigger recovery. Private data is never silently dropped as authoritative state.
- The runtime event broker is ordered and non-blocking. Slow Browser subscribers receive `resync.required` without blocking exchange loops.
- Browser WebSocket sends a stable process `instanceId`, per-connection contiguous `seq` and state revisions. Reload, instance change, sequence gap or overflow causes a normalized resnapshot. WebSocket controls never submit orders.

## Observability

System Status distinguishes REST, public WS, private WS and account synchronization. It exposes last receive/event ages, per-stream reconnects, REST order RTT, ACK-to-first-order event, ACK-to-first-execution event, request errors, normalized rate-limit state, reconciliation status and discrepancies.

Events received before the submission callback are retained briefly and correlated later by client or venue order ID. Metrics are operational evidence only; they do not change trade state.

## Deliberate limitations

- Mainnet, transfers, withdrawals, smart routing, cross-venue failover, full order entry, charts and strategies are out of scope.
- One shared demo login/history is intentional. Strong CLI/Web or multi-instance reservation coordination is not implemented for this MVP.
- Local fixture verification does not prove current exchange availability, balances, permissions, latency or split-host deployment. Those items remain `NOT_RUN` until explicitly authorized and observed.
