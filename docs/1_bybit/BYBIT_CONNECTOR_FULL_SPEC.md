# Bybit CEX Connector Lab — Consolidated Specification


---

## FILE: README_CODEX.md

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


---

## FILE: 01_PRODUCT_SPEC.md

# 01 — Product / Functional Specification

## 1. Project Name

`bybit-connector-lab`

## 2. Objective

Implement a compact trading-technology connectivity stack that can be explained and defended in a senior trading systems interview.

The project should prove practical understanding of:

- exchange authentication;
- real-time market data;
- authenticated order submission;
- asynchronous order state;
- fills/executions;
- reconnect behavior;
- idempotency/correlation;
- rate limits;
- state reconciliation;
- FIX session mechanics.

## 3. Environment

### Mandatory
- Bybit **Testnet**
- Go
- CLI application
- Unit tests
- Integration tests
- Local FIX mock server

### Instruments
- REST/WebSocket: **USDT Linear Perpetual**, default `BTCUSDT` and/or `ETHUSDT`
- FIX: **Spot only**, because current Bybit FIX 4.4 support is Spot-only

## 4. In Scope

### 4.1 REST V5

Implement:

- public server-time request;
- instrument metadata lookup;
- authenticated request signing;
- place order;
- cancel order;
- query active/recent order state;
- query executions;
- query positions for Linear contracts;
- rate-limit header parsing;
- structured error handling;
- `orderLinkId` generation and propagation.

### 4.2 Public WebSocket

Implement subscriptions for:

- `publicTrade.{symbol}`
- `orderbook.{depth}.{symbol}`

Requirements:

- connect;
- subscribe/unsubscribe;
- ping/pong or documented keepalive behavior;
- reconnect with capped exponential backoff + jitter;
- resubscribe after reconnect;
- stale-connection detection;
- bounded internal queues;
- observable message/processing lag.

### 4.3 Private WebSocket

Implement authenticated private stream and subscriptions for:

- `order` or `order.linear`
- `execution` or `execution.linear`
- `position` or `position.linear`

Requirements:

- authentication;
- order/execution correlation by `orderId` and `orderLinkId`;
- state updates;
- duplicate-event tolerance;
- reconnect/resubscribe;
- trigger reconciliation after connection loss.

### 4.4 Order Lifecycle

Minimum supported lifecycle:

```text
PendingSubmit
  -> New
  -> PartiallyFilled
  -> Filled

New / PartiallyFilled
  -> PendingCancel
  -> Cancelled

Any submission/state transition
  -> Rejected
```

The exact exchange status names may differ; map them into internal domain states without losing raw exchange status.

### 4.5 Reconciliation

Implement a reconciliation operation invoked:

- at startup;
- after private WebSocket reconnect;
- manually through CLI.

It must compare:

- local known active orders;
- REST current open/recent orders;
- REST execution history;

and repair local state where possible.

Reconciliation must be idempotent.

### 4.6 FIX 4.4

Build a minimal Bybit-compatible FIX 4.4 client, not a general-purpose FIX engine.

Mandatory:

- FIX encoder;
- FIX parser;
- `BeginString(8)`;
- `BodyLength(9)`;
- `CheckSum(10)`;
- standard header fields;
- TLS transport abstraction;
- `Logon (35=A)`;
- `Logout (35=5)`;
- `Heartbeat (35=0)`;
- `TestRequest (35=1)`;
- session sequence tracking via `MsgSeqNum(34)`;
- `NewOrderSingle (35=D)`;
- cancel request;
- amend request if supported by current Bybit FIX spec;
- `ExecutionReport (35=8)`;
- async ACK/state handling;
- reconnect with a new session;
- post-reconnect state reconciliation design.

Bybit-specific behavior to model:

- FIX is Spot-only.
- TLS is mandatory.
- authentication uses a self-generated RSA key and RSA-SHA256.
- access is whitelist-gated.
- sequence starts at 1 for a new session.
- standard FIX gap-fill/session resume is not available on Bybit.
- missed messages across a disconnect must be handled through reconciliation rather than resend recovery.

### 4.7 Local FIX Mock Server

Because Bybit FIX access may not be whitelisted, create a local test server that:

- accepts TLS or an injectable test transport;
- receives Logon;
- validates basic required fields;
- sends Logon ACK;
- handles heartbeat/test request;
- accepts NewOrderSingle;
- replies with ExecutionReport ACK/New;
- can simulate partial fill;
- can simulate full fill;
- can simulate reject;
- accepts cancel;
- can intentionally disconnect;
- can intentionally send malformed checksum;
- can intentionally send unexpected sequence number;
- supports clean Logout.

The mock exists to validate the client state machine; it does not need to implement all FIX 4.4.

## 5. Out of Scope

Do **not** build:

- trading strategies;
- signal generation;
- portfolio optimization;
- UI/dashboard;
- mainnet trading;
- real-money trading;
- market making;
- smart order routing across multiple exchanges;
- full generic FIX 4.4 standard coverage;
- production HFT latency optimization.

## 6. CLI Requirements

Recommended executable:

`../../cmd/bybitctl`

Recommended commands:

```text
bybitctl time
bybitctl instrument --category linear --symbol BTCUSDT

bybitctl market trades --symbol BTCUSDT
bybitctl market orderbook --symbol BTCUSDT --depth 50

bybitctl order place --category linear --symbol BTCUSDT --side Buy --type Limit ...
bybitctl order cancel --category linear --symbol BTCUSDT --order-link-id ...
bybitctl order status --category linear --symbol BTCUSDT --order-link-id ...
bybitctl executions --category linear --symbol BTCUSDT
bybitctl positions --category linear --symbol BTCUSDT

bybitctl private-stream
bybitctl reconcile

bybitctl fix mock-server
bybitctl fix mock-demo
bybitctl fix connect-testnet
```

Exact flags may be adjusted if documented.

## 7. Observability

Structured logs must include where available:

- protocol: REST / WS / FIX;
- symbol;
- `orderId`;
- `orderLinkId`;
- request/correlation ID;
- WS topic;
- reconnect attempt;
- exchange timestamp;
- receive timestamp;
- processing latency;
- rate-limit remaining/reset;
- FIX `MsgSeqNum`;
- FIX `MsgType`.

Never log:

- API secret;
- full auth signature;
- RSA private key;
- Authorization/authentication payload containing sensitive values.

## 8. Deliverables

Mandatory repository deliverables:

```text
README.md
go.mod
.env.example
.gitignore

cmd/bybitctl/

internal/domain/
internal/rest/
internal/ws/
internal/orderstate/
internal/reconcile/
internal/fix/
internal/fixmock/
internal/observability/

testdata/fix/

docs/
  architecture.md
  protocol-notes.md
  demo.md

IMPLEMENTATION_STATUS.md
```

## 9. Resume-Quality Outcome

The implementation should make the following statement factually defensible:

> Built a Go-based CEX connectivity project against Bybit Testnet using REST and WebSocket for real-time market data, authenticated order management, private execution updates, reconnect handling, and state reconciliation; also implemented a Bybit-oriented FIX 4.4 client covering session lifecycle, sequence management, order messages, execution reports, heartbeat, and recovery semantics.

Do not claim production FIX connectivity unless it was actually achieved.


---

## FILE: 02_ARCHITECTURE.md

# 02 — Architecture Specification

## 1. High-Level Architecture

```text
                         +------------------+
                         |    bybitctl CLI   |
                         +--------+---------+
                                  |
                     explicit user commands only
                                  |
          +-----------------------+------------------------+
          |                       |                        |
          v                       v                        v
+------------------+    +------------------+     +------------------+
|    REST Client   |    | WebSocket Client |     | FIX 4.4 Client   |
|     Bybit V5     |    |     Bybit V5     |     |  Bybit-oriented  |
+--------+---------+    +--------+---------+     +--------+---------+
         |                       |                        |
         |              +--------+---------+              |
         |              |                  |              |
         |              v                  v              |
         |        Public streams     Private streams      |
         |                                                |
         +-------------------+----------------------------+
                             |
                             v
                    +-------------------+
                    | Domain / Order    |
                    | State Machine     |
                    +---------+---------+
                              |
                              v
                    +-------------------+
                    | Reconciliation    |
                    +---------+---------+
                              |
                              v
                    +-------------------+
                    | StateStore        |
                    | file-backed dev   |
                    +-------------------+
```

## 2. Design Principles

1. Keep exchange protocol details out of domain objects.
2. Preserve raw exchange identifiers/status in addition to normalized state.
3. Never assume REST ACK means final order state.
4. Private WebSocket is the primary live state stream.
5. REST is the source for explicit state recovery/reconciliation.
6. Every queue must be bounded.
7. Every goroutine must terminate via `context.Context`.
8. Network reconnect must use backoff + jitter and must not busy-loop.
9. Duplicate events must not corrupt local state.
10. Any state transition must be explainable in logs/tests.

## 3. Domain Types

Recommended conceptual types:

```go
type Side string
type OrderType string
type OrderStatus string

type Order struct {
    Exchange        string
    Category        string
    Symbol          string
    OrderID         string
    OrderLinkID     string
    Side            Side
    Type            OrderType
    Price           decimal-like value
    Qty             decimal-like value
    CumFilledQty    decimal-like value
    AvgFillPrice    decimal-like value
    Status          OrderStatus
    RawStatus       string
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type Execution struct {
    Exchange        string
    Symbol          string
    ExecutionID     string
    OrderID         string
    OrderLinkID     string
    Side            Side
    Price           decimal-like value
    Qty             decimal-like value
    ExchangeTime    time.Time
    ReceivedAt      time.Time
}
```

Do not use floating-point arithmetic for prices/quantities if avoidable. Use decimal strings or a proven decimal representation.

## 4. Connector Interfaces

Do not force REST/WS/FIX into one fake symmetric interface.

Recommended boundaries:

```go
type OrderCommandService interface {
    PlaceOrder(ctx context.Context, req PlaceOrderRequest) (OrderAck, error)
    CancelOrder(ctx context.Context, req CancelOrderRequest) (OrderAck, error)
    AmendOrder(ctx context.Context, req AmendOrderRequest) (OrderAck, error)
}

type OrderQueryService interface {
    GetOpenOrders(ctx context.Context, q OrderQuery) ([]Order, error)
    GetExecutions(ctx context.Context, q ExecutionQuery) ([]Execution, error)
}

type EventSource interface {
    Run(ctx context.Context, sink EventSink) error
}

type Reconciler interface {
    Reconcile(ctx context.Context) (ReconcileReport, error)
}
```

The FIX client may implement command + event behavior in a single session, but internal interfaces should remain explicit.

## 5. Concurrency Model

### REST
- ordinary request/response calls;
- bounded retry policy only for retry-safe failures;
- do not automatically retry an order submission unless idempotency/correlation behavior is explicitly safe.

### WebSocket
Suggested goroutines per connection:

```text
reader
  -> decode
  -> bounded event queue
  -> event processor

writer
  <- subscribe/ping/control commands

supervisor
  -> reconnect/backoff
  -> resubscribe
  -> trigger reconciliation for private stream
```

No unbounded channels.

If the consumer cannot keep up:

- expose queue depth;
- count drops only if the message class is explicitly allowed to drop;
- **order/execution/private state messages must not be silently dropped**;
- if private stream cannot be processed safely, fail the connection and reconcile.

### FIX
Suggested components:

```text
TLS Transport
   |
FIX Framer / Parser
   |
Session Engine
   |-- inbound sequence
   |-- outbound sequence
   |-- heartbeat timer
   |-- TestRequest response
   |
Order Router
   |
Order State / Event Sink
```

## 6. Order State Rules

State transitions must be centralized.

Example normalized transitions:

```text
Unknown -> PendingSubmit
PendingSubmit -> New
PendingSubmit -> Rejected

New -> PartiallyFilled
New -> Filled
New -> PendingCancel

PartiallyFilled -> PartiallyFilled
PartiallyFilled -> Filled
PartiallyFilled -> PendingCancel

PendingCancel -> Cancelled
PendingCancel -> Filled
PendingCancel -> RejectedCancel
```

Important race:

A cancel request can race with a fill. A later Filled event must be allowed to win over an attempted cancel where exchange semantics indicate the order had already filled.

## 7. Correlation / Idempotency

Use `orderLinkId` for client-generated correlation.

Requirements:

- unique per new order;
- <= current Bybit documented maximum length;
- stable across logs/state;
- never reuse for a different logical order.

Suggested shape:

```text
bcx-<utccompact>-<random>
```

Do not assume `orderLinkId` alone is the exchange's canonical ID; persist Bybit `orderId` once known.

## 8. Reconciliation Algorithm

Pseudo-flow:

```text
1. Load local active/non-terminal orders.
2. Fetch REST open/recent orders.
3. Fetch recent executions for relevant symbols/time window.
4. Match by orderId, then orderLinkId.
5. Rebuild cumulative fills from execution data if needed.
6. Move stale local orders to the best known exchange state.
7. Record discrepancies.
8. Persist updated snapshot.
9. Emit a reconciliation report.
```

Requirements:

- idempotent;
- no new orders created;
- no order cancellation unless an explicit future feature is added;
- discrepancies must be logged;
- ambiguous state must remain visible, not silently guessed.

## 9. Local State Store

A simple development implementation is sufficient.

Required interface:

```go
type StateStore interface {
    Load(ctx context.Context) (*Snapshot, error)
    Save(ctx context.Context, snapshot *Snapshot) error
}
```

Default can be atomic JSON file replacement:

1. write temporary file;
2. fsync if practical;
3. rename atomically.

Do not add a database unless necessary.

## 10. Failure Model

Explicitly support/test:

- REST timeout;
- HTTP 429 / exchange rate limit;
- exchange business error;
- malformed JSON;
- WS disconnect;
- WS authentication failure;
- WS duplicate update;
- WS delayed update;
- order filled while cancel is in flight;
- process restart;
- FIX malformed message;
- FIX checksum failure;
- FIX unexpected sequence;
- FIX TestRequest;
- FIX server disconnect;
- FIX non-whitelisted `access denied`.

## 11. Metrics

A lightweight metrics abstraction is enough. At minimum expose counters/gauges in logs or Prometheus-style endpoint if convenient:

- REST request count/error/latency;
- REST rate-limit remaining;
- WS reconnect count;
- WS messages by topic;
- WS event lag ms;
- private queue depth;
- reconciliation runs/discrepancies;
- FIX inbound/outbound messages by MsgType;
- FIX reconnects;
- FIX sequence errors;
- FIX heartbeat/test-request counts.


---

## FILE: 03_PROTOCOL_SPEC.md

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


---

## FILE: 04_IMPLEMENTATION_PLAN.md

# 04 — Implementation Plan

## Phase 0 — Repository / Safety Scaffold

### Tasks

- initialize Go module;
- create directory structure;
- `../../.gitignore`;
- `../../.env.example`;
- config loader;
- testnet-only environment guard;
- structured logger with secret redaction;
- CI command or Makefile targets;
- `IMPLEMENTATION_STATUS.md`.

### Acceptance

- `go test ./...` passes;
- application refuses to start authenticated trading commands without required env vars;
- no secret is present in repository;
- config defaults to testnet;
- mainnet hosts are rejected by default.

---

## Phase 1 — REST V5

### Tasks

1. Basic HTTP client.
2. Server time.
3. Instrument metadata.
4. HMAC signer.
5. Authenticated request builder.
6. Place order.
7. Cancel order.
8. Amend order.
9. Query current/recent orders.
10. Query executions.
11. Query Linear positions.
12. Rate-limit header capture.
13. Typed business errors.

### Mandatory Integration Demo

Use Testnet only.

Example flow:

```text
fetch instrument metadata
  -> derive valid tick/qty
  -> place a small limit order away from market
  -> receive REST ACK
  -> query order
  -> cancel
  -> query terminal state
```

Do not hard-code a quantity that may violate current instrument minimums.

### Acceptance

- signing unit tests pass;
- one authenticated Testnet request succeeds;
- a limit order can be submitted and cancelled;
- `orderId` + `orderLinkId` are persisted/logged;
- rate-limit headers appear in debug/metrics output.

---

## Phase 2 — Public WebSocket

### Tasks

- public Linear connection;
- subscribe to trade topic;
- subscribe to order book;
- typed decoder;
- receive/exchange timestamps;
- latency measurement;
- reconnect;
- resubscribe;
- bounded queue;
- cancellation through context.

### Acceptance

- stream runs for >= 5 minutes without goroutine leak;
- forced disconnect triggers reconnect;
- subscriptions return after reconnect;
- events show symbol, exchange timestamp, receive timestamp, basic lag.

---

## Phase 3 — Private WebSocket + Order State

### Tasks

- authentication;
- order subscription;
- execution subscription;
- position subscription;
- normalized domain events;
- local order state machine;
- deduplication;
- file-backed state snapshot.

### End-to-End Demo

```text
start private stream
  -> submit REST order
  -> REST ACK
  -> private WS New
  -> cancel via REST
  -> private WS Cancelled
```

Optional fill demo:

```text
place safe small Testnet order intended to execute
  -> observe one or more execution events
  -> state reaches PartiallyFilled/Filled
```

### Acceptance

- WS event updates the same local order created through REST;
- duplicate event replay does not double-count fills;
- cancel/fill race test exists;
- state persists and reloads.

---

## Phase 4 — Reconciliation

### Tasks

- startup reconciliation;
- manual `reconcile` command;
- reconcile after private WS reconnect;
- discrepancy report;
- idempotent state repair.

### Acceptance

Test:

1. place order;
2. stop local process;
3. change exchange state while process is offline (e.g. cancel through another session/web);
4. restart;
5. reconciliation updates local state correctly.

Also test with fixture data so CI does not depend on Testnet.

---

## Phase 5 — FIX Codec

### Do Not Use a Full FIX Engine for Core Learning Path

Implement the minimal required codec/session behavior directly so the code demonstrates understanding of:

- SOH-delimited fields;
- BodyLength;
- CheckSum;
- MsgType;
- sequence;
- session messages.

A small helper dependency is acceptable, but do not hide the core FIX mechanics behind QuickFIX or another complete engine unless a second adapter is added only for comparison.

### Tasks

- Tag/Field representation;
- encoder;
- parser;
- BodyLength;
- CheckSum;
- strict/lenient parse mode as appropriate;
- fixture tests.

### Acceptance

- known valid fixtures round-trip;
- incorrect checksum rejected;
- incorrect body length detected;
- parser handles fragmented transport reads through a framing layer;
- multiple concatenated FIX messages can be extracted correctly.

---

## Phase 6 — FIX Session

### Tasks

- transport interface;
- TLS transport;
- outbound/inbound sequence counters;
- Logon builder/parser;
- RSA signer abstraction;
- Heartbeat;
- TestRequest response;
- Logout;
- idle timer;
- reconnect policy.

### Acceptance

Against mock server:

```text
TLS/connect
  -> Logon
  <- Logon ACK
  -> idle
  <- TestRequest
  -> Heartbeat(TestReqID)
  -> Logout
  <- Logout
```

Sequence begins at 1 for new sessions.

---

## Phase 7 — FIX Orders / Execution Reports

### Tasks

- NewOrderSingle;
- cancel;
- amend;
- ExecutionReport mapping;
- request correlation;
- order state integration;
- rejection handling;
- partial fill/full fill.

### Acceptance

Mock scenarios:

- accepted order;
- rejected order;
- partial then full fill;
- cancel accepted;
- cancel race with fill;
- malformed sequence;
- disconnect after New but before fill.

---

## Phase 8 — FIX Recovery / Bybit Behavior

### Tasks

- model no-resend recovery;
- reconnect as new session;
- REST-backed reconciliation hook;
- log non-whitelist `access denied` distinctly.

### Acceptance

Mock:

```text
Session 1
  -> order accepted
  X disconnect before later update

Session 2
  -> seq starts at 1
  -> logon
  -> reconciliation fetcher reports final exchange state
  -> local order repaired
```

No use of unsupported ResendRequest gap-fill as the primary recovery mechanism.

---

## Phase 9 — Documentation / Interview Demo

### README must explain

- why REST ACK is not final state;
- why private WS matters;
- how `orderLinkId` is used;
- reconnect strategy;
- reconciliation;
- REST vs WebSocket vs FIX trade-offs;
- FIX sequence semantics;
- Bybit-specific lack of gap-fill recovery;
- why FIX live connectivity may be unavailable without whitelist.

### Demo Recording / Transcript

Create `demo.md` with commands and expected output for:

1. REST + private WS order lifecycle.
2. WS reconnect.
3. restart + reconciliation.
4. local FIX mock session.
5. FIX order + ExecutionReport.
6. FIX disconnect + recovery.

---

## Phase 10 — Optional Extensions

Only after all mandatory phases:

- WebSocket order entry;
- Prometheus endpoint;
- pprof;
- latency histogram;
- second CEX adapter;
- Docker Compose for local mock/demo;
- benchmark FIX codec;
- property/fuzz tests for parser.


---

## FILE: 05_TEST_PLAN.md

# 05 — Test and Acceptance Plan

## 1. Test Layers

### Unit
No network.

### Fixture / Protocol
Recorded/synthetic REST JSON, WS messages, FIX messages.

### Local Integration
FIX client + local mock server.

### External Integration
Bybit Testnet REST/WS.

CI must not require live Bybit credentials.

---

## 2. REST Unit Tests

Mandatory:

- GET signature construction;
- POST signature construction;
- recv-window header;
- timestamp injection;
- exact-body signing;
- error envelope parsing;
- rate-limit header parsing;
- `orderLinkId` generation uniqueness/length;
- no secret in log output.

Use deterministic timestamp and known secret test vectors generated for tests only.

Never use real API credentials in fixtures.

---

## 3. REST Testnet Tests

Manual or opt-in integration tag:

- server time;
- instrument info;
- authenticated order query;
- place valid limit order;
- cancel order;
- fetch order state;
- fetch execution list;
- fetch position list.

All integration tests must be skipped unless an explicit environment flag is set.

Recommended:

```text
RUN_BYBIT_INTEGRATION=1
```

---

## 4. Public WebSocket Tests

- decode trade event;
- decode orderbook snapshot;
- decode orderbook delta;
- reconnect on EOF;
- reconnect on network error;
- resubscribe after reconnect;
- context cancellation closes goroutines;
- bounded queue does not grow without limit;
- stale connection triggers health failure.

For the order book, if a local book is implemented, test snapshot + delta sequencing separately.

---

## 5. Private WebSocket Tests

Fixtures:

- order New;
- PartiallyFilled;
- Filled;
- Cancelled;
- Rejected;
- duplicate event;
- multiple executions in one message;
- position update.

Acceptance:

- fills are not double-counted;
- order status remains monotonic according to allowed transition rules;
- a late cancel-related event cannot incorrectly overwrite a confirmed Filled state.

---

## 6. State Machine Tests

Table-driven transitions.

At minimum:

```text
PendingSubmit -> New
PendingSubmit -> Rejected

New -> PartiallyFilled
New -> Filled
New -> PendingCancel

PartiallyFilled -> Filled
PartiallyFilled -> PendingCancel

PendingCancel -> Cancelled
PendingCancel -> Filled
```

Invalid transitions should:

- return an explicit error, or
- be preserved as a visible anomaly,

not silently mutate state.

---

## 7. Reconciliation Tests

### Fixture Scenarios

1. Local New / Exchange Cancelled.
2. Local New / Exchange Filled.
3. Local PartiallyFilled / Exchange Filled.
4. Local order absent from open orders but present in recent order history.
5. Missing order update but execution exists.
6. Duplicate execution returned.
7. Unknown exchange order not present locally.
8. Reconciliation repeated twice produces same state.

### Restart Test

- save snapshot;
- instantiate new service;
- load snapshot;
- feed query fixtures;
- reconcile;
- verify repaired state.

---

## 8. FIX Codec Tests

Mandatory known-message fixtures.

### Encoder

- `8=FIX.4.4`;
- `9=BodyLength` correct;
- SOH separators;
- `10=CheckSum` correct;
- field order for required header fields.

### Parser

- valid message;
- fragmented message across multiple reads;
- two messages in one buffer;
- invalid checksum;
- invalid BodyLength;
- missing tag;
- duplicate tag behavior explicitly defined;
- unknown tag preserved or safely ignored according to design.

### Fuzzing

Recommended:

```text
go test -fuzz=FuzzFIXParser
```

Parser must never panic on arbitrary bytes.

---

## 9. FIX Session Tests

Against local mock:

### Logon
- seq 1;
- valid credentials fixture;
- successful logon response;
- access denied response;
- malformed response.

### Heartbeat
- no traffic -> heartbeat behavior;
- server TestRequest -> Heartbeat with matching ID;
- normal application traffic resets idle timer.

### Sequence
- expected inbound seq;
- higher-than-expected seq;
- lower/duplicate seq;
- new session resets to 1.

Because Bybit does not provide standard resend gap-fill recovery, sequence anomaly policy must be explicit and testable.

### Logout
- graceful client logout;
- server logout;
- socket close without logout;
- reconnect backoff.

---

## 10. FIX Order Tests

Mock server scenarios:

### Accepted
`NewOrderSingle -> ExecutionReport(New)`

### Rejected
`NewOrderSingle -> ExecutionReport(Rejected)`

### Partial Fill
`New -> PartiallyFilled -> Filled`

### Cancel
`New -> CancelRequest -> Cancelled`

### Race
Client sends cancel while mock sends final fill.

Expected final state: Filled if exchange execution confirms full fill.

### Disconnect
Connection drops after initial ACK, before final state.

Client must reconnect as a new session and invoke reconciliation.

---

## 11. Security Tests

Automated checks should fail if repository contains patterns resembling:

- real `BYBIT_API_KEY=...`;
- real `BYBIT_API_SECRET=...`;
- PEM private key;
- `../../.env` file tracked by git.

Log tests verify redaction of:

- secret;
- private key;
- signatures;
- auth payload values where sensitive.

---

## 12. Final Mandatory Acceptance Checklist

The project is **not done** until all are true:

- [ ] `go test ./...` passes.
- [ ] no credentials committed.
- [ ] testnet-only guard exists.
- [ ] REST HMAC authentication works on Bybit Testnet.
- [ ] Testnet limit order can be placed.
- [ ] Testnet order can be cancelled.
- [ ] public trade WebSocket works.
- [ ] public orderbook WebSocket works.
- [ ] private order stream works.
- [ ] private execution stream works.
- [ ] reconnect/resubscribe tested.
- [ ] local order state is persisted.
- [ ] reconciliation is implemented and tested.
- [ ] FIX encoder/parser works.
- [ ] FIX BodyLength and CheckSum tests exist.
- [ ] FIX Logon/Logout implemented.
- [ ] FIX heartbeat/TestRequest implemented.
- [ ] FIX sequence handling implemented.
- [ ] FIX NewOrderSingle implemented.
- [ ] FIX ExecutionReport implemented.
- [ ] FIX cancel flow implemented.
- [ ] FIX amend flow implemented if current Bybit docs support it.
- [ ] FIX mock server exists.
- [ ] FIX partial fill/full fill/reject tests exist.
- [ ] FIX disconnect -> new session -> reconciliation is tested.
- [ ] README explains protocol trade-offs and limitations.


---

## FILE: 06_SECURITY_OPERATIONS.md

# 06 — Security and Operations Requirements

## 1. Credential Policy

The project must use environment variables.

Recommended names:

```text
BYBIT_ENV=testnet
BYBIT_API_KEY=
BYBIT_API_SECRET=

BYBIT_FIX_API_KEY=
BYBIT_FIX_PRIVATE_KEY_PATH=
```

The ordinary REST HMAC key and FIX RSA key are different credential models.

### Important

Credentials previously pasted into a chat must be considered exposed.

They must **not** be copied into this repository, test fixtures, docs, shell history examples, source code, or generated files.

Create fresh Testnet credentials before live integration testing.

## 2. `../../.env.example`

Allowed:

```text
BYBIT_ENV=testnet
BYBIT_API_KEY=
BYBIT_API_SECRET=
BYBIT_FIX_API_KEY=
BYBIT_FIX_PRIVATE_KEY_PATH=
```

Not allowed:

```text
BYBIT_API_KEY=actual-key
BYBIT_API_SECRET=actual-secret
```

## 3. Git Ignore

At minimum:

```text
.env
.env.*
!.env.example
*.pem
*.key
secrets/
data/
```

Adjust `data/` if non-sensitive fixtures need tracking; keep live state snapshots untracked.

## 4. Mainnet Guard

This project is Testnet-only.

Authenticated commands must fail fast if:

- base URL resolves to mainnet;
- WebSocket host is mainnet;
- `BYBIT_ENV != testnet`.

Do not add a casual `--mainnet` switch.

If mainnet support is ever added, it must be a separate deliberate change outside this specification.

## 5. Logging

Never log:

- API secret;
- RSA private key;
- full signatures;
- raw private authentication frames if they expose credential material.

Safe identifiers:

- truncated/hashed API key fingerprint;
- order ID;
- orderLinkId;
- FIX SenderCompID;
- FIX message type;
- sequence number;
- rate-limit headers.

## 6. Clock

Authenticated REST/FIX requests depend on time validity.

Requirements:

- use UTC;
- allow server-time diagnostic command;
- warn on material clock skew;
- document that host should be NTP-synchronized.

Do not silently compensate for massive clock drift without surfacing it.

## 7. Retry Policy

### Safe to retry carefully
- public GET;
- read-only authenticated GET;
- connection establishment;
- subscription after reconnect.

### Dangerous
Order placement is not blindly retry-safe.

If an order submission times out after bytes may have reached the exchange:

1. do not immediately submit a second logical order;
2. query by `orderLinkId` / reconcile;
3. only submit again after establishing the original was not accepted.

This behavior should be visible in code and tests.

## 8. Rate Limits

Use response-provided rate-limit information where available.

On limit exhaustion:

- do not busy-loop;
- honor reset/backoff;
- emit structured warning;
- keep critical private stream handling alive.

## 9. Shutdown

On SIGINT/SIGTERM:

1. stop accepting new commands;
2. cancel contexts;
3. flush/persist local state;
4. close WS cleanly when possible;
5. send FIX Logout if session is active;
6. close network transports;
7. exit within a bounded timeout.

## 10. Operational Runbook

`demo.md` should include troubleshooting for:

- REST invalid signature;
- timestamp outside recv window;
- insufficient test balance;
- invalid tick size / quantity;
- WS authentication failure;
- rate limit;
- FIX TLS failure;
- FIX RSA key mismatch;
- FIX `access denied` due to whitelist;
- state discrepancy after reconnect.
