# 03 — Bybit Protocol Specification

This file captures the protocol assumptions used by the implementation. If current official Bybit documentation differs, update this file and record the change.

## 1. REST V5

### Testnet Base URL

```text
https://api-testnet.bybit.com
```

### Authentication Headers

Private REST requests use:

```text
X-BAPI-API-KEY
X-BAPI-TIMESTAMP
X-BAPI-SIGN
X-BAPI-RECV-WINDOW
```

Default receive window is currently documented as 5000 ms.

Timestamp validity rule:

```text
server_time - recv_window <= timestamp < server_time + 1000
```

### HMAC Signing

For system-generated HMAC API keys:

GET plaintext:

```text
timestamp + api_key + recv_window + queryString
```

POST plaintext:

```text
timestamp + api_key + recv_window + jsonBodyString
```

Signature:

```text
lowercase_hex(HMAC_SHA256(secret, plaintext))
```

Important: signature generation must use the exact query/body bytes that are sent.

### Core REST Operations

Use current V5 endpoints. At minimum:

```text
GET  /v5/market/time
GET  /v5/market/instruments-info

POST /v5/order/create
POST /v5/order/cancel
POST /v5/order/amend

GET  /v5/order/realtime
GET  /v5/execution/list
GET  /v5/position/list
```

Use `category=linear` for the REST/WebSocket perpetual demo.

### Order Acknowledgement Semantics

A successful place-order REST response is an **acceptance acknowledgement**, not proof of final order state.

The implementation must use private WebSocket order updates and/or REST reconciliation to establish the authoritative lifecycle.

### `orderLinkId`

Use a unique client-defined order identifier.

Current Bybit documentation limits it to 36 characters for futures/perpetual/spot order usage.

### Rate Limit Awareness

Parse and expose current response headers:

```text
X-Bapi-Limit
X-Bapi-Limit-Status
X-Bapi-Limit-Reset-Timestamp
```

Handle exchange rate-limit error code such as `10006` without hot-loop retries.

Do not design to operate at the documented ceiling.

---

## 2. WebSocket V5

### Testnet Endpoints

Public Linear:

```text
wss://stream-testnet.bybit.com/v5/public/linear
```

Private:

```text
wss://stream-testnet.bybit.com/v5/private
```

WebSocket Order Entry exists at:

```text
wss://stream-testnet.bybit.com/v5/trade
```

WebSocket order entry is **optional** for this project because authenticated order entry is mandatory through REST. It may be added as an extension after the mandatory scope is complete.

### Public Topics

Trades:

```text
publicTrade.BTCUSDT
publicTrade.ETHUSDT
```

Order book:

```text
orderbook.1.BTCUSDT
orderbook.50.BTCUSDT
```

The exact depth can be configurable.

### Private Topics

Use either all-in-one or categorized topics consistently.

Recommended categorized topics:

```text
order.linear
execution.linear
position.linear
```

Do not subscribe to both all-in-one and categorized variants for the same stream if Bybit forbids mixing them.

### Execution Semantics

One WebSocket message may contain multiple executions for an order.

The event handler must iterate every execution entry and deduplicate by execution identifier where available.

### Order Race Conditions

A cancel can race with a fill. Tests must allow the order stream to produce seemingly conflicting cancel/fill-related events and resolve them by exchange state rather than assuming request order equals event order.

### Reconnect

Private WebSocket reconnect flow:

```text
disconnect
  -> backoff
  -> reconnect
  -> authenticate
  -> resubscribe private topics
  -> trigger REST reconciliation
  -> resume live processing
```

---

## 3. FIX 4.4

### Current Scope Limitation

Bybit FIX API currently supports **Spot only**.

Do not use FIX for the Linear/Perpetual demo.

### Testnet Endpoint

```text
Host: fix-oe-testnet.bybit.com
Port: 9000
TLS: required
```

### Access

Bybit FIX requires whitelist onboarding.

A non-whitelisted account can be rejected during Logon with a Logout carrying an `access denied` text.

Therefore:

- live FIX Testnet connection is optional;
- a complete local FIX mock integration is mandatory.

### API Key Type

Current Bybit FIX requires a **self-generated RSA API key**, not the ordinary HMAC secret used by REST.

RSA key sizes accepted by current docs: 2048 or 4096 bits.

### Authentication

Construct expiry:

```text
expires = current_unix_ms + 5000
```

Plaintext:

```text
"GET/realtime" + expires
```

Signature:

```text
Base64(RSA_SHA256_sign(privateKey, plaintext))
```

Logon fields include:

```text
35=A
98=0
108=10
553=<API KEY>
95=<RawData length>
96=<Base64 signature>
141=Y
30023=<expires>
```

### Standard Header

Required concepts:

```text
8   BeginString          FIX.4.4
9   BodyLength
35  MsgType
49  SenderCompID
56  TargetCompID         BYBIT_FIX_SERVER
34  MsgSeqNum
52  SendingTime
10  CheckSum
```

### BodyLength

Byte count from the first byte of tag `35=` through the delimiter immediately before tag `10=`.

Implementation must calculate from encoded bytes, not rune/character count assumptions.

### CheckSum

Sum all bytes before `10=` modulo 256.

Encode as zero-padded three-digit decimal:

```text
000 ... 255
```

### Sequence Behavior

Current Bybit behavior:

- `MsgSeqNum` is monotonic within a session;
- new connection/session restarts at 1;
- `ResetSeqNumFlag(141)=Y` is recommended;
- standard FIX gap-fill `ResendRequest(2)` / `SequenceReset(4)` recovery is not supported;
- missed messages during disconnect are not retransmitted on reconnect.

Therefore the client's recovery design is:

```text
disconnect
  -> create new session
  -> seq = 1
  -> logon
  -> reconcile exchange state using REST
```

### Heartbeat

Server heartbeat interval is currently fixed at 10 seconds.

Heartbeat should be treated as idle keepalive rather than blindly emitted on a fixed timer regardless of traffic.

If `TestRequest(35=1)` is received, reply with `Heartbeat(35=0)` echoing the request identifier.

### Mandatory FIX Message Types

Session:

```text
0  Heartbeat
1  TestRequest
5  Logout
A  Logon
```

Application:

```text
D  NewOrderSingle
8  ExecutionReport
```

Also implement Bybit-documented cancel and amend request/ack message flows using the exact current tags/message types from the official API pages.

### Integer Width

Bybit extension timestamps/rate-limit fields may exceed signed 32-bit range. Use `int64` where appropriate.

---

## 4. Official Documentation References

REST integration guide:
https://bybit-exchange.github.io/docs/v5/guide

REST create order:
https://bybit-exchange.github.io/docs/v5/order/create-order

REST rate limits:
https://bybit-exchange.github.io/docs/v5/rate-limit

WebSocket connection:
https://bybit-exchange.github.io/docs/v5/ws/connect

Public trades:
https://bybit-exchange.github.io/docs/v5/websocket/public/trade

Public order book:
https://bybit-exchange.github.io/docs/v5/websocket/public/orderbook

Private order:
https://bybit-exchange.github.io/docs/v5/websocket/private/order

Private execution:
https://bybit-exchange.github.io/docs/v5/websocket/private/execution

Private position:
https://bybit-exchange.github.io/docs/v5/websocket/private/position

FIX integration guide:
https://bybit-exchange.github.io/docs/fix-api/guide
