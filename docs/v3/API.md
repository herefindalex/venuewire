# VenueWire V3.1 browser API

Status: Phase A authentication boundary is locally verified. Account, quote and trade contracts remain pending.

All routes use the single configured HTTPS origin through the trusted Nginx proxy. Private responses use `Cache-Control: no-store`. Errors contain a stable public code, safe message and correlation `requestId`; internal errors and exchange credentials are never returned.

## Authentication

### `POST /api/auth/login`

Requires `Content-Type: application/json` and exact `Origin`. The JSON body contains `username` and `password`. A successful response sets the `__Host-trading_session` Secure, HttpOnly, SameSite=Strict cookie and returns:

```json
{
  "username": "configured display name",
  "csrfToken": "opaque session-bound token",
  "expiresAt": "2026-09-10T20:00:00Z",
  "readOnly": true
}
```

Invalid username and password produce the same `401` response. Failed logins are limited per trusted client IP, with a bounded global limiter; blocked requests return `429` and `Retry-After`.

### `GET /api/auth/me`

Requires a valid session and returns the same non-secret session view. The frontend uses this after reload to obtain its CSRF token and absolute expiry.

### `POST /api/auth/logout`

Requires session, exact `Origin` and `X-CSRF-Token`. Returns `204`, revokes the server-side session, expires the cookie and closes Browser WebSockets attached to that session.

## Venue discovery

### `GET /api/venues`

Requires a valid session. Returns only enabled venue IDs, Testnet environment, configured account aliases, default venue and the Web trading switch. It never returns endpoint credentials.

## Quick Trade

### `POST /api/venues/{venue}/quotes`

Requires session, exact Origin and CSRF. The browser supplies only `routeId` and decimal source `amount`; identity, account, environment, side, instrument, fees, price protection and order parameters are resolved by the backend. An executable quote contains the frozen Limit IOC parameters, reference bid/ask, visible-depth estimate, fee source, worst source debit, 50-bps protection and absolute 5-second expiry. Non-executable fee previews are explicitly marked and cannot be confirmed.

### `POST /api/trades/confirm`

Requires session, exact Origin, CSRF and `WEB_TRADING_ENABLED=true`. The browser sends only `quoteId` and `clientRequestId`. The backend revalidates freshness, current marketability, metadata, venue limits, capacity and fees before atomically persisting the intent and Demo quota occupancy. New submissions return `202`; duplicate logical requests return the existing trade and never submit another venue order.

Transport uncertainty produces `Unknown`; it does not produce a failure result or replacement order. Explicit venue rejection produces `Rejected`.

## Shared trade history

### `GET /api/trades?limit=25`

Returns newest-first shared Demo history, bounded from 1 to 100 rows. Internal login identity and session IDs are excluded.

### `GET /api/trades/{intentId}`

Returns normalized lifecycle, IDs, quantities, execution price, fees, net received state and synchronization quality for one intent.

### `POST /api/trades/{intentId}/recheck`

Requires session, exact Origin and CSRF. The operation queries through the configured reconciliation service and cannot submit a replacement. Concurrent rechecks coalesce; repeated checks are rate-limited. `Unknown` and its reservations remain until authoritative evidence resolves the trade.

## Browser WebSocket

### `GET /api/ws`

Requires a valid session cookie, exact Origin/Host and the trusted proxy boundary during upgrade. Tokens are not accepted through the URL. Phase A sends `session.ready`; later phases add versioned account, valuation, trade and health snapshots/events. Logout and session expiry close the connection.

## Security boundary

The direct TCP peer must match one of the single-host entries in `WEB_TRUSTED_PROXY_CIDRS`. Nginx must overwrite `X-Forwarded-For` and `X-Real-IP` with the same single client IP and set `X-Forwarded-Proto: https`. VenueWire rejects untrusted peers, forwarding chains, mismatched Host, cross-origin writes and malformed content types before the application handler runs.
