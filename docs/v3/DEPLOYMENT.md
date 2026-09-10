# VenueWire V3.1 deployment

Status: implementation and reference configuration are locally verified. No Nginx, firewall, certificate, service or Testnet account change was performed during V3.1 work; real split-host deployment is `NOT_RUN`.

## Topology

```text
Interviewer Browser
        |
      HTTPS/WSS
        |
   edge Nginx host
        |
  private HTTP network
        |
 VenueWire Go host
```

TLS terminates at the existing Nginx. VenueWire does not accept TLS certificate settings. The Go listener binds an explicit private interface; a host/network ACL must allow its port only from the actual Nginx source address.

## Build

From a clean checkout with Go, Node and npm available:

```bash
make build
sha256sum ./bin/venuewire
./bin/venuewire help
```

`make build` runs `npm ci`, frontend typecheck/build and `go build -tags webui -o ./bin/venuewire ./cmd/venuewire`. The `webui` build embeds the generated production assets. A normal CLI-only build remains available without frontend assets.

## Configuration

Copy the canonical root `.env.example` to an operator-owned path outside the repository when practical, set mode 600 and fill secrets without printing them. `docs/3_web/.env.example` intentionally mirrors the same values for the specification bundle.

```bash
install -m 600 .env.example /etc/venuewire/venuewire.env
```

Critical deployment relationships:

```dotenv
WEB_HOST=10.0.0.20
WEB_PORT=8080
WEB_PUBLIC_ORIGIN=https://trade.example.com
WEB_TRUSTED_PROXY_CIDRS=10.0.0.10/32
WEB_TRADING_ENABLED=false
QUOTE_TTL=5s
```

- `WEB_HOST` is an address owned by the Go host, not the Nginx host and not a wildcard.
- `WEB_TRUSTED_PROXY_CIDRS` contains the direct address Go actually sees for Nginx. Each entry must represent exactly one host; forwarded proxy chains are rejected.
- `WEB_PUBLIC_ORIGIN` is the one external HTTPS origin and determines the accepted Host/Origin boundary.
- Generate `WEB_SESSION_SECRET` with a cryptographically secure tool and use a unique `WEB_PASSWORD` of at least 12 characters. Never commit either.
- Keep endpoint constants on the exact documented Bybit/Deribit Testnet allowlist. Fill only credentials for enabled venues and preserve the existing independent read/trading/FIX gates.
- Begin with `WEB_TRADING_ENABLED=false`. Enabling Browser writes is an operator decision after read/stream verification and account-limit review.

VenueWire loads dotenv itself; existing OS variables, including explicitly empty values, take precedence:

```bash
./bin/venuewire --env-file /etc/venuewire/venuewire.env web
```

When `--env-file` is omitted, it reads `.env` beside the executable and then `.env` in that directory's parent. The binary-local file wins duplicate file keys, while OS environment still wins both. This permits the standard `bin/.env` or project-root `.env` layouts without depending on the service working directory:

```bash
./bin/venuewire web
```

## Nginx integration

Use the reviewed reference fragments rather than creating an unrelated public server:

- `docs/3_web/deploy/nginx-http-map.conf` — place in `http {}` scope.
- `docs/3_web/deploy/nginx-proxy-common.conf` — common no-cache, no-retry and forwarding headers.
- `docs/3_web/deploy/nginx-https-locations.conf` — merge its HTTP and WebSocket locations into the existing TLS server.

Set the public host and private upstream explicitly. Nginx must replace, not append, client forwarding headers; proxy retry must not replay trade POSTs. Preserve the existing HTTP→HTTPS redirect, certificate/ACME paths and default-vhost protection.

After reviewing the actual host diff, the deployment operator runs:

```bash
sudo nginx -t
# Reload only if the syntax test and host review pass.
sudo systemctl reload nginx
```

These commands are documentation, not evidence that the V3.1 deployment occurred.

## Host ACL

Allow `Nginx-private-IP -> Go-private-IP:WEB_PORT/TCP` and deny other sources. Check existing rule order, IPv4/IPv6, NAT/container mappings and SSH access before changing a firewall. A private bind, network ACL, trusted-peer validation and application session/CSRF checks are four separate layers.

## Deployment verification

Record each item as PASS, FAIL or NOT_RUN:

1. Only the HTTPS origin is Internet-visible; direct Go access is denied by ACL.
2. Login sets Secure/HttpOnly/SameSite=Strict host-only cookie and returns no secrets.
3. Unauthenticated private REST and WebSocket requests fail.
4. Forged Host, Origin, XFF chain or requests from an untrusted direct peer fail.
5. `/api/ws` upgrades through WSS, receives snapshot/events and survives expected heartbeat idle periods.
6. Logout and absolute session expiry close their WebSockets; relogin restores shared unresolved history.
7. Venue switching never displays a late response/revision from the other venue.
8. With trading disabled, Confirm is rejected server-side.
9. Only after separate order authorization: one bounded Testnet Spot flow shows persisted ID correlation, terminal/Unknown behavior and account synchronization.

## Rollback

Disable new Browser submissions first with `WEB_TRADING_ENABLED=false` and restart. Preserve the durable intent state and inspect every unresolved trade; do not delete state or assume shutdown cancelled an order. Roll back Nginx only after retaining a recovery path to the Go service. An older binary must not open a state version it does not understand.

The more detailed address/ACL checklist remains in `docs/3_web/deploy/DEPLOYMENT_NOTES.md`.
