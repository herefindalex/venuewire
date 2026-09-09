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
