# Demo transcript

Use fresh Testnet credentials and derive order values from current `instrument` output. Never paste credentials into commands or transcripts.

## 1. REST plus private WebSocket lifecycle

Terminal A:

```bash
go run ./cmd/venuewire private-stream
```

Expected: startup reconciliation and categorized order/execution events.

Terminal B:

```bash
go run ./cmd/venuewire instrument --category linear --symbol BTCUSDT
go run ./cmd/venuewire order place --category linear --symbol BTCUSDT --side Buy --type Limit --qty <valid-qty> --price <away-price>
go run ./cmd/venuewire order cancel --category linear --symbol BTCUSDT --order-link-id <reported-id>
```

Expected: REST ACK logs include both IDs and say final state is pending; Terminal A receives `New`, then `Cancelled` (or a racing fill).

## 2. Public WebSocket reconnect

```bash
go run ./cmd/venuewire market orderbook --symbol BTCUSDT --depth 50
```

Interrupt the network briefly and restore it. Expected: capped reconnect, resubscription, and resumed events with exchange/receive timestamps and lag.

## 3. Restart and reconciliation

Place an away-from-market order, stop the private stream, cancel through another Testnet session, then restart:

```bash
go run ./cmd/venuewire private-stream
go run ./cmd/venuewire reconcile --category linear --symbol BTCUSDT
```

Expected: local `New` is repaired to `Cancelled` and the discrepancy is printed.

## 4–6. Local FIX session, order, and recovery

```bash
BYBIT_STATE_FILE=/tmp/bybit-fix-demo.json go run ./cmd/venuewire fix mock-demo
go test ./internal/fixmock -run 'TestMock|TestDisconnect' -v
```

Expected demo: Logon → NewOrderSingle → New → PartiallyFilled → Filled → Logout, with two persisted execution IDs. Tests also show reject, cancel, amend, cancel/fill race, malformed sequence, and disconnect → new sequence-1 session → reconciliation.

## Troubleshooting

- REST timestamp rejection: run `venuewire time`, inspect clock skew, and enable NTP. Do not hide large skew with silent compensation.
- WS auth failure: confirm fresh Testnet HMAC credentials and exact Testnet URL.
- FIX TLS failure: verify port 9000 reachability, SNI hostname, and TLS trust.
- FIX RSA mismatch: REST HMAC secrets are not FIX keys; use a 2048/4096-bit self-generated RSA key matching the FIX API key.
- FIX `access denied`: Bybit FIX is whitelist-gated; use the local mock unless onboarding is complete.
- Reconciliation discrepancy: inspect both IDs, raw status, executions, and report action; ambiguous state is intentionally not guessed.
