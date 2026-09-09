# Multi-venue demonstration

Run this demo from the repository root. All external connections are Testnet-only. Load `.env` without printing it:

```bash
go build -o ./bin/bybitctl ./cmd/bybitctl
set -a
source .env
set +a
./bin/bybitctl help
```

## 1. Local commands without exchange writes

Both FIX dialects have credential-free local lifecycles:

```bash
./bin/bybitctl --venue bybit fix mock-demo
./bin/bybitctl --venue deribit fix mock-demo
```

Expected: Bybit produces an isolated filled fixture snapshot. Deribit reports `LOCAL_TESTED`, exercises SHA-256 Logon, heartbeat, resend/reset, SecurityList multiplier validation, and D/G/F/8 handling, then ends with `mockFinalOrderStatus=4`. Neither command reads or modifies configured persistent order state.

Migration is explicit and reversible:

```bash
./bin/bybitctl state migrate-v1 --bybit-account-alias bybit-test --dry-run
./bin/bybitctl state migrate-v1 --bybit-account-alias bybit-test
./bin/bybitctl state restore-v1
```

Review the dry-run before apply. Apply creates a mode-`0600` backup; restore validates that backup and atomically restores v1 bytes.

## 2. Read both venues

```bash
./bin/bybitctl --venue bybit account balances --coin BTC,ETH,USDT
./bin/bybitctl --venue deribit doctor
./bin/bybitctl --venue deribit account balances --currency all
./bin/bybitctl --venue deribit positions --currency BTC --kind future
./bin/bybitctl --venue all status
./bin/bybitctl --venue all portfolio
```

Expected: every result keeps venue/account identity and native currency. `doctor.private.cancelOnDisconnect` shows account-scope COD. A failed venue appears as a venue-scoped error; it is never converted into a zero balance for the healthy venue.

## 3. Public/private WebSocket behavior

```bash
./bin/bybitctl --venue deribit public-stream \
  --channels ticker.BTC-PERPETUAL.100ms --duration 5s

./bin/bybitctl --venue deribit private-stream --duration 10s
```

Expected: heartbeat setup and subscription complete; `metrics.CODQueried=true`, `metrics.CODScope=connection`, and the current connection setting is displayed. `user.changes` events decode every order, trade, and position in the message. Orders and canonical trade IDs are persisted through the reconciliation reducer.

Connection-scope COD is an explicit, gated write:

```bash
export RUN_MULTI_VENUE_E2E=1
export RUN_DERIBIT_TRADING_TESTS=1
./bin/bybitctl --venue deribit private-stream \
  --duration 10s --enable-connection-cod --confirm
```

Expected: `metrics.CODEnabled=true` from a read on that same connection. Closing the connection does not change account-scope COD and does not remove the need to query orders/trades after disconnect.

## 4. Planned HTTP/WS/FIX lifecycle

Read current metadata and ticker first:

```bash
./bin/bybitctl --venue deribit instrument --name BTC-PERPETUAL
./bin/bybitctl --venue deribit ticker --instrument BTC-PERPETUAL
```

Choose a valid passive price inside `DERIBIT_MAX_PRICE_DEVIATION_PCT`. For each desired transport (`http`, `ws`, or `fix`):

```bash
./bin/bybitctl --venue deribit order plan \
  --instrument BTC-PERPETUAL --side buy --amount 10 \
  --type limit --price <passive-price> --transport <transport> --post-only

./bin/bybitctl --venue deribit order execute --plan-id <plan-id> --confirm

./bin/bybitctl --venue deribit order amend \
  --order-id <native-id> --amount 10 --price <new-valid-price> \
  --transport <transport> --confirm

./bin/bybitctl --venue deribit order cancel \
  --order-id <native-id> --transport <transport> --confirm
```

For FIX writes, also set `DERIBIT_FIX_ENABLED=true` and `RUN_DERIBIT_FIX_TESTS=1`. Every response must have `verified=true`; `readState` is a distinct HTTP JSON-RPC read with the same native order identity. No command changes transport after an error.

Verify terminal state separately:

```bash
./bin/bybitctl --venue deribit order status --order-id <native-id>
./bin/bybitctl --venue deribit order trades --order-id <native-id>
./bin/bybitctl --venue deribit orders list --instrument BTC-PERPETUAL
./bin/bybitctl --venue deribit positions --currency BTC --kind future
```

For a passive lifecycle, expect `cancelled`, no trades, no open order, and position `direction=zero`. If a minimum fill is intentionally tested, cleanup must be connector-owned and reduce-only.

## 5. Complete automated E2E

After confirming both accounts start with no relevant exposure:

```bash
RUN_MULTI_VENUE_E2E=1 \
RUN_BYBIT_READ_TESTS=1 RUN_BYBIT_TRADING_TESTS=1 \
RUN_DERIBIT_READ_TESTS=1 RUN_DERIBIT_TRADING_TESTS=1 \
RUN_DERIBIT_FIX_TESTS=1 \
scripts/e2e/run_multi_venue_e2e.sh
```

The final JSON must contain:

```json
{
  "result": "PASS",
  "independentReads": true,
  "privateEvents": true,
  "positionsZero": true,
  "fix": { "bybit": "BLOCKED_GATE", "deribit": "PASS" }
}
```

The runner covers every CLI command family, uses an isolated v1 fixture for migration/restore, runs bounded public/private streams, enables and reads connection COD, exercises HTTP/WS/FIX mutations, matches ACK trade/fee totals to canonical history, and independently reads final zero exposure. `BLOCKED_GATE` is an explicit external limitation, not a pass.
