# VenueWire V3.1 demonstration

This guide separates credential-free local evidence, an operator-configured read-only Web demonstration and an explicitly authorized Testnet trade. Do not turn on gates merely to make a demo appear green.

## 1. Credential-free local proof

From the repository root:

```bash
go test ./... -count=1 -timeout=180s
go test -race ./... -count=1 -timeout=240s
go vet ./...
npm --prefix web test
npm --prefix web run build
go build -tags webui -o /tmp/venuewire-web ./cmd/venuewire
```

The section 23 fault fixtures can be shown without external traffic:

```bash
go test -v ./internal/intent ./internal/quicktrade ./internal/tradereconcile ./internal/accountstate ./internal/webconsole ./cmd/venuewire
```

They demonstrate duplicate Confirm, two-tab idempotency, uncertain timeout, private disconnect before ACK, restart recovery, partial/zero fill, fill-after-cancel, stale streams, instrument rejection, rate limit, refresh during Unknown, all Unknown terminal resolutions and disabled trading.

Existing connector/FIX local mocks remain available:

```bash
go build -o /tmp/venuewire-cli ./cmd/venuewire
/tmp/venuewire-cli --venue bybit fix mock-demo
/tmp/venuewire-cli --venue deribit fix mock-demo
```

## 2. Build the interview binary

```bash
make build
sha256sum ./bin/venuewire
```

Prepare an untracked mode-600 file from the root `.env.example`. Keep Browser trading disabled initially and start the service through the intended split-host path:

```bash
./bin/venuewire --env-file /absolute/path/to/venuewire.env web
```

For the standard repository layout, `./bin/venuewire web` also searches `bin/.env` and then the project-root `.env`; binary-local keys win parent-file keys and OS environment wins both.

Do not demonstrate through direct public access to the Go port. Use the configured Nginx HTTPS URL so the proxy, Host, Origin, Secure cookie and WSS boundaries are real.

## 3. Read-only interviewer walkthrough

1. On the login page, point out VenueWire, Bybit/Deribit and the prominent TESTNET/no-real-money wording.
2. Sign in with the shared demo credential without exposing the password in screen recording or shell history.
3. Switch venues. Explain account snapshot time, exchange-reported total, local USD mark or partial subtotal, unpriced assets and separate stream/account freshness.
4. Open System Status. Show public/private receive and event ages, reconnect counts, order/event timings, rate-limit state, reconciliation status and non-sensitive build metadata.
5. Open Quick Trade. Show the two routes for the selected venue, source-asset amount, fixed 0.5% protection, Limit IOC parameters, fee estimate and five-second expiry. With `WEB_TRADING_ENABLED=false`, the session is view-only and Confirm remains server-blocked.
6. Open Recent Trades and a lifecycle drawer. VenueWire/client/venue IDs, fills, fee/net amount, balance sync and Recheck behavior should be explainable. Recheck only queries.
7. Open About and explain the Browser → Nginx → Go → normalized adapters/state/reconciliation architecture.

## 4. Authorized Testnet write walkthrough

This walkthrough was exercised on 2026-09-10 after the operator explicitly authorized Testnet writes and enabled the required venue/Web gates. Repeat the checks and obtain fresh authorization before any later run.

For one supported direction:

1. Review a small source-asset budget within both account capacity and configured venue cap.
2. Confirm once before the quote expires. Do not repeat Confirm to chase an ambiguous response.
3. Record the VenueWire intent ID, client order ID, venue order ID/Unknown state, REST/RPC RTT and first event timings.
4. Verify IOC Filled, partial-cancel, zero-fill or rejection with independent reconciliation evidence.
5. Verify the shared Recent Trades lifecycle and that account synchronization is reported separately.
6. If the outcome is Unknown, leave it active and use bounded Recheck; never resubmit it.
7. Record only sanitized screenshots/JSON. Remove usernames, cookies, account identifiers, hostnames/private IPs, request authentication material and all credentials.

## 5. Expected evidence labels

- `LOCAL_VERIFIED`: deterministic tests, fixture UI, build or local mock.
- `TESTNET_VERIFIED`: current authorized exchange observation with timestamp and sanitized evidence.
- `NOT_RUN`: no attempt was authorized or available.
- `BLOCKED`: an authorized verification could not proceed because a required external gate or environment was unavailable.

Prior V2 connector Testnet/FIX evidence remains valid historical evidence but does not prove the new Browser Quick Trade deployment.
