# VenueWire V3.1 Browser API

Status: locally verified and externally exercised through the configured HTTPS/WSS origin with authorized Bybit and Deribit Testnet orders.

All routes are same-origin and are intended to be reached only through the configured HTTPS Nginx proxy. Private responses use `Cache-Control: no-store`. Errors contain a stable public code, safe message and correlation `requestId`; stack traces, credentials, provider payloads and session identities are never returned.

## Authentication and request boundary

`POST /api/auth/login` accepts JSON `username` and `password`, requires the exact configured Origin/Host boundary and applies per-client plus globally bounded rate limiting. Success sets the Secure, HttpOnly, SameSite=Strict `__Host-trading_session` cookie and returns the display username, session-bound CSRF token, absolute expiry and `readOnly` state. Invalid username and password have the same response.

`GET /api/auth/me` restores the public session view after reload. `POST /api/auth/logout` requires session, Origin and CSRF; it revokes the server session, expires the cookie and closes Browser WebSockets belonging to that session.

All routes below require the session cookie. Every state-changing route also requires exact Origin, JSON where applicable and `X-CSRF-Token`. URL tokens are unsupported.

## Venue, account and status

| Method and path | Response semantics |
|---|---|
| `GET /api/venues` | Enabled Testnet venue IDs, account aliases, capability flags, default venue and global Web trading state; never endpoints or credentials. |
| `GET /api/venues/{venue}/account` | Cached normalized account snapshot only; Browser requests do not call an exchange. |
| `POST /api/venues/{venue}/account/refresh` | Coalesced bounded authoritative refresh; safe stale/error state preserves the last snapshot. |
| `GET /api/system/status` | Per-venue REST/public WS/private WS/account sync state, receive/event ages, reconnects, order/event latency, request/rate-limit and reconciliation metrics plus non-sensitive build data. |

An account snapshot carries venue, Testnet environment, alias, account type, monotonic process revision, snapshot time, exchange-reported total/as-of time, local complete total or partial priced subtotal, valuation basis/completeness/unpriced assets, liability status, nullable derivative-position presence with evidence, and asset rows. Asset quantities remain exchange-authoritative; local `usdValue` is separate from `exchangeReportedUsdValue`. Unknown decimal values are omitted rather than converted to zero.

## Quick Trade

`POST /api/venues/{venue}/quotes` accepts only:

```json
{"routeId":"bybit-usdt-btc","amount":"100"}
```

Identity, account, environment, instrument, side, units, metadata, fee source, capacity and order parameters are resolved server-side. The response freezes exact decimal base quantity, reference bid/ask, visible-depth estimate, fee/reserve information, source-debit upper bound, 50-bps protection and absolute five-second expiry. Stale/crossed/incomplete books, invalid metadata, unknown fees or insufficient verified capacity fail closed or return an explicitly non-executable review.

Supported route IDs:

| Venue | Route | Venue operation |
|---|---|---|
| Bybit | `bybit-usdt-btc` | Buy BTC on `BTCUSDT`, budget in USDT |
| Bybit | `bybit-btc-usdt` | Sell BTC on `BTCUSDT`, budget in BTC |
| Bybit | `bybit-usdt-eth` | Buy ETH on `ETHUSDT`, budget in USDT |
| Bybit | `bybit-eth-usdt` | Sell ETH on `ETHUSDT`, budget in ETH |
| Deribit | `deribit-usdc-btc` | Buy BTC on Spot `BTC_USDC`, budget in USDC |
| Deribit | `deribit-btc-usdc` | Sell BTC on Spot `BTC_USDC`, budget in BTC |

`POST /api/trades/confirm` accepts only `quoteId` and a Browser-generated `clientRequestId`. It requires `WEB_TRADING_ENABLED=true`, revalidates the frozen quote and atomically persists the intent, quota accounting and concurrent reservation before the one venue submission attempt. Duplicate request IDs or a consumed quote return the existing logical trade. Transport uncertainty becomes `Unknown`; explicit venue rejection becomes `Rejected`.

## Shared trade history and recovery

| Method and path | Semantics |
|---|---|
| `GET /api/trades?limit=50` | Newest-first shared demo history, bounded to 1–100 rows. |
| `GET /api/trades/{intentId}` | One public lifecycle with VenueWire/client/venue IDs, quantities, actual fills, fees, net received, sync state and timestamps. |
| `POST /api/trades/{intentId}/recheck` | Rate-limited authoritative query for unresolved/selected trade; never submit, retry or force-resolve. |

Public trade DTOs omit login identity and session IDs. `Unknown` remains active and consumes the single global concurrent slot until Filled, Cancelled or Rejected evidence is durable.

## Browser WebSocket

`GET /api/ws` validates the session, exact Origin/Host and trusted proxy boundary during upgrade. There are at most five connections per session and 500 process-wide.

Each connection receives `session.ready` and then a normalized `snapshot`. Subsequent envelopes contain:

```json
{
  "schemaVersion": 1,
  "instanceId": "stable-for-this-process",
  "seq": 12,
  "type": "account.updated",
  "venue": "bybit",
  "accountAlias": "bybit-demo",
  "stateRevision": 42,
  "sentAt": "2026-09-10T20:00:00Z",
  "payload": {}
}
```

Event types are `account.updated`, `valuation.updated`, `trade.updated`, `venue.health.updated` and `resync.required`. A valuation event includes the normalized account snapshot. Sequence is per connection; `instanceId`, gaps or overflow require `resync`. The Browser rejects older account revisions. Client controls are bounded to `subscribe`, `unsubscribe`, `resync` and `ping`; WebSocket never accepts a trade.

## Trusted proxy contract

The direct TCP peer must match one explicit single-host entry in `WEB_TRUSTED_PROXY_CIDRS`. Nginx overwrites `X-Forwarded-For` and `X-Real-IP` with one client IP, sets `X-Forwarded-Proto: https` and preserves the configured public Host. VenueWire rejects untrusted peers, forwarded chains, HTTP public origin, mismatched Host, cross-origin upgrades/writes and malformed content types before the application handler runs.
