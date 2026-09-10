# VenueWire V3.1 Public Testnet Web Console Handoff

Updated: 2026-09-10

Delivery status: **Testnet MVP deployed and externally verified**

## Completed Work

VenueWire now uses an authenticated Vue 3 Web Console as its primary interview-demo interface. The Go backend continues to use the existing Bybit/Deribit HTTP, JSON-RPC, WebSocket, FIX, persisted data, and CLI components. The connectors were not rebuilt, and the V2 contract/FIX semantics were not incorrectly applied to the new Spot Quick Trade flow.

- The program can load dotenv itself through `--env-file`; existing OS variables, including explicitly empty values, take precedence.
- Fixed-credential login, absolute session expiry, server-side logout, Secure/HttpOnly/SameSite cookies, CSRF protection, Host/Origin validation, and the trusted-proxy boundary are complete.
- The Browser can switch independently between Bybit and Deribit and reads normalized account snapshots from the backend cache. Browser traffic does not directly trigger venue API calls.
- The home page displays exchange-reported USD separately from the local public-price USD mark. Partial pricing shows only the priced subtotal; USDT is not assumed to equal one US dollar.
- The Browser WebSocket provides an initial snapshot, continuous sequence numbers, process instance ID, revision rollback protection, bounded resync, and account/valuation/trade/health events.
- Quick Trade supports Bybit BTC↔USDT and ETH↔USDT, plus Deribit BTC↔USDC Spot. Review → Confirm uses a five-second quote, 0.5% protection, Limit IOC, exact decimals, metadata/fee/capacity validation, and no-borrow submission.
- Confirm persists the intent, idempotency index, quota, and active slot before sending exactly once. `Unknown` is not resubmitted and retains the sole concurrent slot. Startup, private reconnect, periodic recovery, and Recheck share serialized reconciliation.
- The distinct semantics for Filled, partial-cancel, zero-fill, Rejected, Unknown, actual fees, net received amount, and balance sync are implemented.
- System Status exposes actual public/private receive/event age, reconnects, REST order RTT, ACK-to-order/execution event timing, rate limits, reconciliation status, and discrepancies.
- Recent Trades is shared demo history. Incomplete trades remain visible after a new login or Browser reload. Recheck only queries the venue; it never resubmits or force-resolves.
- All 15 V3.1 §23 failure scenarios have deterministic local fixtures.

## Verified Results

```text
go test ./...                                        PASS: 419 tests / 23 packages
go test -race ./... -count=1 -timeout=240s           PASS: 419 tests / 23 packages
go vet ./...                                          PASS
npm --prefix web run typecheck                        PASS
npm --prefix web test -- --run                        PASS: 13 tests / 5 files
npm --prefix web run build                            PASS
make build                                             PASS: bin/venuewire
```

See `docs/v3/TEST_REPORT_V3.md` for complete evidence by requirement and the root `IMPLEMENTATION_STATUS.md` for phase history.

## Build and Start

```bash
make build
sha256sum ./bin/venuewire
./bin/venuewire --env-file /absolute/path/to/venuewire.env web
```

Without `--env-file`, the program looks relative to the executable: it reads `bin/.env` first and then the parent `.env`. Settings in the binary's directory take precedence when the same key appears in both files, while the OS environment remains above both. You can therefore run `./bin/venuewire web` directly without depending on the current working directory.

`make build` performs a locked frontend install, typecheck/build, and the `webui`-embedded Go build. The CLI can still be built separately:

```bash
go build -o ./bin/venuewire ./cmd/venuewire
```

Do not `source .env` first. Create an untracked mode-600 configuration file from the root `.env.example`; `docs/3_web/.env.example` is the identical specification attachment. Supply:

- The Go host's own private `WEB_HOST` and `WEB_PORT`.
- The single HTTPS `WEB_PUBLIC_ORIGIN`.
- The Nginx `/32` that Go actually sees in `WEB_TRUSTED_PROXY_CIDRS`.
- `WEB_USERNAME`, a `WEB_PASSWORD` of at least 12 characters, and a `WEB_SESSION_SECRET` with at least 32 bytes of entropy.
- Testnet credentials for enabled venues; Deribit continues to use `DERIBIT_API_KEY` and `DERIBIT_API_SECRET`.

Keep `WEB_TRADING_ENABLED=false` initially. The V3.1 canonical quote setting is `QUOTE_TTL=5s`. Demo limits are 10 trades per session, 30 in the global rolling hour, one global concurrent trade, and the documented per-venue source-asset caps.

## Deployment Boundary

The production topology must be Browser HTTPS/WSS → existing Nginx → private HTTP → VenueWire Go. Go does not terminate TLS, and its port must not be exposed directly to the Internet.

Reference files:

- `docs/3_web/deploy/nginx-http-map.conf`
- `docs/3_web/deploy/nginx-proxy-common.conf`
- `docs/3_web/deploy/nginx-https-locations.conf`
- `docs/v3/DEPLOYMENT.md`

Nginx must overwrite XFF/Real-IP, preserve the public Host, prevent upstream retry of trade POST requests, and support the `/api/ws` upgrade. Only the deployment operator may run `nginx -t` or reload after verifying the actual IPs, ACLs, existing locations, and certificates.

## Completed External Verification

- Bybit/Deribit Testnet accounts, public/private streams, and public HTTPS/WSS were exercised.
- Small Limit IOC orders on Deribit `BTC_USDC` and Bybit `BTCUSDT` returned venue order IDs and persisted terminal fills, average prices, fees, and balance sync. Deribit also reconciled successfully after a service restart.
- Both Bybit `ETHUSDT` directions and both Deribit `BTC_USDC` directions produced executable quotes; stale books continued to fail closed.
- Headless Chrome verified login/logout, venue switching, route options, quote expiry, About, desktop/mobile layouts, contrast, zebra rows, and no post-login console errors.
- The operator reported a passing `nginx -t` and completed reload. This process did not directly read privileged Nginx or host-firewall configuration.

## Rules for External Verification

1. Start in read-only mode and verify both account snapshots, public/private stream freshness, and WSS.
2. Check existing open orders, balances, API permissions, and Demo caps.
3. Enable `WEB_TRADING_ENABLED=true` and the necessary venue gate only after receiving explicit authorization for Testnet orders.
4. Execute only the selected small direction and preserve VenueWire/client/venue IDs plus independent reconciliation evidence.
5. If the outcome is `Unknown`, retain it and query through Recheck. Never Confirm again or release the slot manually.
6. Remove credentials, cookies, account IDs, hostnames, private IPs, request authorization, and API/FIX secrets before screenshots or recordings.
7. Disable Web trading after testing. Before any cleanup, verify connector ownership and an independent terminal state.

## Explicit MVP Limitations

- Interviewers use only the Web UI. The CLI remains an engineering test interface and does not receive intensive DX work.
- One shared login and shared Recent Trades are confirmed Demo design choices.
- There is no public fault simulator.
- There is no strong CLI/Web consistency or multi-process/multi-instance quota coordination.
- Mainnet, transfers, withdrawals, strategies, automated trading, arbitrage lines, a full order book, a third venue, smart routing, and cross-venue failover are unsupported.
- This verification applies only to the current Testnet deployment. Repeat external verification after changing the account, host, Nginx, or network boundary.
