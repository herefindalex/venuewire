# 02 — Architecture Specification

## 1. High-Level Architecture

```text
                         +------------------+
                         |    venuewire CLI   |
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
