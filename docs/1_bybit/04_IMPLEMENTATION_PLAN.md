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
