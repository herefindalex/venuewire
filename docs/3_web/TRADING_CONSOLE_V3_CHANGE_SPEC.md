# Multi-Venue Trading Console V3 — Formal Incremental Change Specification

**Change ID: CR-003 · Version: 3.0 · Date: 2026-09-10**
**Intended for: Codex · Scope: Existing Go-based Bybit + Deribit project**
**Primary goal: Add an authenticated Web trading console, exposed externally through Nginx HTTPS, on top of the already completed exchange integrations.**

> This document defines development and acceptance requirements. It is not proof that the current implementation has already passed these tests. The user reports that both exchanges are already connected, but the repository source code was not provided for this revision. Codex must use Phase 0 to audit the actual implementation. Do not assume all items in previous specifications are already complete, and do not build a second trading core just for the Web upgrade.
>
> Previous reference: `DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md`. This document explicitly overrides the previous limitations of "do not build a full Web frontend" and "Deribit first release only supports inverse perpetual order entry": **V3 adds an authenticated Web interface and Quick Trade support for specific Spot instruments. Existing contracts/FIX/CLI capabilities must remain intact, and product units must never be mixed.**
>
> V3.1 implementation must also follow `VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md` and its confirmed decisions in §26. Where the documents conflict, §26 takes precedence, especially for global Demo concurrency limits. Keep the V3 configuration names and use `QUOTE_TTL=5s`. See `../v3/V3_1_GAP_ANALYSIS.md` for the current gap analysis.

---

## 0. Codex Starting Instructions and Implementation Principles

First read the repository's `AGENTS.md`, `README.md`, existing specifications, handoff documents, and this document. After completing Phase 0, implement the remaining phases in order. Do not stop after delivering only a plan.

1. **Incremental changes only.** Reuse the existing Bybit/Deribit adapters, order tracking, execution deduplication, persistence, recovery, and tests. If an existing dashboard can be reused, integrate into it rather than creating an unrelated backend.
2. **Testnet only.** New Web trading supports only the specified Spot products. Do not use real money, withdraw funds, transfer funds, borrow automatically, change leverage/margin/account settings, or use Mainnet as a fallback.
3. **Do not use credentials previously pasted in chat.** Examples and tests must use empty or synthetic data. Real credentials are configured by the user on the deployment host.
4. **Audit before modifying.** Do not overwrite uncommitted work, delete old data, or force renaming of existing binaries/CLI/environment variables.
5. **Trading truth belongs in the backend.** The frontend must not independently decide order quantity, price, venue, success state, or available funds, and must never hold exchange credentials.
6. **Distinguish three completion levels:** `IMPLEMENTED`, `LOCAL_VERIFIED`, and `TESTNET_VERIFIED`. If credentials, external network access, or deployment information are unavailable, mark the work `BLOCKED` or `NOT_RUN` and continue with work that can be completed locally.
7. **No automatic external side effects during development.** Tests, server startup, and page loading must not place orders. External order validation requires separate user authorization and explicit test flags. Deployment examples must not automatically modify real Nginx or firewall configuration.
8. At the end of each phase, run relevant tests and regressions for both venues, update `IMPLEMENTATION_STATUS.md`, and deliver a Traditional Chinese `CODEX_HANDOFF_V3.md` at the end.

Priority order: safety and data integrity > verified official protocol behavior / actual responses > requirements confirmed in this document > older proposals. If official behavior differs from documentation, preserve sanitized evidence and record the blocker. Do not silently substitute a different trade.

---

## 1. Confirmed Requirements and Scope

### 1.1 Decision List

| ID | Confirmed requirement | Implementation requirement |
|---|---|---|
| R01 | Application loads `.env` itself | User no longer needs to `source .env`; support an explicit file path |
| R02 | Web UI switches between Bybit / Deribit | Selection is per browser; it must not change one global "current venue" on the server |
| R03 | Home page displays account balance and assets | Large total-value area, asset list, available balance, data timestamp, and connection status |
| R04 | Asset valuation updates with market prices | Initial query + private WS + public price WS + backend valuation + Browser WS |
| R05 | Quick Trade modal on the same page | Input → Review → explicit Confirm → Waiting → Result; no separate trading page |
| R06 | Automatic refresh after execution | Modal balance area and underlying home page use the same account store and update together |
| R07 | Deribit BTC ↔ USDC | `BTC_USDC` Spot, one instrument with Buy/Sell directions; metadata, tick, amount step, minimum amount, contract-size fallback, and fee currency must come from that instrument's API |
| R08 | Bybit BTC/ETH ↔ USDT | `BTCUSDT` and `ETHUSDT` Spot, each with Buy/Sell directions |
| R09 | Limit IOC + 0.5% price protection | Do not change to raw Market order; do not guarantee full fill; 0.5% excludes fees |
| R10 | Simple login | One fixed username/password loaded from `.env` or OS environment; no user DB, registration, or password reset |
| R11 | Interview demo can be accessed externally | Through existing Nginx HTTPS; Go port is not directly public |
| R12 | Nginx and Go run on different hosts | Nginx → Go over private-network HTTP/WS; Go binds to configured private address and only trusts approved proxy sources |
| R13 | SSL already handled by Nginx | Do not add Go TLS certificate management; no `TLS_CERT_FILE` / `TLS_KEY_FILE` requirement |

### 1.2 Technical Direction

The new frontend uses **Vue 3 + TypeScript + Vite + Ant Design Vue**. If the existing frontend already uses a compatible architecture, extend it directly. Go remains responsible for API, authentication, trading, and push updates. Do not add a Node backend. Node is only a frontend build tool. Lock dependency versions based on the current project; do not use unpinned `latest` versions as the delivery baseline.

The UI should use English labels for interview presentation, such as `Account Balance`, `Quick Trade`, `Review Trade`, and `Confirm Trade`. Operational, deployment, and handoff documentation should remain in Traditional Chinese.

### 1.3 Out of Scope for This Version

Do not add a third exchange, automated arbitrage/strategy, cross-venue replacement orders, multi-hop conversion, withdrawals/transfers/subaccount creation, leveraged/perpetual Web order entry, full charting, order-book visualization, registration/OAuth/RBAC/multi-tenancy, native Go TLS, or reimplementation of FIX.

Preserve existing contract/FIX CLI features, but the Web must not expose a generic native exchange-order proxy. If a configured Spot instrument is unavailable, fail closed; do not silently route through another instrument or asset.

---

## 2. Phase 0: Baseline Audit and Compatibility

Create `docs/v3/BASELINE_AUDIT.md` and record the current commit, working tree, Go/frontend versions, and the following actual state:

| Area | Must verify |
|---|---|
| Entrypoints | Actual main package, CLI name, existing web/dashboard subcommands |
| Configuration | Existing env names, whether dotenv is already loaded, default account/category, semantics of key paths |
| Bybit | Spot REST, public Spot WS, wallet, order/execution, fee and balance queries |
| Deribit | Whether Spot products are already supported; V2 inverse-perpetual units must not be reused |
| Account | Actual account alias/account type; do not assume funds must be transferred to a subaccount; record the account identity returned by the API |
| Web | Existing routes, authentication, frontend framework, static asset serving, push updates, and CORS |
| Storage | Order intents, dedup/fees, schema, concurrent writes, startup recovery |
| Tests | Existing unit/integration/race/build results and pre-existing failures |

Baseline tests must not place/cancel orders or modify account settings. Recommended commands, adjusted to the actual structure:

```bash
git status --short
go list ./...
go test ./...
go vet ./...
go test -race ./...
```

In addition to existing regression protection, V3 must ensure:

- If only Bybit is configured, existing Bybit commands still work; same for Deribit-only configuration.
- Non-Web CLI commands must not fail because Web settings such as `WEB_PASSWORD` are missing.
- Existing data must not be interpreted as belonging to an empty account because new `venue`/`account` fields were introduced; migration needs versioning, backup, and rollback.
- Previously fixed fee behavior must remain correct: **execution quantity ≠ post-fee credited quantity**.
- Do not create a separate Web-only order reducer/reconciler that produces a different result for the same order than the CLI path.

---

## 3. Target Architecture and Responsibility Boundaries

```text
Browser (Vue)
    | HTTPS / WSS, one public origin
    v
Existing Nginx: TLS termination (Host A)
    | HTTP / WS, controlled private network
    v
Go Console (Host B, configured private IP:port)
    |
    +-- Auth / Sessions / CSRF / Trusted Proxy
    +-- Account Query + Account State
    +-- Pricing + Valuation
    +-- Quote + Quick Trade Application Service
    +-- Existing Intent / Order Tracker / Reconciler / Store
    +-- Browser WS Hub
    |
    +-- Bybit Adapter: REST + public/private WS
    +-- Deribit Adapter: HTTP JSON-RPC + public/private WS
```

### Must Be Shared vs Must Remain Isolated

**Shared:** domain model, account observation, order state, execution deduplication, fee model, recovery, persistence, and risk checks.
**Isolated:** venue/environment/account/product type/instrument metadata, rate limits, health, subscriptions, reconnect handling, and quote caches.

Suggested capability boundaries: `AccountSnapshotProvider`, `AccountEventSource`, `PriceProvider`, `SpotTradeCapacityProvider`, `SpotQuoteService`, `QuickTradeService`, `OrderTracker`. Do not restructure the entire project just to match these names.

Web handlers call application services; **they must not shell out to the CLI to place orders**. The Browser must never connect directly to exchanges and must never receive API keys, secrets, login tokens, or FIX credentials.

All exchange connections are maintained centrally by the backend. Opening another interviewer browser tab must not create a full additional set of exchange WebSockets or independent REST polling loops.

---

## 4. `.env` Loading and Configuration Contract

### 4.1 Precedence

```text
Existing explicit CLI flags (preserve current meaning; do not add secret flags)
    > OS environment
    > selected dotenv file
    > non-sensitive defaults
```

- By default, read only `.env` from the working directory. Add `--env-file /absolute/path/config.env`; exact flag placement should follow the existing CLI conventions and be documented.
- Support `--env-file .ENV` explicitly for uppercase filenames; Linux must not assume `.env` and `.ENV` are the same.
- If an explicitly specified file is missing/unreadable/malformed: startup fails. If the default `.env` is absent: OS env alone is allowed, but required settings must still be validated.
- An OS variable that exists but is empty still has precedence over file values; validation should then reject the required empty value. Do not silently fall back to an older password.
- Use a dotenv parser, not `sh -c`, `source`, or `eval`. Do not execute command substitution, recursively search parent directories for `.env`, or load dotenv automatically during package import.
- Reuse the existing loader if suitable. If adding one, `godotenv.Load` or explicit `Read`/merge is acceptable; do not use `Overload` to overwrite OS env. [C1]
- Document handling of quotes, `#`, whitespace, `$`, backslashes, and newlines. Passwords with special characters should use literal quoting supported by the parser; add tests. Dotenv is not a full shell script.
- `.env` is backend-only. Do not make repo root the frontend public directory, do not put secrets in `VITE_*`, and do not dump the entire environment/config to logs.
- Configuration is loaded at startup only. V3 does not implement hot reload. Restart after changing password/session secret. Unfinished trade intents must survive restart.

### 4.2 Web and Policy Settings

The following are the recommended canonical names. Existing synonymous settings may remain as aliases, but there must be only one effective source and the mapping must be documented.

| Setting | Default / requirement | Meaning |
|---|---|---|
| `WEB_HOST` | Required in deployment | Private interface IP on Go host; must not be the Nginx IP |
| `WEB_PORT` | `8080` | Not exposed directly to the Internet |
| `WEB_PUBLIC_ORIGIN` | Required HTTPS origin | e.g. `https://trade.example.com`; no path, userinfo, or query |
| `WEB_TRUSTED_PROXY_CIDRS` | Required | Actual Nginx source IP visible to Go, e.g. `10.0.0.10/32`; do not trust an entire broad network for convenience |
| `WEB_USERNAME` | Required | Single fixed login name |
| `WEB_PASSWORD` | Required, minimum 12 chars | Example remains empty; no default weak password |
| `WEB_SESSION_SECRET` | Required | Base64 encoding of at least 32 random bytes; validate decoded length |
| `WEB_SESSION_TTL` | `8h` | Absolute expiration; WS heartbeat must not extend indefinitely |
| `WEB_DEFAULT_VENUE` | `bybit` | Must be enabled; if offline, do not silently route orders to another venue |
| `WEB_TRADING_ENABLED` | `false` | Global Web write switch; requires explicit `true` to allow confirmed Web orders |
| `WEB_PUSH_INTERVAL` | `250ms` | Coalescing interval for valuation updates; must not delay order state |
| `ACCOUNT_RECONCILE_INTERVAL` | `30s` | One background snapshot scheduler per venue/account, with jitter/rate limiting |
| `QUOTE_TTL` | `5s` | Review quote validity; expired quote requires new review + confirmation |
| `TRADE_BOOK_MAX_AGE` | `3s` | Maximum executable-book age; controlled HTTP refresh may be used |
| `VALUATION_PRICE_MAX_AGE` | `15s` | After this age, stop presenting valuation as live; keep last value + marker |
| `QUICK_TRADE_SLIPPAGE_BPS` | `50` | Fixed maximum 50 bps = 0.5%; UI cannot raise it |
| `QUICK_TRADE_MAX_BTC` | `0.01` | Per-trade BTC source-asset spend cap, project policy |
| `QUICK_TRADE_MAX_ETH` | `1` | Per-trade ETH source-asset spend cap, project policy |
| `QUICK_TRADE_MAX_USDT` | `1000` | Per-trade USDT source-asset spend cap, project policy |
| `QUICK_TRADE_MAX_USDC` | `1000` | Per-trade USDC source-asset spend cap, project policy |

These time and amount values are **project defaults, not exchange limits, current market data, or measured latency**. Order submission is also subject to venue metadata, available balance, fees, and any stricter existing limits. Unset values must never mean unlimited.

`WEB_TRADING_ENABLED` controls only new Web write operations and must not silently alter existing CLI rules. A single shared login cannot distinguish owner from interviewer; when enabled, every authenticated user can execute the allowed Testnet trades. Set it to `false` for view-only demonstrations. This version does not add a second login or role system.

### 4.3 Example

```dotenv
# The IP/domain values below are examples only; replace them for real deployment.
WEB_HOST=10.0.0.20
WEB_PORT=8080
WEB_PUBLIC_ORIGIN=https://trade.example.com
WEB_TRUSTED_PROXY_CIDRS=10.0.0.10/32
WEB_USERNAME=alex
WEB_PASSWORD=
WEB_SESSION_SECRET=
WEB_SESSION_TTL=8h
WEB_DEFAULT_VENUE=bybit
WEB_TRADING_ENABLED=false

WEB_PUSH_INTERVAL=250ms
ACCOUNT_RECONCILE_INTERVAL=30s
QUOTE_TTL=5s
TRADE_BOOK_MAX_AGE=3s
VALUATION_PRICE_MAX_AGE=15s
QUICK_TRADE_SLIPPAGE_BPS=50
QUICK_TRADE_MAX_BTC=0.01
QUICK_TRADE_MAX_ETH=1
QUICK_TRADE_MAX_USDT=1000

QUICK_TRADE_MAX_USDC=1000

# V3.1: global rolling-hour/concurrency limits and per-venue source-asset caps.
DEMO_MAX_TRADES_PER_SESSION=10
DEMO_MAX_TRADES_PER_HOUR=30
DEMO_MAX_CONCURRENT_TRADES=1
DEMO_MAX_BYBIT_BTC_QTY=0.01
DEMO_MAX_BYBIT_ETH_QTY=1
DEMO_MAX_BYBIT_USDT_AMOUNT=1000
DEMO_MAX_DERIBIT_BTC_AMOUNT=0.01
DEMO_MAX_DERIBIT_USDC_AMOUNT=1000

# Preserve existing exchange configuration; do not overwrite the user's real file.
BYBIT_ENV=testnet
BYBIT_API_KEY=
BYBIT_API_SECRET=
BYBIT_WS_SPOT_URL=wss://stream-testnet.bybit.com/v5/public/spot
DERIBIT_ENABLED=true
DERIBIT_ENV=testnet
DERIBIT_API_KEY=
DERIBIT_API_SECRET=
```

`.env.example` must contain empty secrets only. Real `.env`, session files, state, keys, and raw private responses must not enter Git. Deployment credential files should be readable only by the service account.

---

## 5. Login, Sessions, and API Access

### 5.1 Interface

When unauthenticated, show only the login page and public static assets. Do not expose balances, venue account aliases, orders, market streams, or internal health details.

```text
Trading Console
Username   [________________]
Password   [________________]
           [ Sign In ]
```

Endpoints:

- `POST /api/auth/login`: JSON credentials; on success, Set-Cookie and return display name, CSRF token, expiration, and read-only state; never return the password.
- `GET /api/auth/me`: authenticated-session status; may reissue the session-bound CSRF token.
- `POST /api/auth/logout`: validate CSRF, revoke the session immediately, clear the cookie, and close Browser WS connections belonging to that session.

### 5.2 Session Design

Use existing reliable session middleware or a bounded server-side session store built with standard cryptographic primitives. Recommended: random opaque ID + signature using `WEB_SESSION_SECRET`. Do not invent a custom encryption algorithm.

- Cookie name: `__Host-trading_session`; `Secure=true`, `HttpOnly=true`, `SameSite=Strict`, `Path=/`, no Domain. [S1]
- Fixed absolute 8h expiration. Server restart may invalidate login sessions, but trade intents/tracking must survive.
- Regenerate session ID after login to prevent fixation. Logout and expiration must take effect immediately on the backend.
- Do not store password, bearer session tokens, or exchange tokens in localStorage or URL queries.
- Password comes from `.env` as required by the user. Compare using a mature password verifier, or constant-time comparison of fixed-length derived values. Do not log candidate passwords. Do not describe plain SHA-256 as an adequate slow password hash for persistent password storage.
- Account-not-found and wrong-password responses must be indistinguishable. Bound request-body size and log only sanitized login-failure metadata.

Default brute-force protection: for each trusted client IP, after 5 failures within 5 minutes, cool down for 30 seconds and return `429` + `Retry-After`. Also add a bounded global limiter to protect resources from distributed attempts, but do not permanently lock the shared account.

### 5.3 CSRF, Origin, and Authorization

- Validate session in the backend for every protected HTTP/WS endpoint; do not rely only on Vue routing.
- All write requests, including quote creation, confirm, refresh, and logout, must validate an **exactly matching** `Origin` and CSRF token. Never use suffix/contains matching for domains. [S2]
- Login itself must validate Origin, require JSON and correct Content-Type, and prevent login CSRF. Browser write APIs with no Origin should be rejected by default.
- CSRF token is bound to the session and submitted via `X-CSRF-Token`. `SameSite` is additional protection, not the sole defense.
- Do not enable cross-origin CORS. Browser API and WS use one public origin.
- WS handshake validates session, Origin, Host, and trusted proxy. Do not place tokens in WS URLs. [S3]
- Quotes/intents belong to the authenticated identity and allowed account scope. Users cannot modify URLs to operate on another venue/account.
- Enforce `WEB_TRADING_ENABLED=false` in the backend; manipulating frontend buttons must not permit trading.
- Nginx ACL does not replace application authentication. Even requests from the trusted proxy must authenticate before reading assets or trading.

### 5.4 Responses and Static Assets

Private APIs use `Cache-Control: no-store`. Never expose `.env`, `.git`, PEM files, state, logs, or sensitive source maps. API 404s must not fall back to SPA index and appear successful. Bundle frontend fonts/icons locally where practical rather than relying on external CDNs.

Apply a workable CSP, `frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, and similar protections. Test that Ant Design Vue still renders correctly. Do not "fix" UI issues by disabling CSP entirely.

---

## 6. Home Page: Account Balance and Venue Switching

### 6.1 Visual Hierarchy and Layout

Use the user's account screenshot as an information-hierarchy reference: dark background, large total-value card, currency table, right-aligned numbers. **Do not copy screenshot balances as real data, do not display fictitious APR, and do not add Deposit/Withdraw actions.**

Top bar includes product name, `Bybit / Deribit` selector, `TESTNET` badge, account alias, public-market/private-event status, refresh, and logout. Primary action: `Quick Trade`.

Table must include at least:

| Field | Meaning |
|---|---|
| Asset | Asset name/code; use letter icon if no icon is available |
| Balance | Exchange-reported asset quantity / cash balance |
| Equity | Exchange equity; may include PnL and must not always be treated as tradeable quantity |
| Value (USD) | Row valuation with source/freshness; show `—` when unpriced |
| Available to Trade | Amount validated by this application for the supported Spot route; show `—` when unknown |
| Status / action | Live / Snapshot / Stale / Unpriced; offer Quick Trade only for supported directions |

If a reliable withdrawable field already exists, it may be displayed as additional information, but **do not rename Available to Trade to Withdrawable merely to mimic the screenshot**.

Keep unknown/unsupported non-zero assets and liabilities visible. A "hide zero balances" toggle is allowed, but negative balances, borrowings, and anomalous items must not be hidden by a generic "assets only" filter.

### 6.2 Switching Rules

- Each browser independently remembers venue selection. localStorage may store only UI preference, never credentials or financial snapshots.
- On switch, cancel old HTTP requests or use request-generation guards; a late Bybit response must not overwrite the Deribit screen.
- Quotes and modals are bound to their original venue/account. Switching during EDIT/REVIEW closes or clears the quote; after SUBMITTING begins, the modal's venue cannot change.
- Backend tracking must continue even when the UI switches away. An in-progress modal may be collapsed, but the home page must keep a notification entry so the trade can be reopened.
- Failure of one venue only marks that venue unavailable/stale. Preserve last-known data + timestamp; never interpret failure as zero balance and never bring down the other venue.

### 6.3 Data Types

All API prices, quantities, fees, and monetary values must be **decimal strings**. Timestamps use UTC RFC3339 or explicitly named millisecond fields. Unknown values use `null` plus a reason, not `0`. The Browser formats display only and must not use JavaScript `Number` for order or accounting calculations.

---

## 7. Account Synchronization and Available Funds

### 7.1 Initial Snapshot and WS Bridging

After starting a venue/account:

1. Validate environment, account identity, and read scope.
2. Establish private WS and subscribe; begin buffering events and record connection generation.
3. Fetch account snapshot, required orders/executions, and available-funds data.
4. Merge snapshot and buffered events according to venue time/revision/semantics; do not create a gap by fetching REST first and subscribing afterward.
5. Re-query data that cannot be ordered or conflicts. Enter Ready only after required recovery is complete.

Private events must never be silently dropped. If bounded buffers overflow, disconnect, or gaps occur, mark the venue Recovering/Degraded, block new Web trades, and reconcile. The UI may continue showing the last snapshot and its age.

Periodic snapshot, user refresh, post-trade refresh, and reconnect reconciliation must share/coalesce scheduling. **At most one recovery/refresh per account at a time.** Do not let every browser window independently call exchange APIs.

### 7.2 Bybit

- Initial asset snapshot uses `GET /v5/account/wallet-balance`, with account type based on the existing account mode; V3 expects UNIFIED. Funding wallet is outside this default total and must be identified as a different account scope in UI. [B1]
- Subscribe to private `wallet`; preserve existing `order`/`execution` subscriptions if they already include Spot, otherwise add Spot coverage without duplicate subscriptions and duplicate processing.
- **The wallet subscription has no initial snapshot, and unrealised PnL changes do not trigger wallet events.** Therefore it cannot be the only source for live USD valuation. [B2]
- Keep raw semantics for balance/equity/locked/borrow separate. Do not use deprecated fields or parse empty strings as real zero values.
- During Review/Confirm, use the current Spot trade-capacity query. In `/v5/order/spot-borrow-check`, `spotMaxTradeQty` / `spotMaxTradeAmount` exclude borrowed capacity. Do not treat `maxTradeQty` / `maxTradeAmount`, which include borrowable capacity, as actual Spot holdings. [B6]
- Explicitly use `category=spot`, `isLeverage=0`. If funds are in Funding instead of the trading account, show a scope warning; do not transfer automatically.
- Clearly distinguish full snapshots from partial coin updates. A partial message that omits ETH does not mean ETH becomes zero. If a verified complete-nonzero listing omits a known asset, handle according to verified endpoint semantics or query the coin explicitly.

### 7.3 Deribit

- Prefer `private/get_account_summaries` for account data. `extended=true` may be used for startup diagnostics. Parse `result.summaries`; do not treat the result object as an array and do not use `get_positions` as the wallet-balance source. [D1]
- Map returned account identity to the UI alias. There is no requirement to create or transfer into a subaccount. Display only the account actually connected by the API key; do not automatically aggregate other accounts through main-account privilege.
- `user.portfolio.any` or verified per-currency portfolio subscriptions provide private account updates. Dynamically added assets should update subscription/valuation coverage. Assets without event support remain visible through controlled snapshots. [D2]
- Preserve or extend Spot-capable `user.orders` / `user.trades` / `user.changes` subscriptions for Spot orders/executions. Verify names against current docs and actual subscription ACK; do not blindly copy placeholders from documentation. [D8]
- `available_funds` is margin-availability information and **must not automatically be treated as Spot-spendable currency balance**. Under cross-collateral, some aggregate values are expressed across currencies and must not be added row-by-row. [D1][D2]
- `availableToTrade` must use validated Spot-specific rules including actual holdings, Spot reserve, locked amounts, this application's reservations, and account restrictions. If there is no reliable formula/precheck, block order submission with an explicit reason rather than assuming all collateral is borrowable.
- V3 must not assume there are no legacy derivatives positions just because the page displays Spot. Preserve raw account margin model, liabilities, margin usage, and unsupported risk information for diagnostics.

### 7.4 Balance Must Not Be Permanently Updated by Simply Adding Trade Results

Executions may produce expected asset changes for validation and result display, but the authoritative account store is still updated from exchange wallet/portfolio/snapshot data. Do not subtract a fill once and then subtract again when the wallet update arrives.

After submission, separately track: order terminal state, completeness of fee/execution details, and account synchronization. Do not treat HTTP success or a later local receive timestamp as proof that the account balance already reflects the trade.

---

## 8. USD Valuation: Fast, but Not Pretending to Be Full Exchange Equity

### 8.1 Two Numbers Must Remain Distinct

1. **Exchange Equity (USD):** account equity reported by the exchange, with `asOf`; do not reinterpret it.
2. **Estimated Asset Value (USD):** local live valuation = asset quantity × verified price, marked `Estimated / ≈`; this is not a liquidation/margin engine.

If a venue, such as a Deribit multi-currency account summary, does not report one aggregate USD total, the UI must retain the Exchange-reported total field and display `Not reported by venue`. It must not hide the gap or substitute a local valuation or per-currency conversion as an exchange-reported total.

For a pure Spot account with no liabilities and complete pricing coverage, the main card may show `Total Account Value — Estimated` and update with market prices while still displaying the exchange snapshot nearby. If derivatives, options, borrowings, or incomplete pricing exist, use a label such as "Priced Asset Value / Subtotal" instead of claiming full-account value.

**Do not calculate `equity(snapshot) × latest coin price` and call it a precise derivatives-equity reconstruction. Do not add UPL twice. Do not sum Deribit cross-collateral totals that are repeated by currency.** [B1][D1]

### 8.2 Price Sources

For every price store `asset`, `price`, `quoteCurrency`, `sourceVenue`, `sourceKind`, `sourceInstrument`, `exchangeAsOf`, `receivedAt`, and `quality`.

Valuation source priority:

1. Verified same-environment official USD index / asset-price stream.
2. Same-environment Spot book mid, then traceable FX conversion with source and timestamp.
3. If no live path exists, retain exchange-provided USD snapshot value and mark it `Snapshot`; do not pretend it is live.
4. If still unpriced, display `Unpriced`, retain the asset row, and mark totals Partial/priced subtotal.

`BTCUSDT` is quoted in USDT and **must not automatically be treated as BTC/USD**. USDT, USDC, USDe, etc. must not be hard-coded to exactly 1 USD. Multi-hop conversion is allowed only for valuation, maximum two legs, with traceable sources. This is not a multi-hop trading feature. Do not mix data from different environments/venues without labeling it.

For Bybit public Spot use the appropriate endpoint's `tickers.{symbol}` / orderbook; for Deribit use verified ticker/index subscriptions plus metadata. Do not use Linear execution prices as Spot executable prices. [B7][D3]

### 8.3 Real-Time Processing and Staleness

- Revalue whenever quantity or price changes. Asset valuation must continue to move with valid prices even without wallet events.
- Coalesce valuation updates every 250ms so every tick does not redraw the full table. Terminal order state and health changes take priority.
- Track freshness per asset. On expiry, retain the last value and timestamp, stop pretending it is live, and never replace unknown with zero.
- Browser `Live` means backend data-quality rules pass, not merely that the Browser WS is connected.
- Before comparing local valuation to exchange value, ensure scope, valuation basis, and timestamps are compatible; differences do not automatically imply missing funds.

V3 does not require every token in the screenshot to have a live feed. It requires every balance to remain visible, each value's semantics to be correct, and major tradable assets to update live when valid market data exists.

---

## 9. Spot Quick Trade: Fixed Directions and Native Mapping

| Route ID | UI direction | Native instrument | Action | Order quantity unit |
|---|---|---|---|---|
| `deribit-usdc-btc` | USDC → BTC | `BTC_USDC`, Spot | `private/buy` | BTC (base), derived from the USDC spend cap |
| `deribit-btc-usdc` | BTC → USDC | `BTC_USDC`, Spot | `private/sell` | BTC (base) |
| `bybit-usdt-btc` | USDT → BTC | `BTCUSDT`, Spot | Buy | BTC (base), derived from the USDT spend cap |
| `bybit-btc-usdt` | BTC → USDT | `BTCUSDT`, Spot | Sell | BTC (base) |
| `bybit-usdt-eth` | USDT → ETH | `ETHUSDT`, Spot | Buy | ETH (base), derived from the USDT spend cap |
| `bybit-eth-usdt` | ETH → USDT | `ETHUSDT`, Spot | Sell | ETH (base) |

Every direction must map to Buy or Sell on the same real venue instrument. Do not invent a reverse symbol or synthesize a missing order-book side. [B3][D3][D4][D7]

### 9.1 Metadata Gate

At startup and after a reasonable TTL, query product metadata: kind/category, base/quote, active state, price tick, quantity precision/step, minimum notional, maximum limit quantity, fee/rule source, and current trading limits. [B4][D3]

V3 must add **Deribit Spot capability** and must not reuse the USD-notional rules from `BTC-PERPETUAL`. Preserve V2 contract support. Allowlist keys are `(venue, productType, instrument)`.

Deribit Spot may be marked in metadata as `is_cbe_routed` / `is_csr`. This does not mean the project has gained a Coinbase adapter. Validate the required order/report semantics based on returned flags; do not assume all Spot orders synchronously return fills and do not require the user to configure Coinbase credentials. [D5]

Deribit `public/get_instrument` does not provide a separate pre-trade `fee_currency` in Spot metadata. The Review fee estimate must use the `quote_currency` and `taker_commission` returned by that instrument API, never a currency inherited from another instrument. After execution, each execution's `fee_currency` and `fee` from `private/get_user_trades_by_order` are authoritative and replace the estimate. Fail closed if these values are missing or cannot be mapped.

If the instrument is missing, inactive, metadata is unclear, IOC unsupported, liquidity unavailable, or required scope missing, list the route but disable it and show the reason. Do not change product, increase size, switch environment, or change to GTC merely to make the demo succeed.

### 9.2 Input Semantics

User input means **Spend up to X of the From asset**, including fees that may be charged in the From asset. It does not mean "guarantee the full amount is spent" or "guarantee a specific net received quantity."

The form shows From/To, available amount, Amount, reference rate, estimated receive, fee status, and price protection. Optional 25%/50%/Max shortcuts are allowed, but Max must be capped by per-trade limit, fee reserve, precision, and actual Spot capacity; never fill in total equity directly.

Accept only positive bounded decimal strings. Reject `NaN`, Infinity, negative numbers, zero, scientific notation, and excessively long input. If final quantity falls below exchange minimums, return a human-readable error.

---

## 10. Quote, Price Protection, Quantity, and Fees

### 10.1 A Quote Is Not an Executable Guarantee

The backend fully computes and stores the quote. It includes identity, venue, environment, account, route, native instrument/direction, spend cap, actual base quantity, limit price, TIF, metadata revision, book observation, balance version, fee estimate, and expiry.

The Browser sends only route + input amount. Confirm sends only `quoteId` + `clientRequestId`. The Browser must not be allowed to overwrite price/side/account/fee/slippage and have those accepted as real trading parameters.

Quotes use the latest valid **executable bid/ask and depth**, not last trade, index, or valuation price. Insufficient depth may warn about partial fill but must not estimate fills beyond the protected range.

### 10.2 Price Formula

Let `s=0.005`. `floorTick` / `ceilTick` mean rounding to a valid price on the instrument's effective tick ladder.

```text
Buy:
    limitPrice = floorTick(bestAsk × (1 + s))

Sell:
    limitPrice = ceilTick(bestBid × (1 - s))
```

**Buy limit rounds down to a valid tick; sell limit rounds up**, so rounding never pushes execution beyond the user's 0.5% boundary. If no valid tick remains marketable inside the boundary, reject the quote instead of adding one more tick.

The limit and worst price must be frozen and displayed during Review. At Confirm, revalidate data and constraints, but **do not recompute a worse limit from a new best bid/ask**. If the quote expires or quantity/price/fee cap must change, return to Review and require explicit reconfirmation.

0.5% is execution-price protection relative to the direction's best ask/bid at quote time. It **does not include bid-ask spread, trading fees, FX movement, cross-venue risk differences, and does not guarantee full fill**.

### 10.3 Spend Cap and Quantity

Use decimal arithmetic throughout. Round order quantity down to valid quantity steps; never treat quote-currency input as base quantity.

```text
Buy (From = quote asset):
    find the largest valid baseQty such that
    baseQty × buyLimitPrice + maximumSourceAssetFee(baseQty) <= SpendBudget

Sell (From = base asset):
    find the largest valid baseQty such that
    baseQty + maximumSourceAssetFee(baseQty) <= SpendBudget
```

Also validate non-borrowed available funds, this application's reservations, venue capacity, native maximum quantity, and minimum notional. If fees may be charged in the To asset, subtract them from estimated net received. If charged in a third asset, validate capacity for that asset and display it separately in Review.

**Illustrative example only; not a market-price or fee commitment:** Ask = 100,000 USDT/BTC, budget = 1,000 USDT, tick = 0.1, qty step = 0.00001, fee charged from received BTC. Limit price = 100,500, baseQty = 0.00995, worst gross spend = 999.975 USDT. Even on full fill, unused budget may remain. Do not display "spent 1,000" or automatically submit a top-up order.

### 10.4 Fee Policy

- Prefer account/instrument fee-rate APIs and verified fee models. Do not hard-code 0%, 0.1%, or a previous Testnet 1% rate. [B8]
- Store fee rate, fee currency, and source in the quote. If the fee model cannot be verified sufficiently to enforce the spend cap, allow preview but block Confirm with `FEE_MODEL_UNAVAILABLE`; unknown fee must never display as zero.
- If fees may be charged additionally from the source asset or a third asset, maintain a verifiable upper-bound reserve. Displaying an estimate does not replace spend protection.
- After execution, use actual per-execution fee/currency. Aggregation may include multiple currencies, discounts, rebates, or extra fees. Do not add fee quantities across different currencies. [B5]
- UI displays gross fill, gross paid/received, fees by asset, net received, and actual source debit separately.
- If the same execution arrives through REST/RPC response, WS, and reconciliation, account for it once. Do not add order cumulative quantity and individual fills together.
- If order status is Filled but fills/fees are still incomplete, show `Filled — syncing trade details`; do not fabricate net receive.

### 10.5 Native Order Settings

Bybit: `category=spot`, `orderType=Limit`, `timeInForce=IOC`, `isLeverage=0`, `orderFilter=Order`; `qty` is base units. `marketUnit` is a Spot market-order option and must not be used to turn this version's limit order into a quote-amount order. [B3]

Deribit: `type=limit`, `time_in_force=immediate_or_cancel`, `post_only=false`, using base amount and base/quote price validated against Spot metadata; do not send inverse-contract USD notional. [D4]

When exchange risk controls reject an order, display a sanitized code/reason. **Do not relax 0.5%, switch to raw Market, route elsewhere, split into two legs, or automatically retry.**

---

## 11. Modal, Confirmation, and Trade State Machine

### 11.1 Visual Flow

```text
EDIT -> REVIEW -> SUBMITTING -> WAITING / RECOVERING -> RESULT
```

**EDIT:** venue, direction, source balance, spend amount, estimated rate, Review button.
**REVIEW:** fixed quote spend cap, baseQty, estimated gross/net receive, fees, limit/0.5% protection, IOC partial-fill warning, quote-expiry countdown; Back/Confirm.
**SUBMITTING:** disable duplicate submission; never retry invisibly in the background.
**WAITING:** show persisted intent, submitted/accepted state, known fill amount, and current wait state; animations must not pretend a response has arrived.
**RESULT:** show actual terminal outcome: filled, partial, unfilled cancellation, rejection, or unresolved outcome.

Pressing Enter, double-clicking, refreshing, or closing the modal must never create a second order. Closing the modal does not cancel the trade. Reopening it must show the existing intent state.

### 11.2 Keep Business State Separate

Reuse V2 `ExchangeState` and `CommandState`, with an additional Web-result view. Do not turn every transient UI state into an exchange state.

| Web result | Condition | UI meaning |
|---|---|---|
| `FILLED` | Native submitted quantity fully filled | Trade filled; budget may still remain because of rounding/price improvement |
| `PARTIAL_CANCELLED` | 0 < filled < submitted qty and remainder cancelled | Partially filled; show actual conversion and unused source budget |
| `CANCELLED_NO_FILL` | Terminal cancel with zero execution | No fill within protection limit; this is not a successful conversion |
| `REJECTED` | Explicit exchange rejection / validation failure definitely before submission | Show reason; do not fake-update balances |
| `OUTCOME_UNKNOWN` | Timeout/disconnect/crash means acceptance cannot be proven | Checking order status; must not be treated as safe-to-retry failure |

A UI waiting timeout (recommended 15s) should only change copy to "taking longer than expected; reconciling". It must not cancel or retry. Backend performs bounded queries and continues private-event tracking. If still unresolved, show NeedsReview and keep reservations/blocking.

Deribit RPC may include `order`/`trades` immediately and should be merged at once. Bybit ACK is not terminal. **Do not force an already-filled order to continue "waiting" for animation, and do not treat a no-fill ACK as execution.** [B3][D4][D5]

### 11.3 Result Display

At minimum show venue, route, IntentID, native OrderID, submitted base quantity, actual gross fills, weighted average price, actual source debit, net destination received, fees by currency, unused budget, timestamps, and data completeness.

`Again` must create a fresh quote and require a new confirmation, and only after the previous intent result/funds are understood. Never replay the prior POST.

### 11.4 Automatic Balance Update in Modal and Underlying Page

- When opening the modal, save a timestamped before-account snapshot for comparison.
- On execution/terminal events, trigger backend account refresh/reconciliation while public valuation continues updating.
- Modal `Current Balance` and background home page subscribe to the same venue/account store rather than maintaining two independent copies.
- If the trade is complete but wallet state has not caught up: show `Trade filled — updating balance`; retain the last balance with syncing status instead of inventing an after-balance.
- Update to synchronized only after a reliable snapshot/wallet update is confirmed and unresolved discrepancies are reconciled. A later receive timestamp alone does not prove the update includes this trade.
- If multiple logged-in users, CLI, or the exchange website can trade simultaneously, Before/After are account observations, not proof that all delta belongs to this trade. Actual per-trade asset changes are separately computed from deduplicated fills.
- Balances may update while fees are still incomplete, but net trade result remains pending; neither should pretend to be the other.

---

## 12. Confirm Submission, Idempotency, and Persistence

### 12.1 Single Confirmed Trade

Confirm runs within the account writer/store transaction:

1. Validate login, CSRF, allowlist, `WEB_TRADING_ENABLED`, quote ownership/expiry.
2. Check whether `clientRequestId` or quote already has an intent. Same content returns the existing intent; different content reusing request ID returns `409`.
3. Revalidate venue health, private recovery completeness, book quality, metadata, price protection, non-borrowed funds, fees, and spend limits.
4. Atomically consume quote, persist immutable intent, create reservation, and store request→intent mapping before allowing network send.
5. Before network send, persist a state meaning "may have been submitted"; from that point onward, timeout/crash is not treated as definite non-submission.
6. Send exactly once over the selected transport and hand result to the existing tracker/reducer. Browser HTTP disconnect must not cancel backend tracking.

The same quote must not be submitted again even with another `clientRequestId`. Idempotency is based on authenticated identity + request ID and unique quote consumption, not only on memory mutexes, disabled buttons, or ephemeral session IDs.

### 12.2 Timeout and Disconnect

If the Browser loses the Confirm response, keep the original clientRequestId and query/retry the same request to retrieve the existing result; **do not generate a new ID and place another order**. An earlier HTTP 202, later WS final event, and GET lookup may arrive in any order and must merge into the same intent.

Bybit `orderLinkId` / Deribit `label` are correlation identifiers, not cross-venue exactly-once guarantees. Deribit label may map to multiple orders. [D6]

For unknown outcome: query native OrderID first; if needed use label/client ID + parameters, order history, and executions. Failure to find it once does not prove it was never accepted. Never automatically re-submit across REST/WS/FIX or another venue.

### 12.3 Reservation and Multiple Clients

Per-account available funds must first subtract this application's reservations to prevent two browsers from spending the same balance. V3 may conservatively allow only one new Quick Trade intent per account at a time and return `ACCOUNT_BUSY` otherwise; this must not block another venue.

Private events/RPC responses/HTTP snapshots must not independently release/deduct reservations multiple times. Release only when submission is proven not to have occurred, or after terminal state and balance reconciliation. Unknown outcome must not release because the UI timed out.

If Web and CLI share storage, use the existing single-writer or cross-process lock. If the current store is not safe for multiple writers, explicitly prevent simultaneous CLI writes and return a clear error rather than pretending atomic rename provides cross-process transactional safety.

### 12.4 Must Persist

Persist: confirmed quote copy, IntentID, clientRequestId, quote consumption, venue/env/account/route, native params, send-attempt state, NativeOrderID, fill/fee dedup keys, result, reservations, recovery cursor, audit references.

Unconfirmed quotes may expire in memory. **Confirmed intents must survive restart.** Server restart may invalidate login sessions but must not erase order recovery state; after re-login, users can retrieve unfinished/recent intents.

---

## 13. HTTP API Contract

Use `/api` as same-origin prefix unless the existing project already uses versioned routes; if so, preserve them and document mapping. All routes except login require authentication, and all POSTs require §5 protections.

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/auth/login` | Fixed credential login |
| GET | `/api/auth/me` | Session, CSRF, non-secret config summary |
| POST | `/api/auth/logout` | Revoke session |
| GET | `/api/venues` | Supported venue, account alias, health, read/trade capability |
| GET | `/api/venues/{venue}/account` | Cached normalized account snapshot, valuation, quality |
| POST | `/api/venues/{venue}/account/refresh` | Trigger coalesced controlled refresh rather than immediate duplicate exchange calls |
| GET | `/api/venues/{venue}/quick-trades` | Fixed routes, limits, availability, reasons |
| POST | `/api/venues/{venue}/quotes` | Build a Review quote from routeId/amount; no order |
| POST | `/api/venues/{venue}/trades` | Confirm one intent using quoteId/clientRequestId |
| GET | `/api/venues/{venue}/trades/{intentId}` | Status, fills/fees, synchronization state |
| GET | `/api/venues/{venue}/trades` | Bounded recent/pending list; support clientRequestId lookup |
| GET | `/api/ws` | Authenticated WebSocket upgrade for backend account/valuation/order updates |

Create-quote example:

```json
{
  "routeId": "bybit-usdt-btc",
  "amount": "1000"
}
```

Confirm example:

```json
{
  "quoteId": "q_<opaque-id>",
  "clientRequestId": "req_<browser-generated-random-id>"
}
```

After a new intent is durably stored, return `202` with `intentId` and the currently known state. `202` is not proof of execution. Duplicate Confirm returns the existing resource and must not create a second side effect.

Error envelope:

```json
{
  "error": {
    "code": "QUOTE_EXPIRED",
    "message": "Quote expired. Review a fresh quote before confirming.",
    "retryAction": "REQUOTE",
    "requestId": "r_<opaque-id>"
  }
}
```

Required error codes: `AUTH_REQUIRED`, `FORBIDDEN`, `CSRF_INVALID`, `UNTRUSTED_PROXY`, `READ_ONLY`, `INVALID_AMOUNT`, `UNSUPPORTED_ROUTE`, `MARKET_UNAVAILABLE`, `STALE_MARKET_DATA`, `INSUFFICIENT_SPOT_BALANCE`, `TRADE_LIMIT_EXCEEDED`, `FEE_MODEL_UNAVAILABLE`, `QUOTE_EXPIRED`, `QUOTE_CHANGED`, `IDEMPOTENCY_CONFLICT`, `ACCOUNT_BUSY`, `VENUE_RECOVERING`, `OUTCOME_UNKNOWN`.

Unknown outcome is the state of an existing intent. Do not return a generic 500 that implies it is safe to place another order. Keep HTTP status distinct from exchange business codes.

---

## 14. Browser WebSocket and Frontend Store

### 14.1 Data Flow

The Browser first uses HTTP for session/venues. After login, establish same-origin WSS and subscribe to the selected venue's account plus intents visible to that identity. To avoid an initialization gap, when subscription is processed the WS server must first emit a complete UI snapshot from the same ordered state source, then subsequent updates. A cached account fetched over HTTP is only for initial display and must not overwrite a newer WS revision.

Events:

```text
snapshot
account.updated
valuation.updated
trade.updated
venue.health.updated
resync.required
session.expiring
```

Each envelope contains `schemaVersion`, `instanceId`, `seq`, `type`, `venue`, `accountAlias`, `stateRevision`, `sentAt`, and `payload`. `seq` increments continuously within one Browser stream. Valuation coalescing happens before assigning `seq`, so coalescing does not create fake gaps.

On disconnect / changed instanceId / sequence gap: stop treating data as Live and resubscribe for snapshot + pending intents. Late events from an old generation must not overwrite new data.

### 14.2 Control and Backpressure

- Browser WS only accepts bounded subscribe/unsubscribe/resync control messages. **It must not accept order-submission messages**; orders use CSRF-protected HTTP.
- Maintain application/control heartbeat within 30s; Nginx timeout should be longer. Login expiry/logout closes the socket without requiring page refresh. [S3][N1]
- Limit connections per session (default 5) and global clients/message size/queue.
- Slow clients may receive only latest valuation snapshot, but critical order/account events must not be silently dropped. If they cannot be delivered, disconnect and require resync rather than blocking exchange event processing.
- Push normalized data only; never forward raw auth/private responses or sensitive account fields.

### 14.3 Frontend Implementation

Use one normalized store keyed at least by venue/environment/account. Home page, modal balance, and recent trades all use it. Preserve original decimal strings in the store and format only for display. USD may be shown to two decimals where appropriate; asset quantities follow precision. Copy actions should preserve necessary precision.

The modal must support keyboard interaction, visible focus, narrow screens, and loading/empty/error/stale states. Do not use only red/green color to convey status. Submission may be collapsed but not cancelled; a top-level trade notification should reopen it. Long OrderIDs must not break layout.

---

## 15. Split-Host Nginx and Go Deployment

### 15.1 Actual Topology

```text
User / interviewer Browser
        |
 HTTPS + WSS, public domain
        |
Nginx Host A, e.g. 10.0.0.10 (existing SSL)
        |
 controlled private LAN / VLAN or encrypted private tunnel
        | HTTP + WS
Go Host B, e.g. 10.0.0.20:8080
```

All IP/domain values are examples; real deployment values come from configuration. **Go does not bind `127.0.0.1`; the Nginx upstream is not `127.0.0.1:8080`.** Do not add native Go TLS or move the existing certificate flow.

The Nginx→Go segment is unencrypted HTTP. This is acceptable only on a controlled trusted private network. If the path crosses an untrusted network, it must use VPN/encrypted private tunnel. External SSL termination does not make a publicly exposed backend HTTP port safe.

### 15.2 Network and Proxy Trust

- Go binds the configured `WEB_HOST` private IP. Use `0.0.0.0` only through an explicit operator choice and still restrict via firewall.
- Backend inbound ACL must allow only the **actual source IP seen from Nginx**, including IPv4/IPv6/container/NAT considerations. Do not blindly copy example IPs.
- ACL is a network boundary, not a login replacement. All private APIs still require sessions.
- Backend trusts forwarding headers only when `RemoteAddr` falls within `WEB_TRUSTED_PROXY_CIDRS`; in this deployment mode, untrusted direct Web requests are rejected and self-declared XFF is ignored. [N2]
- V3 assumes **one edge Nginx**. Nginx overwrites XFF with one validated client IP and does not preserve arbitrary inbound chains from the Internet. Go validates format and does not blindly take the first header value.
- Expected Origin/Host derives from `WEB_PUBLIC_ORIGIN`, not user-supplied Host.
- If a CDN/second proxy is added later, introduce an explicit trusted-chain design rather than trusting all `X-Forwarded-*` by default.

### 15.3 Nginx Example Delivery

The user already has SSL. This package provides an `http`-scope map and a **location snippet to be inserted into the existing HTTPS server**, not a conflicting new `server_name` / certificate config. [N1][N3]

`http` context:

```nginx
map $http_upgrade $console_connection_upgrade {
    default upgrade;
    ''      close;
}
```

Inside the existing HTTPS server:

```nginx
# Replace domain and upstream IP with actual values.
# Do not cache private API responses.
location / {
    proxy_pass http://10.0.0.20:8080;
    proxy_http_version 1.1;

    proxy_set_header Host trade.example.com;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-Host trade.example.com;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header Forwarded "";

    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $console_connection_upgrade;

    proxy_connect_timeout 5s;
    proxy_read_timeout 90s;
    proxy_send_timeout 90s;
    proxy_buffering off;
    proxy_cache off;
    proxy_next_upstream off;
    client_max_body_size 64k;
}
```

Also add suitable long-lived timeout/proxy buffering for `/api/ws` and reject sensitive dotfiles as documented in the accompanying example. Ensure websocket headers, Host, and forwarding headers are configured in the actually matched location and are not lost because of Nginx `proxy_set_header` inheritance rules. [N3]

`proxy_next_upstream off` prevents the reverse proxy from automatically trying another backend for a trade request, but it does not replace application idempotency. Preserve existing external HTTP→HTTPS redirect, default-vhost unknown-Host rejection, and certificate settings.

### 15.4 Cookies and External URL

Because the Browser sees HTTPS, Go still sets `Secure` cookies even though upstream traffic from Nginx is HTTP. Determine this from `WEB_PUBLIC_ORIGIN` and trusted-proxy policy, not only `r.TLS != nil`.

Browser APIs use relative paths. Browser WS uses public-origin `wss://.../api/ws`; never expose `10.0.0.20` to Browser or hard-code `ws://` and create mixed content. [S1][S3]

### 15.5 Deployment Acceptance

1. Nginx host can connect to the Go port; other unauthorized hosts cannot.
2. External login works over HTTPS; cookie is Secure/HttpOnly/SameSite with no Domain.
3. Reverse-proxied `/api/ws` returns `101`, stays connected, and closes on logout/expiry.
4. Modified Origin/Host/forwarded IP cannot bypass authorization or rate limits.
5. `/.env`, `/.ENV`, `/.git/config`, state/key files return 403/404 and are never exposed through SPA fallback.
6. Authenticated APIs are not proxy-cached; unauthenticated asset queries still return 401.
7. Only after actually running `nginx -t` should an operator reload Nginx. If Codex lacks machine/certificate access, mark NOT_RUN rather than claiming deployment success.

---

## 16. Build, Startup, and Delivery

### 16.1 Build Compatibility

Provide a `make build` or equivalent entrypoint that: installs locked frontend dependencies → typechecks/builds frontend → copies output into the actual embed directory → produces an explicit Go binary.

Existing CLI `go test ./...` and CLI-only builds must not fail merely because `dist/` has not been generated. A `webui` build tag may separate real embedded assets from a no-UI stub. **A full Web binary that lacks assets must fail clearly, not silently serve a fake page.**

Candidate commands below should only be used if the repository contains these paths; otherwise update to the actual structure:

```bash
# Development
npm --prefix web ci
npm --prefix web run typecheck
npm --prefix web run build

# make build must place dist in the real go:embed location
make build

# Delivery must produce an explicit binary, not only go build ./...
# Example:
# go build -tags webui -o ./bin/venuewire ./cmd/venuewire

# Application loads the file itself; no source/export required
./bin/venuewire --env-file /etc/trading-console/console.env web
```

Go serves the static frontend; do not use the Vite development server as the deployment server. Deliver the executable together with its SHA/build information, preserving the project's existing `venuewire` name.

Local development tests may inject config through httptest/Browser test harness. Do not leave a production remote-deployment flag that disables all validation simply to make local HTTP testing easier.

### 16.2 Runtime Lifecycle

On SIGTERM, stop accepting new quote confirmations, leave persisted intents recoverable, persist state, close Browser/exchange connections, and exit within a bounded timeout. Do not automatically liquidate or cancel orders not owned by this application.

On startup, recover pending intents/reservations before enabling Web trading. The read-only home page may display recovering snapshots earlier. User logout/disconnect does not stop backend recovery.

### 16.3 Required Codex Deliverables

- Incremental source code, Go/frontend tests, actual build/start commands.
- `.env.example` with empty secrets only and compatibility with existing settings.
- `docs/v3/BASELINE_AUDIT.md`, `PROTOCOL_NOTES.md`, `API.md`, `DEPLOYMENT.md`, `DEMO.md`.
- Nginx map/location snippets matching the implementation and deployment ACL documentation.
- `IMPLEMENTATION_STATUS.md`, `TEST_REPORT_V3.md`, Traditional Chinese `CODEX_HANDOFF_V3.md`.
- UI screenshots: login, Bybit balance, Deribit balance, review, waiting, filled/partial, balance syncing/synced, degraded.
- Sanitized test evidence with no test username/password, cookies, API secret, or real account ID.

---

## 17. Phased Implementation and Exit Criteria

| Phase | Work | Exit criteria |
|---|---|---|
| 0 | Baseline audit, regression tests, gap matrix | Actual code state, known failures, and preservation list documented |
| 1 | dotenv, config validation, login/session/CSRF, proxy trust | Missing required config fails safely; no private route can be read unauthenticated |
| 2 | Account normalization, private WS, recovery, API | Both venues can show fixture + available real account data; positions are not misused as balance |
| 3 | Vue home, venue store, Browser WS, valuation | Venue switch does not mix data; valuation updates when price changes without quantity changes; stale visible |
| 4 | Spot metadata, capacity, quote/decimal/fee | Four route mappings and spend-cap tests pass; no real order submitted |
| 5 | Confirm/intent/reservation/tracker, modal results | Double-click/timeout/restart do not duplicate; filled/partial/unknown correct |
| 6 | Post-trade balance sync, UX, Nginx build/deployment example | Modal + home update together; cookie/WSS/proxy tests pass |
| 7 | Testnet opt-in, regressions, handoff | Local acceptance complete; external results listed individually as PASS/BLOCKED/NOT_RUN |

Blocked external validation is not a reason to stop local implementation, weaken acceptance, or fabricate success.

---

## 18. Test and Formal Acceptance Matrix

### 18.1 Configuration / Authentication / Proxy

| ID | Case | Required result |
|---|---|---|
| T01 | Same variable in OS env and dotenv; OS value empty | OS wins; required empty value errors; no fallback to old secret |
| T02 | Quotes, whitespace, `#`/`$`/backslash, explicit `.ENV` | Parser behavior tested/documented; no shell execution |
| T03 | Missing/malformed file, missing password/session secret | Behaves per §4; Web fails closed; old CLI not blocked by Web requirements |
| T04 | Unauthenticated HTTP/WS/existing dashboard endpoint | All private data/writes rejected, not only new routes |
| T05 | Correct login, bad credentials, brute-force limiting | Secure cookie correct; uniform errors; 429 works and does not permanently lock account |
| T06 | Logout/TTL/restart | Session invalidated; old WS closes; orders remain recoverable |
| T07 | Malicious Origin/Host/CSRF, forged XFF | Rejected by policy; cannot forge trusted client IP |
| T08 | Nginx private IP, WSS, direct port, dotfiles | Deployment conditions verifiable; no mixed content; secrets inaccessible |
| T09 | Global read-only mode | Even modified frontend/direct POST cannot trade |

### 18.2 Account / Market Data / UI

| ID | Case | Required result |
|---|---|---|
| T10 | Bybit/Deribit non-zero, multi-asset, zero, liability | Per-asset/field semantics correct; unknown never becomes zero |
| T11 | Deribit `summaries` with `positions=[]` | Balance still displayed; empty derivatives positions ≠ no assets |
| T12 | Wallet has no initial snapshot; only price changes | Initial quantity correct and valuation keeps updating without wallet events |
| T13 | Private connection quiet but heartbeat healthy | No false offline just because there are no trades |
| T14 | Delta contains only BTC; full snapshot omits zero asset | ETH not wrongly cleared; zeroing follows verified snapshot semantics |
| T15 | Snapshot/private-event interleaving / recovery-buffer overflow | Merge correctly; if uncertain show Recovering and block new trade |
| T16 | USDT ≠ 1 USD, no FX, unknown token | Do not hard-code 1:1; do not claim complete total |
| T17 | Account has derivatives/cross-collateral | Do not reconstruct full equity incorrectly or double-count aggregate |
| T18 | Venue switch with late old HTTP/WS | No cross-account overwrite; no global selected venue |
| T19 | Multiple Browsers + slow client | Exchange API/WS count does not scale linearly; slow client does not block trading |
| T20 | WS gap/instance change/reload | Resnapshot and recover pending intents; do not falsely display live |

### 18.3 Trading / Fees / Recovery

| ID | Case | Required result |
|---|---|---|
| T21 | Four directions, amount/base/quote | Mapping correct; Deribit Spot does not use USD contract amount |
| T22 | Tick/step/minimum/maximum | Buy does not exceed protection, sell does not go below protection; quantity not rounded up |
| T23 | Stale/gapped/crossed book, empty depth, venue risk limit | Quote/confirm blocked or normally rejected; no route/price-policy mutation |
| T24 | Quote expires / capacity changes / payload tampering | Original quote not silently recomputed; must review/confirm again |
| T25 | Fee in From/To/third asset, synthetic 1% fee, rebate | Spend cap, gross/net, fee currencies correct |
| T26 | Synthetic fixture: 1.00000 ETH gross, 0.01000 ETH fee | Display 1 ETH filled, 0.99 ETH net credited; not partial fill |
| T27 | ACK no fill, RPC immediate fills, WS arrives first | Correct truth + dedup; no false success/fake wait |
| T28 | IOC partial then cancel, zero-fill cancel | `PARTIAL_CANCELLED` / `CANCELLED_NO_FILL`; preserve fill |
| T29 | Double confirm, concurrency, refresh, same quote with different request ID | At-most-one local intent dispatch; return same resource or conflict |
| T30 | HTTP timeout after possible submission / Browser disconnect | OutcomeUnknown; reconcile with original ID; no second order |
| T31 | Crash at persist/send/ACK points | Persisted intent recoverable; restart does not resend unknown order |
| T32 | Same execution via REST/WS/reconcile | Quantity/fees/reservations accounted once |
| T33 | Two sessions spend same funds, CLI concurrent write | Reservation/account writer effective; no lost update |
| T34 | Terminal trade first, wallet later, fee later | Sync status visible; both balances update; no fabricated after balance |
| T35 | Deribit routed Spot async fills, if metadata enables | Use correct event/query source; do not rely only on HTTP response |
| T36 | Existing Bybit/Deribit/FIX/CLI regression | Web upgrade does not break existing functionality/data |

### 18.4 Test Levels

1. **Unit:** dotenv, safe config, decimal/tick, quote cap, fee model, intent idempotency, account reducer, valuation quality, session/proxy.
2. **Local integration:** mock exchange + Go API + WS + persistent restart; include dropped packets, out-of-order events, body errors, business rejection, clock injection.
3. **Browser E2E:** login → switch venues → live balance → modal review/confirm → filled/partial → modal + home update; logout/relogin/refresh recovery.
4. **Deployment integration:** optional local Nginx fixture for proxy/cookie/WSS; actual two-host deployment listed separately.
5. **Real Testnet:** separate `RUN_BYBIT_INTEGRATION` / `RUN_DERIBIT_INTEGRATION` and an **independent write authorization flag** `ALLOW_TESTNET_ORDERS=1`. Integration flag alone does not authorize orders. Use strict test budgets and isolate from automatic CI.

Real Testnet validation requires account query, market data, private events, and at least one explicitly authorized order flow per venue. Fixture success is not a substitute. Routes blocked by market/account conditions are marked BLOCKED rather than repeatedly sweeping orders to make tests green.

### 18.5 Performance and Presentation Targets

The following are controlled-test acceptance targets only and must not be described as measured results:

- Under normal conditions, after backend processing, account/valuation events should appear in Browser in roughly 1s; measure exchange latency separately from internal push latency.
- When valid prices change, valuation should visibly update even if quantity does not; do not simulate with random animation.
- After order terminal/balance-updated events arrive, Modal and home page must use consistent revisions.
- Five foreground Browser sessions should not increase exchange private connection count; long-running/reconnect tests must not show unbounded queue/goroutine growth.

---

## 19. Definition of Done and Handoff Format

### Local Functionality Complete

- [ ] All R01–R13 have corresponding implementation and tests.
- [ ] Existing two-venue connectivity/CLI/FIX regression results are clear with no unexplained regressions.
- [ ] No shell source required; fixed-env login, CSRF/proxy/WSS work.
- [ ] Home page displays real account snapshot; valuation shows source and quality.
- [ ] Four Spot directions, units, 0.5% protection, and fee rules are verifiable.
- [ ] Confirm uses persisted idempotency + reservation; unknown outcomes are not automatically retried.
- [ ] Filled/Partial/Cancelled/Rejected/Unknown all have UI and tests.
- [ ] After trade, Modal/home account sync; pending fee/balance states are visible.
- [ ] Full Web build produces an explicit binary; old CLI build does not require hidden frontend artifacts.
- [ ] Split-host Nginx/Go examples, ACL, secret protection, and rollback docs are complete.

### External Validation Complete

- [ ] Bybit Testnet: read/stream/authorized trading validation + evidence.
- [ ] Deribit Testnet: read/stream/authorized trading validation + evidence.
- [ ] Actual Nginx HTTPS → separate Go host login/WSS/trade/balance flow.

If the external checklist is incomplete, handoff wording must be "local implementation complete; external validation pending" rather than saying everything is complete.

`CODEX_HANDOFF_V3.md` must include:

- Modified files and baseline→V3 differences; data migration/rollback.
- Actual dotenv path, config-name mapping without values, build/start commands.
- Official protocol verification date and Spot/fee/valuation differences.
- Test commands and PASS/FAIL/BLOCKED/NOT_RUN plus evidence paths for each category.
- Nginx/Go placeholders still needing real deployment values.
- Known limitations, unresolved intents, and external conditions requiring user action.

---

## 20. Official Sources and Verification Record

This section defines protocol references, not copy-paste response requirements. Reviewed on **2026-09-10**. Codex must re-check any method it implements. Defaults, UI behavior, budgets, and acceptance policy come from this specification and must not be described as exchange rules.

### Bybit

- **[B1] Wallet balance / account scope / field semantics:**
  `https://bybit-exchange.github.io/docs/v5/account/wallet-balance`
- **[B2] Private wallet stream / snapshot and PnL limitations:**
  `https://bybit-exchange.github.io/docs/v5/websocket/private/wallet`
- **[B3] Place order / Spot parameters / ACK:**
  `https://bybit-exchange.github.io/docs/v5/order/create-order`
- **[B4] Instrument precision / tick / limits:**
  `https://bybit-exchange.github.io/docs/v5/market/instrument`
- **[B5] Executions / per-fill fee currency:**
  `https://bybit-exchange.github.io/docs/v5/websocket/private/execution`
- **[B6] Actual Spot trade capacity excluding borrowing:**
  `https://bybit-exchange.github.io/docs/v5/order/spot-borrow-quota`
- **[B7] Public ticker / Spot updates:**
  `https://bybit-exchange.github.io/docs/v5/websocket/public/ticker`
- **[B8] Account fee rate:**
  `https://bybit-exchange.github.io/docs/v5/account/fee-rate`

### Deribit

- **[D1] Multi-currency account summaries and cross-collateral information:**
  `https://docs.deribit.com/api-reference/account-management/private-get_account_summaries`
- **[D2] Private portfolio updates and field semantics:**
  `https://docs.deribit.com/subscriptions/user/userportfoliocurrency`
- **[D3] Instrument metadata / Spot units / routed flag:**
  `https://docs.deribit.com/api-reference/market-data/public-get_instrument`
- **[D4] Spot order / IOC / order and trades response:**
  `https://docs.deribit.com/api-reference/trading/private-buy`
- **[D5] Deribit / Coinbase-routed Spot differences:**
  `https://docs.deribit.com/articles/spot-trading-venues`
- **[D6] Query multiple orders by label:**
  `https://docs.deribit.com/api-reference/trading/private-get_order_state_by_label`
- **[D7] Official Spot instruments:**
  `https://support.deribit.com/hc/en-us/articles/31424969480093-Spot-Instruments`
- **[D8] Private Spot execution events:**
  `https://docs.deribit.com/subscriptions/user/usertradeskindcurrencyinterval`

### Configuration / Web Security / Nginx

- **[C1] godotenv loading and env precedence:**
  `https://github.com/joho/godotenv`
- **[S1] OWASP Session Management:**
  `https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html`
- **[S2] OWASP CSRF Prevention:**
  `https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html`
- **[S3] OWASP WebSocket Security:**
  `https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html`
- **[N1] Nginx WebSocket proxying / heartbeat timeout:**
  `https://nginx.org/en/docs/http/websocket.html`
- **[N2] Nginx trusted real IP:**
  `https://nginx.org/en/docs/http/ngx_http_realip_module.html`
- **[N3] Nginx proxy headers / timeouts / retry / inheritance:**
  `https://nginx.org/en/docs/http/ngx_http_proxy_module.html`

---

## Appendix A: Prompt Ready to Paste into Codex

```text
Read TRADING_CONSOLE_V3_CHANGE_SPEC.md together with the repository's AGENTS.md,
README, existing V2 specification, and handoff documents.
Perform the Phase 0 baseline audit first, then implement V3 incrementally.

Do not rebuild the project and do not break the completed Bybit/Deribit,
CLI, FIX, stored data, or existing tests.
This version adds: application-level dotenv loading, fixed env-based login,
a Vue Web UI for switching venues, a live account/valuation page, and Spot
Quick Trade modals for Deribit BTC<->USDC and Bybit BTC/ETH<->USDT.

Trades must follow Review -> Confirm and use protected Limit IOC with a
0.5% price boundary. Correctly handle partial fills, actual fees, unknown
outcomes, durable idempotency, and recovery. Trade result and account
synchronization are separate concerns. The modal balance and underlying
home page must use the same account store and update together.

Nginx already terminates SSL and runs on a different host from Go.
Go binds to a configurable private IP, trusts only explicitly configured
Nginx sources, supports HTTPS/WSS reverse proxying plus Secure session
cookies and Origin/CSRF validation. Do not add Go TLS or expose the Go
port publicly.

Complete local tests and builds first. If credentials or deployment access
are unavailable, mark external validation BLOCKED/NOT_RUN.
Do not place Testnet orders, modify Nginx/firewall, or use previously leaked
credentials without explicit user authorization.
Update IMPLEMENTATION_STATUS.md after each phase.
Finally provide actual build/start commands, test report, and a Traditional
Chinese CODEX_HANDOFF_V3.md.
```

---

## Appendix B: Minimum Frontend/Backend Data Contract Fields

These are normalized DTOs, not requirements that exchanges return the same field names. Existing naming may be retained, but `docs/v3/API.md` must provide one-to-one mapping. All decimal values remain strings. Unknown values may be `null` with a reason.

### B.1 AccountView

| Field | Requirement |
|---|---|
| `venue`, `environment`, `accountAlias`, `accountType` | Explicit scope; environment must be testnet |
| `accountRevision`, `observedAt` | Account-store revision and observation time; do not present as an exchange-global sequence |
| `syncStatus` | Ready / Recovering / Degraded / Unavailable |
| `exchangeEquityUsd`, `exchangeEquityAsOf` | Exchange-reported value and time; null if unsupported |
| `valuation.totalUsd` | Only populated when pricing coverage is complete and quantity basis is clear |
| `valuation.pricedSubtotalUsd` | Priced-range subtotal; must not substitute for full total |
| `valuation.basis`, `valuation.completeness`, `valuation.unpricedAssets` | Explain holdings/equity basis, completeness, and missing assets |
| `liabilityStatus`, `hasDerivativePositions` | Distinguishable states such as none/present/unknown; controls heading and risk warning |
| `assets[]` | AssetView below; zero and unknown must not be conflated |

AssetView must include at least `asset`, `balance`, `equity`, `locked`, `liability`, `availableToTrade`, `availableToTradeAsOf`, `availableStatus`, `valuationQuantity`, `quantityBasis`, `usdValue`, `priceSource`, `priceAsOf`, and `quality`.

Local holdings valuation defaults to verified cash/wallet balance semantics. Do not use available margin or incomplete derivatives equity as asset quantity. When liabilities exist, valuation of raw cash is gross holdings and must not be labeled net worth. If there is no reliable net-quantity model, retain liability information and lower total-completeness status. Use the main `Total Account Value` heading only for pure Spot / no liabilities / complete valuation.

If `availableToTrade` varies by route, return `capacityByRoute`. Assets without supported routes still show observable balance but trade capacity is null. Never rename withdrawable or aggregate margin into `availableToTrade`.

### B.2 QuoteView

Must include at least:

```text
quoteId, venue, environment, accountAlias, routeId
fromAsset, toAsset, spendBudget
instrument, side, baseQty, limitPrice, timeInForce
referenceBid, referenceAsk, bookObservedAt, metadataRevision
priceProtectionBps, grossReceiveEstimate, netReceiveEstimate
fees[] { asset, estimatedAmount, source, calculationBasis }
sourceDebitUpperBound, thirdAssetReserves[]
accountRevision, createdAt, expiresAt
warnings[], executable, blockedReason
```

Fee estimate is not the spend-protection cap. The model must constrain `sourceDebitUpperBound` to the user's budget. A quote with `executable=false` must remain unexecutable even if the frontend sends Confirm.

### B.3 TradeView

Must include at least:

```text
intentId, clientRequestId, quoteId
venue, environment, accountAlias, routeId, nativeOrderId
submittedBaseQty, submittedLimitPrice, submittedTimeInForce
exchangeState, commandState, resultStatus, stateRevision
filledBaseQty, grossSourceSpent, grossDestinationReceived, averagePrice
fees[] { asset, amount }, netDestinationReceived, actualSourceDebit
remainingBudget, fillDetailsStatus, feeDetailsStatus, balanceSyncStatus
createdAt, submittedAt, terminalAt, lastCheckedAt
beforeAccountRevision, afterAccountRevision, discrepancyReasons[]
```

Use explicit states such as `pending / complete / needs_review` separately for `fillDetailsStatus`, `feeDetailsStatus`, and `balanceSyncStatus`. Do not populate a fabricated net result while details remain incomplete. `afterAccountRevision` stays null until reconciled.

If an exchange fee total already includes an extra fee component, do not add it again. The inclusion relationship must be verified in adapter logic and fixtures. Preserve sanitized diagnostics for unknown fields rather than arbitrarily aggregating monetary values.
