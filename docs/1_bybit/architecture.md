# Implemented architecture

```text
venuewire
  ├─ REST V5 client ─────────────┐
  ├─ public WS (bounded/droppable)│
  ├─ private WS (fail on overflow)├─> domain order state ─> atomic JSON store
  └─ FIX session + order router ──┘             ^
                                                │
                         startup/reconnect/manual REST reconciliation
```

Protocol objects are decoded at connector boundaries and mapped into decimal-string domain objects. REST does request/response work and never blindly retries order placement. Public and private WebSocket clients use capped jittered backoff, resubscribe after reconnect, and terminate through context cancellation. The private reconnect hook runs reconciliation after authentication and subscription.

All order transitions pass through `../../internal/orderstate`. Snapshots are written to a same-directory temporary file, synced, permissioned `0600`, and atomically renamed. Reconciliation matches `orderId` first and `orderLinkId` second, deduplicates executions, imports unknown exchange orders, and leaves ambiguous local-only orders visible.

The FIX stack is deliberately direct rather than wrapping a general engine:

```text
TLS / injected transport
  -> bounded framer
  -> strict codec
  -> session (Logon, seq, idle timers, heartbeat, logout)
  -> Bybit Spot order router (D/F/XAR; 8/XCA/XAA)
  -> shared order state
```

On disconnect, the transport is closed only after in-flight writes finish. A new session resets both directions to sequence 1; after its Logon ACK, the reconciliation hook repairs missed exchange state.

