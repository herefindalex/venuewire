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
