# Bybit CEX Connector Lab

A Go connectivity lab for Bybit **Testnet only**. It demonstrates authenticated REST V5 order management, public and private WebSocket V5 streams, durable order state and reconciliation, plus a directly implemented Bybit-oriented FIX 4.4 codec/session/order stack with a local mock server.

This is not a strategy or autonomous trading system. Every order begins with an explicit CLI or test command. Bybit FIX currently supports Spot only; REST and WebSocket demonstrations use USDT Linear products.

## Safety and configuration

Copy `../../.env.example` into your own untracked environment file and create **fresh Testnet credentials**. Never reuse credentials pasted into chat. Export that file before running commands (`set -a; source .env; set +a`); the program deliberately reads the process environment and does not parse secret files itself. It rejects `BYBIT_ENV` values other than `testnet` and endpoints other than the exact approved Testnet endpoints. There is no mainnet switch.

```text
BYBIT_ENV=testnet
BYBIT_API_KEY=
BYBIT_API_SECRET=
BYBIT_FIX_API_KEY=
BYBIT_FIX_PRIVATE_KEY_PATH=
```

REST HMAC credentials and FIX self-generated RSA credentials are different. The FIX key must be 2048 or 4096-bit RSA. Keep the host clock synchronized with NTP: authenticated requests use UTC timestamps, and `venuewire time` reports material skew.

## Build and test

```bash
make check
make build
```

Routine CI is network- and credential-independent. Live tests are opt-in:

```bash
RUN_BYBIT_WS_INTEGRATION=1 go test ./internal/ws -run TestPublicTestnetFiveMinutes -count=1 -v
RUN_BYBIT_INTEGRATION=1 go test ./internal/rest -run TestTestnetOrderLifecycle -count=1 -v
```

The REST integration test fetches current instrument metadata and ticker data, then derives a tick-aligned price and quantity satisfying current minimums. It does not hard-code a quantity that may be invalid tomorrow.

## CLI examples

```bash
go run ./cmd/venuewire time
go run ./cmd/venuewire instrument --category linear --symbol BTCUSDT
go run ./cmd/venuewire account info
go run ./cmd/venuewire account balances
go run ./cmd/venuewire account balances --coin BTC,ETH,USDT
go run ./cmd/venuewire market trades --symbol BTCUSDT
go run ./cmd/venuewire market orderbook --symbol BTCUSDT --depth 50

go run ./cmd/venuewire order place --category linear --symbol BTCUSDT --side Buy --type Limit --qty <derived-qty> --price <safe-price>
go run ./cmd/venuewire order status --category linear --symbol BTCUSDT --order-link-id <id>
go run ./cmd/venuewire order cancel --category linear --symbol BTCUSDT --order-link-id <id>
go run ./cmd/venuewire executions --category linear --symbol BTCUSDT
go run ./cmd/venuewire positions --category linear --symbol BTCUSDT
go run ./cmd/venuewire private-stream
go run ./cmd/venuewire reconcile --category linear --symbol BTCUSDT

go run ./cmd/venuewire fix mock-demo
go run ./cmd/venuewire fix mock-server --listen 127.0.0.1:9001 --scenario accepted
go run ./cmd/venuewire fix connect-testnet
```

Local state uses an atomic, permission-restricted JSON snapshot at `../../state/orders.json` by default. Set `BYBIT_STATE_FILE` to use another path.

## Why REST ACK is not final state

Bybit's successful create/cancel REST response acknowledges acceptance of the request, not the terminal order outcome. The client persists `orderId` and client-generated `orderLinkId` but leaves a create ACK as `PendingSubmit/REST_ACK`. Private WebSocket order and execution events are the primary live truth. REST open/recent orders and executions repair state at startup, after private reconnect, and via `reconcile`.

`orderLinkId` is unique, at most 36 characters, and remains stable through logs, local state, REST, WebSocket, and FIX `ClOrdID(11)`. Once available, Bybit's canonical `orderId` is retained too. A timed-out submission is not blindly retried: its outcome is marked uncertain and must be queried by `orderLinkId` or reconciled first.

Execution IDs make fill application idempotent. REST execution output includes the exchange-reported fee amount, fee rate, and fee currency so gross fills can be reconciled with net wallet balances. Cumulative quantities remain decimal strings, avoiding binary floating-point corruption. The centralized state machine rejects invalid transitions visibly; a confirmed full fill wins over a racing cancellation.

## REST vs WebSocket vs FIX

| Protocol | Strength | Limitation / role here |
|---|---|---|
| REST V5 | Simple request/response, queries, recovery | ACK is asynchronous; order POST is unsafe to retry blindly |
| WebSocket V5 | Primary low-latency market and private state stream | Connections drop; clients need bounded queues, resubscription, and reconciliation |
| FIX 4.4 | Stateful institutional order entry with sequence/session mechanics | Bybit access is whitelist-gated and Spot-only; missed messages are not replayed |

Public WebSocket events may be dropped with an explicit counter if their bounded queue is saturated. Private order/execution events are never silently dropped: saturation fails the connection, reconnects, and reconciles.

## FIX sequence and recovery semantics

The codec calculates byte-accurate `BodyLength(9)` and `CheckSum(10)` and the framer handles fragmented and concatenated reads. Every new Bybit FIX session starts inbound and outbound `MsgSeqNum(34)` at 1 with `ResetSeqNumFlag(141)=Y`. Heartbeats are emitted after outbound idle; `TestRequest(1)` receives `Heartbeat(0)` with the same `TestReqID(112)`.

Bybit does not support standard FIX `ResendRequest(2)` / `SequenceReset(4)` gap-fill recovery and does not retransmit updates missed during disconnect. Recovery creates a fresh session at sequence 1 and invokes REST reconciliation immediately after its Logon ACK. Live FIX may return distinct `access denied` Logout unless the account/IP is whitelisted; the complete required flow remains testable against the local mock.

See [docs/architecture.md](docs/bybit/architecture.md), [docs/protocol-notes.md](docs/bybit/protocol-notes.md), and [docs/demo.md](docs/bybit/demo.md).
