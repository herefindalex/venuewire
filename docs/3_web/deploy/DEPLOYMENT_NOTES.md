# V3 Deployment Notes: Existing Nginx SSL → Go on Another Host

This document is the V3.1 split-host deployment reference. On 2026-09-10, the operator updated and reloaded Nginx, and the public HTTPS/WSS Browser flow was verified. This process did not directly inspect privileged Nginx or firewall configuration. `../TRADING_CONSOLE_V3_CHANGE_SPEC.md` remains the canonical acceptance specification.

V3.1 also follows the decisions confirmed in §26 of `../VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md`: shared trade history, global rolling-hour and concurrency limits, an `Unknown` trade retaining its slot, and restart recovery. The root `.env.example` is canonical; `../.env.example` must remain identical. Continue using `WEB_TRUSTED_PROXY_CIDRS` and `QUICK_TRADE_SLIPPAGE_BPS`, with `QUOTE_TTL=5s`.

## 1. Values to Supply

| Item | Example | Meaning |
|---|---|---|
| Public origin | `https://trade.example.com` | Used by Browser login, API, and WSS |
| Nginx source address | `10.0.0.10` | The proxy source that Go actually sees; verify it when NAT is present |
| Go bind address | `10.0.0.20` | The Go host's private interface, not loopback |
| Go port | `8080` | The ACL allows only Nginx; do not expose it through Internet port forwarding |

Use HTTP between the two hosts only on a trusted, controlled LAN or VLAN. Establish an encrypted private tunnel before crossing an untrusted network; external SSL does not encrypt the Nginx-to-Go leg.

## 2. Backend `.env`

Merge the new settings from the supplied `.env.example` into the existing configuration. Do not overwrite existing Bybit/Deribit keys, account aliases, state, or FIX settings. The user supplies all secrets on the Go host.

Generate `WEB_SESSION_SECRET` on your own host:

```bash
openssl rand -base64 32
```

Save the result directly in a restricted dotenv file. Do not commit it to Git or paste it into a conversation. Use a dedicated service account, set file mode 600, and verify the owner. `WEB_PASSWORD` must contain at least 12 characters; quote special characters according to the dotenv parser used by the program.

Codex should provide a startup command that matches the actual CLI, for example:

```bash
./bin/venuewire --env-file /etc/trading-console/console.env web
```

The program reads the file itself, so `source` is unnecessary. If systemd uses a different `WorkingDirectory`, pass an absolute `--env-file` path. Avoid also injecting the same values through `EnvironmentFile`; otherwise an existing OS environment value can silently override a changed `.env` value.

## 3. Three Example Nginx Files

Place `nginx-http-map.conf` in the `http {}` scope. For example, if the environment already includes `conf.d/*.conf` from `http`, an operator may put it in that directory. Do not include it from a `server` block.

Save `nginx-proxy-common.conf` as `/etc/nginx/snippets/trading-console-proxy-common.conf` and update the public Host first.

Merge the contents of `nginx-https-locations.conf` into the existing HTTPS server. Update the Go IP and common-include path. Do not copy it into a second conflicting `location /` or `server_name`. Preserve the existing HTTP-to-HTTPS redirect, certificate renewal/ACME configuration, and default-vhost protections.

The example assumes a single edge Nginx. If the existing configuration enables `real_ip`, verify that it trusts only the actual upstream proxy. An arbitrary client must not be able to rewrite `$remote_addr` through a header.

After reviewing the changes, the operator runs:

```bash
sudo nginx -t
# Reload only after the command above passes and the changes have been reviewed.
sudo systemctl reload nginx
```

Codex must not operate the user's Nginx or firewall directly.

## 4. Go Host ACL

The rule must allow “actual Nginx source → Go private IP:8080/TCP” and reject every other source. Inspect the existing firewall rule order, IPv4/IPv6 behavior, container mappings, and management channel first. Do not automatically apply rules that could affect SSH on an unknown host.

Binding only to a private IP does not mean that only Nginx can connect. All three layers are required: the network ACL, Go's trusted-proxy check, and application session/CSRF protection. None replaces another.

## 5. Demo and Trading Modes

```dotenv
# Every signed-in user can view assets and quotes but cannot submit orders.
WEB_TRADING_ENABLED=false
```

```dotenv
# Every user sharing the fixed credentials can perform permitted Testnet Spot trades.
WEB_TRADING_ENABLED=true
```

This version has one fixed login account. It does not distinguish “Alex can trade” from “visitors are read-only.” Restart after changing the setting. The backend must recover pending intents before reopening trading.

## 6. Deployment Checks

- External clients see only the HTTPS origin, the API uses relative paths, and the WebSocket connection uses WSS.
- The login cookie has `Secure / HttpOnly / SameSite=Strict / Path=/` and no `Domain` attribute.
- An unauthenticated request to `/api/venues/.../account` returns 401; an unauthenticated WebSocket cannot upgrade.
- After login, `/api/ws` returns 101 and heartbeats keep it alive beyond the ordinary idle timeout.
- Logout or session expiration closes the old WebSocket; logging in again still exposes prior trade intents.
- Forged XFF, Origin, or Host values cannot bypass rate limits or write protections.
- `.env`, `.ENV`, `.git`, state, and logs cannot be downloaded.
- The Go port rejects unapproved hosts; Nginx neither caches nor retries upstream requests.
- After one authorized Testnet trade completes, the modal and background balance agree. An HTTP 200 alone is not sufficient verification.

## 7. Rollback

Before rolling back the UI or binary, stop accepting new Web intents and preserve and inspect unfinished trades. Follow the schema migration's backup and compatibility procedure. Do not point an older binary at an unknown data format. Keep every outcome-unknown trade traceable; never clear state merely because a version is being rolled back.

The current deployment has verified HTTPS/WSS, the service, and reported small Testnet trades. Reverify in the deployment environment after changing IPs, ACLs, certificates, or accounts.
