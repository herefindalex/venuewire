# Bybit + Deribit Multi-Venue Upgrade Handoff

Updated: 2026-09-09

## Completed Work

This project incrementally added Deribit to the existing Bybit Testnet connector. It did not create an unrelated program or remove Bybit. When `--venue` is omitted, the legacy Bybit CLI behavior remains the default.

- Phases 0–7: completed R1 venue routing, composite identity, Deribit HTTP/WS, plan→execute, persistence, recovery, read-only aggregation, and external lifecycle behavior.
- Phases 8–9: completed an independent Deribit FIX dialect, local mock, real Testnet Logon, and D/G/F order flow.
- Phase 10: completed COD, tiered ticks, the private-event reducer, full WS create/edit/cancel, command-level E2E, and shared documentation.

Primary correctness guarantees:

- Identity includes venue/environment/account/namespace/native ID, so equal order/trade IDs from two venues cannot overwrite one another.
- Deribit JSON-RPC validates `jsonrpc`, request ID, and result/error. Token refresh uses single-flight, and only read-only calls receive bounded retries under auth/rate-limit conditions.
- Deribit HTTP/WS quantities and prices are sent as JSON numbers. Internal validation uses exact decimals; trading calculations do not use `float64`.
- HTTP/WS `amount` for `BTC-PERPETUAL` and `ETH-PERPETUAL` is USD notional, while settlement and fees remain native BTC/ETH. A Deribit account holding USDT does not make these inverse perpetuals USDT-settled.
- Metadata preserves `tick_size_steps`. The plan selects the valid tick for the current price tier and does not silently widen or round across a boundary.
- A new Deribit order must be planned first and then run through `execute --confirm` within its TTL. HTTP, WS, and FIX are explicit transport choices; a failed request is never resent through another transport.
- HTTP, WS, and FIX create/edit/cancel operations are verified through an independent HTTP JSON-RPC read. An ACK alone is never completion evidence.
- The private WS parser handles every order/trade/position in one `user.changes` message. Orders and canonical `trade_id` values pass through the same reducer used by reconciliation, preventing duplicate fee application.
- Cleanup touches only exposure owned by this connector run, uses reduce-only, and finishes by reading the position again to verify zero.

## Build and Configuration

```bash
go build -o ./bin/venuewire ./cmd/venuewire
cp .env.example .env
chmod 600 .env
set -a
source .env
set +a
./bin/venuewire help
```

Deribit uses `DERIBIT_API_KEY` and `DERIBIT_API_SECRET`; JSON-RPC and FIX share these Testnet client credentials. Never put their values in code, documentation, logs, or commits. `.env` is excluded by `.gitignore`; this work did not modify or delete it.

`.env.example` contains symmetric gates:

- `RUN_BYBIT_READ_TESTS`, `RUN_BYBIT_TRADING_TESTS`, `RUN_BYBIT_FIX_TESTS`
- `RUN_DERIBIT_READ_TESTS`, `RUN_DERIBIT_TRADING_TESTS`, `RUN_DERIBIT_FIX_TESTS`
- Every E2E write also requires `RUN_MULTI_VENUE_E2E=1`

The old `RUN_BYBIT_INTEGRATION` and `RUN_BYBIT_WS_INTEGRATION` compatibility names are not retained.

## COD Behavior

`doctor` displays account-scoped Cancel-on-Disconnect in read-only mode. `private-stream` queries connection-scoped COD on the same WS connection and does not modify it by default.

Enable it only with an explicit command:

```bash
RUN_MULTI_VENUE_E2E=1 RUN_DERIBIT_TRADING_TESTS=1 \
  ./bin/venuewire --venue deribit private-stream \
  --duration 10s --enable-connection-cod --confirm
```

This does not modify account scope. COD is not a synchronous cancellation guarantee; order/trade state must still be queried after a disconnect. Orders created through HTTP or another connection are not incorrectly marked as protected by this connection.

## Real Testnet Verification

The final full runner passed from 2026-09-09T19:37Z to 19:39Z. The build was based on `d7f0031`, with the migration assertion fixed in `d65c1cc`:

```json
{
  "result": "PASS",
  "independentReads": true,
  "privateEvents": true,
  "positionsZero": true,
  "fix": { "bybit": "BLOCKED_GATE", "deribit": "PASS" }
}
```

Latest minimum-fill fees:

- Bybit ETHUSDT linear: entry and cleanup were both `0.01 ETH`; fees were `0.01367245` and `0.01367234 USDT`.
- Deribit BTC-PERPETUAL: entry and cleanup were both `10 USD` notional; fees were `0.00000006` and `0.00000006 BTC`.

Additional independent evidence:

- Deribit WS amend read: `verified=true`, state `open`; WS cancel read: `verified=true`, state `cancelled`.
- Private stream: connection COD query `enabled=true`, ready generation 1, ten notifications received.
- The private reducer stored five Deribit orders and two canonical executions in isolated state.
- A separate read API check after the runner showed zero open orders and zero nonzero positions on both Bybit and Deribit.
- Migration dry-run/apply/restore passed with backup mode `0600`.

## FIX Verification Levels

| Level | Deribit | Evidence |
|---|---|---|
| `IMPLEMENTED` | PASS | Independent auth/session/codec/order dialect |
| `LOCAL_TESTED` | PASS | Fixtures/mocks, heartbeat, resend/reset, SecurityList, D/G/F/8/9 |
| `TESTNET_LOGON` | PASS | Real TLS Logon/Logout at `fix-test.deribit.com:9883` |
| `TESTNET_ORDER_FLOW` | PASS | 10 USD BTC-PERPETUAL D/G/F, independently checked through JSON-RPC |

Deribit FIX uses `TargetCompID=DERIBITSERVER`, a 32-byte nonce, a strictly increasing timestamp, and `Base64(SHA256(RawData || client_secret))`, not Bybit RSA. A live order is permitted only after SecurityList proves the conversion between JSON amount and FIX contracts/multiplier.

The local Bybit FIX mock passed, but live Bybit FIX remains `BLOCKED_GATE`: the independent RSA credentials/whitelist gate was not enabled. Do not describe it as a live PASS.

## Known Limitations and Next Steps

- There is no dashboard/UI. Phase 0 confirmed that the only existing user interface was the CLI, so there was no UI to migrate.
- ShellCheck was not installed and is recorded as `BLOCKED_TOOLING`; `bash -n` passed, but ShellCheck must not be described as PASS.
- Supported products are intentionally limited to BTC/ETH perpetuals. Options, dated futures, portfolio-margin orchestration, cross-venue smart routing, wallet transfers, and Mainnet are excluded.
- Live Bybit FIX requires the user to provide and enable that venue's RSA/whitelist access separately. This does not change Deribit R2 having reached `TESTNET_ORDER_FLOW`.

See `docs/upgrade/DEMO.md` for complete commands and `docs/upgrade/VALIDATION_REPORT.md` for requirement-level tests and external evidence.
