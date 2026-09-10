# VenueWire

VenueWire is a public-demo-oriented multi-venue trading connectivity and execution prototype for Bybit and Deribit Testnet. Its primary interface is an authenticated Vue 3 Web Console backed by Go; the existing CLI and the independently implemented HTTP/JSON-RPC, WebSocket and FIX connector surfaces remain available for engineering tests.

The project demonstrates normalized account state, exact-decimal Spot quoting, durable idempotent trade intents, Limit IOC execution, partial-fill and fee accounting, uncertain-outcome recovery, stream freshness and operational observability. It is not a strategy product, exchange clone, Mainnet client, transfer tool or withdrawal tool.

## Safety boundary

- Venue endpoints are restricted to exact Testnet allowlists. Redirects, altered paths, Mainnet and unsafe bind/proxy settings fail closed.
- Exchange credentials and Web login secrets come from process environment or a local dotenv file and never enter Browser DTOs. `.env`, key files, state, logs and binaries are ignored.
- Browser trading is disabled unless `WEB_TRADING_ENABLED=true`; venue read/trading/FIX gates remain independently enforced.
- Browser orders are limited to four fixed Spot directions: Bybit BTC↔USDT and Deribit ETH↔BTC. Review produces a five-second frozen quote; Confirm sends a fee-aware, no-borrow Limit IOC with 0.5% protection.
- Confirm persists the intent and quota reservation before submission. Duplicate confirmation cannot create a second logical trade. A transport-uncertain result becomes `Unknown` and is reconciled without automatic resubmission.
- Testnet writes and deployment changes require explicit operator authorization. Local tests and builds do not place orders.

## Build and verify

Go 1.26.3 and Node/npm are used by the current verification environment.

```bash
# Complete Web binary: locked frontend install, typecheck/build and Go embed.
make build
./bin/venuewire help

# Local regression checks; no exchange writes.
go test ./...
go test -race ./...
go vet ./...
npm --prefix web test
```

The ordinary CLI build remains independent of generated frontend assets:

```bash
go build -o ./bin/venuewire ./cmd/venuewire
```

## Configure the Web Console

Start from the canonical empty-secret example. The application loads it itself; do not `source` it and do not commit the resulting file.

```bash
cp .env.example .env.web
chmod 600 .env.web
```

At minimum, set the exact public HTTPS origin, private Go bind address, trusted Nginx source IP, Web login/session secrets and credentials for enabled Testnet venues. Keep the documented endpoint values unchanged. For an initial read-only demonstration, retain:

```dotenv
WEB_TRADING_ENABLED=false
```

Build and start:

```bash
make build
./bin/venuewire --env-file /absolute/path/to/.env.web web
```

Without `--env-file`, VenueWire searches `.env` beside the executable first (`bin/.env` for the standard build), then the executable directory's parent (`.env` at the project root). OS environment values have highest priority; for duplicate file keys, the binary-directory file wins. The search is based on the executable location, not the current working directory:

```bash
./bin/venuewire web
```

The Browser must enter through the configured HTTPS Nginx origin. Go serves HTTP on its configured private interface; it does not terminate TLS and its port must not be Internet-accessible. See [V3 deployment](docs/v3/DEPLOYMENT.md).

Web server logs are appended as JSON Lines to `log/venuewire.log` with mode 0600 and are also written to stderr. The ignored `log/` directory is created automatically. Each request accepted by the trusted proxy boundary records the request ID, method, path, status, response bytes, duration, and validated client IP. Rejected trade operations also record a stable error code and redacted provider cause. Logs deliberately omit query values, bodies, credentials, cookies, authorization headers, CSRF tokens, and session identifiers.

## Interviewer flow

1. Open the HTTPS URL and sign in with the configured shared demo login.
2. Switch between Bybit and Deribit and inspect account snapshot, local USD marks, exchange-reported values and System Status.
3. Open Quick Trade, select one of the two directions for that venue, enter a source-asset budget and review the exact IOC parameters and five-second expiry.
4. If trading is explicitly enabled, Confirm once and follow the durable lifecycle in Recent Trades. `Unknown` remains visible and occupies the concurrency slot until authoritative reconciliation resolves it.
5. Use Recheck only to query venue evidence; it never resubmits or force-resolves an order.

The authenticated history is shared because all interviewers operate the same configured demo accounts. Session expiry or logout does not discard pending trade recovery state.

## CLI engineering interface

CLI commands use the global venue selector; write commands never infer a destination venue from a symbol.

```bash
./bin/venuewire --venue bybit account balances --coin BTC,ETH,USDT
./bin/venuewire --venue deribit account balances --currency all
./bin/venuewire --venue all status
./bin/venuewire --venue all portfolio

./bin/venuewire --venue bybit fix mock-demo
./bin/venuewire --venue deribit fix mock-demo
```

`--venue all` is read-only. Live connector tests remain behind the symmetric `RUN_BYBIT_*`, `RUN_DERIBIT_*` and `RUN_MULTI_VENUE_E2E` gates documented in the V2 material; they are not part of the public interview workflow.

## Documentation

- [V3 API](docs/v3/API.md) — Browser REST/WebSocket contracts and safety boundary
- [V3 protocol notes](docs/v3/PROTOCOL_NOTES.md) — account, valuation and Spot execution semantics
- [V3 deployment](docs/v3/DEPLOYMENT.md) — private Go host and split-host Nginx procedure
- [V3 demo](docs/v3/DEMO.md) — safe local and interviewer demonstrations
- [V3 test report](docs/v3/TEST_REPORT_V3.md) — requirement and failure-scenario evidence
- [V3 handoff](docs/v3/CODEX_HANDOFF_V3.md) — Traditional Chinese operator handoff
- [Implementation status](IMPLEMENTATION_STATUS.md) — incremental phase history
- [V2 upgrade documentation](docs/upgrade/) — CLI/FIX connector capabilities and prior Testnet evidence
