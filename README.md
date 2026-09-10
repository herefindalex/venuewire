# VenueWire

VenueWire demonstrates real multi-venue connectivity and order-lifecycle correctness on Bybit and Deribit Testnet. It includes authenticated REST/HTTP JSON-RPC, public and private WebSocket streams, durable intent/reconciliation state, and separate Bybit and Deribit FIX 4.4 dialects.

It is not a trading strategy, dashboard, Mainnet client, wallet-transfer tool, or withdrawal tool. Every order is initiated explicitly from the CLI. Remote endpoints are restricted to exact Testnet allowlist entries.

## Safety model

- Secrets come only from the process environment. `.env`, key files, generated state, and binaries are ignored. `.env.example` has empty credentials.
- Mainnet, redirects, altered endpoint paths, malformed limits, ambiguous amount units, and unsupported instruments fail closed.
- Deribit new orders use a short-lived durable `plan`, followed by `execute --confirm`.
- Transports are explicit. HTTP, WebSocket, and FIX failures never trigger automatic cross-transport or cross-venue retries.
- An ACK is not business-state proof. Every create, amend, cancel, fill, fee total, and cleanup is checked by an independent read path.
- Cleanup touches only connector-owned exposure and uses reduce-only orders, followed by a separate zero-position read.
- Deribit inverse perpetuals use native BTC/ETH collateral. The supported HTTP/WS order amount is USD notional, not BTC or ETH quantity. USDT assets in a Deribit account do not change that contract unit or settlement currency.

## Build and local checks

Go 1.26.3 was used for the final verification.

```bash
go build -o ./bin/venuewire ./cmd/venuewire
./bin/venuewire help

go test ./...
go test -race ./...
go vet ./...
bash -n scripts/e2e/run_multi_venue_e2e.sh
```

## Configure

Create a local configuration and fill only credentials issued for the corresponding Testnet accounts:

```bash
cp .env.example .env
chmod 600 .env
```

Load every parameter into the current shell without printing values:

```bash
set -a
source .env
set +a
```

Deribit uses `DERIBIT_API_KEY` and `DERIBIT_API_SECRET` for both JSON-RPC and FIX. Set `DERIBIT_ENABLED=true` for Deribit commands and `DERIBIT_FIX_ENABLED=true` only for live FIX. Keep the endpoint constants unchanged.

External test gates are symmetric and have no legacy aliases:

```dotenv
RUN_MULTI_VENUE_E2E=0
RUN_BYBIT_READ_TESTS=0
RUN_BYBIT_TRADING_TESTS=0
RUN_BYBIT_FIX_TESTS=0
RUN_DERIBIT_READ_TESTS=0
RUN_DERIBIT_TRADING_TESTS=0
RUN_DERIBIT_FIX_TESTS=0
```

The removed `RUN_BYBIT_INTEGRATION` and `RUN_BYBIT_WS_INTEGRATION` names are intentionally unsupported.

## Common reads

Bybit remains the default when `--venue` is omitted:

```bash
./bin/venuewire time
./bin/venuewire account balances --coin BTC,ETH,USDT
./bin/venuewire positions --category linear --symbol ETHUSDT
```

Deribit reads:

```bash
./bin/venuewire --venue deribit doctor
./bin/venuewire --venue deribit account balances --currency all
./bin/venuewire --venue deribit positions --currency BTC --kind future
./bin/venuewire --venue deribit orders list --instrument BTC-PERPETUAL
```

`doctor` reports public health, authenticated account access, and the account-scope Cancel-on-Disconnect setting. Aggregation keeps venue failures and native currencies separate:

```bash
./bin/venuewire --venue all status
./bin/venuewire --venue all portfolio
```

## Streams and connection-scoped COD

```bash
./bin/venuewire --venue deribit market orderbook --instrument BTC-PERPETUAL --depth 10 --duration 5s
./bin/venuewire --venue deribit private-stream --duration 10s
```

Private streams query and report the effective connection-scope COD state without changing it. To enable COD only for that private connection, all write gates and explicit confirmation are required:

```bash
RUN_MULTI_VENUE_E2E=1 RUN_DERIBIT_TRADING_TESTS=1 \
  ./bin/venuewire --venue deribit private-stream \
  --duration 10s --enable-connection-cod --confirm
```

This does not alter account-scope COD and does not imply synchronous cancellation. Orders and trades must still be read after a disconnect. Private `user.changes` messages decode all order/trade/position entries; orders and canonical trade IDs enter the same persistent reducer used by reconciliation.

## Planned Deribit orders

Create a plan with live metadata and a current price, then execute it before its configured TTL expires:

```bash
./bin/venuewire --venue deribit order plan \
  --instrument BTC-PERPETUAL --side buy --amount 10 \
  --type limit --price <valid-passive-price> --transport ws --post-only

./bin/venuewire --venue deribit order execute --plan-id <plan-id> --confirm
```

Use `--transport http`, `ws`, or `fix` deliberately. Amend and cancel support all three and independently verify the result through canonical HTTP JSON-RPC reads:

```bash
./bin/venuewire --venue deribit order amend \
  --order-id <native-id> --amount 10 --price <valid-price> \
  --transport ws --confirm

./bin/venuewire --venue deribit order cancel \
  --order-id <native-id> --transport ws --confirm
```

FIX amend/cancel additionally require persisted connector-owned label/native-ID/instrument identity.

## FIX validation levels

Local mocks do not need exchange credentials or `DERIBIT_ENABLED=true`, and do not write configured persistent order state:

```bash
./bin/venuewire --venue bybit fix mock-demo
./bin/venuewire --venue deribit fix mock-demo
```

Live Deribit FIX Logon requires `DERIBIT_FIX_ENABLED=true` and `RUN_DERIBIT_FIX_TESTS=1`:

```bash
./bin/venuewire --venue deribit fix connect-testnet --duration 3s
```

Deribit reached `TESTNET_ORDER_FLOW`: a metadata-minimum BTC-PERPETUAL D/G/F lifecycle was sent through FIX and independently reconciled over JSON-RPC. Bybit FIX is `LOCAL_TESTED`; live Bybit FIX remains `BLOCKED_GATE` because its separate RSA credentials and exchange whitelist access were not enabled.

## Full opt-in E2E

Inspect both Testnet accounts for existing open orders and positions first, then run:

```bash
RUN_MULTI_VENUE_E2E=1 \
RUN_BYBIT_READ_TESTS=1 RUN_BYBIT_TRADING_TESTS=1 \
RUN_DERIBIT_READ_TESTS=1 RUN_DERIBIT_TRADING_TESTS=1 \
RUN_DERIBIT_FIX_TESTS=1 \
scripts/e2e/run_multi_venue_e2e.sh
```

The runner uses isolated state, tests migration dry-run/apply/restore, derives current metadata-minimum sizes and prices, exercises HTTP/WS/FIX lifecycles, verifies private events and canonical fees, performs connector-owned reduce-only cleanup, and independently proves final zero positions. An unavailable live FIX surface is reported `BLOCKED_GATE`, never silently counted as passing.

## Documentation

- `docs/PROJECT_IDENTITY.md` — canonical project, module, CLI, and compatibility identifiers
- `IMPLEMENTATION_STATUS.md` — phase and verification status
- `docs/upgrade/DEMO.md` — reproducible demonstration
- `docs/upgrade/VALIDATION_REPORT.md` — sanitized external evidence and requirement audit
- `docs/upgrade/CAPABILITY_MATRIX.md` — venue/transport capabilities
- `docs/upgrade/PROTOCOL_NOTES.md` — protocol decisions and observed dialect details
- `docs/upgrade/MIGRATION.md` — reversible snapshot v1→v2 migration
- `docs/upgrade/CODEX_HANDOFF.md` — Traditional Chinese handoff
- `docs/1_bybit/` — archived Bybit-only documentation
- `docs/2_deribit/DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md` — controlling upgrade specification
