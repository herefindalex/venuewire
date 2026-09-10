# VenueWire V3.1 — Public Interview Demo Hardening Supplement

> Section 26 records the confirmed product decisions and takes precedence where V3 or this supplement differs. Configuration names follow V3; see `.env.example`. Implementation gaps are tracked in `../v3/V3_1_GAP_ANALYSIS.md`.

## 0. Purpose

This document is an **incremental supplement** to `TRADING_CONSOLE_V3_CHANGE_SPEC.md`.

Do **not** rebuild the project. Preserve all working Bybit/Deribit connectors, CLI tools, FIX work, account state, trading flows, and existing tests.

The purpose of V3.1 is to make VenueWire suitable for limited public access by interviewers while making the system's trading-technology design visible and defensible.

Primary goals:

1. Make the public demo safe enough for Testnet use.
2. Make order/execution behavior explainable under failure.
3. Expose multi-venue architecture rather than just two exchange integrations.
4. Make freshness, reconciliation, and observability visible in the UI.
5. Ensure the project is described accurately as a prototype, not production infrastructure.

---

# 1. Product Naming and Positioning

The public product name is:

> **VenueWire — Multi-Venue Trading Connectivity Console**

The login page and main navigation should use `VenueWire`.

Recommended login subtitle:

> Multi-Venue Trading Connectivity Console  
> Bybit · Deribit · TESTNET

The project must not claim:

- production trading platform;
- production FIX connectivity unless actually verified;
- high-frequency trading;
- production-grade multi-venue execution;
- real-money trading.

Recommended README wording:

> A multi-venue trading connectivity and execution prototype integrating Bybit and Deribit test environments over REST/HTTP JSON-RPC and WebSocket, with venue abstraction, normalized order state, reconciliation, idempotency, failure handling, and observability.

---

# 2. Testnet-Only Safety Guard

Testnet-only operation is a hard requirement.

## 2.1 Startup Validation

At startup, authenticated trading must fail closed if any configured exchange endpoint is not a recognized Testnet endpoint.

The application must explicitly validate configured hosts, not only trust an environment label.

Examples:

- Bybit REST must resolve to the configured Testnet host.
- Bybit WebSocket must resolve to the Testnet stream host.
- Deribit HTTP/WebSocket must resolve to `test.deribit.com` or the current documented Testnet equivalent.

If an endpoint does not pass the allowlist:

```text
FATAL: mainnet or unapproved exchange endpoint detected
```

Do not provide a casual `--mainnet` override in this project.

## 2.2 UI

Every authenticated page must display a visible:

```text
TESTNET
```

badge.

The trade review modal must also display `TESTNET`.

---

# 3. Secrets and Exchange Credentials

Exchange credentials remain backend-only.

## 3.1 Browser Boundary

The browser must never receive:

- Bybit API key;
- Bybit API secret;
- Deribit client secret;
- FIX private key;
- signatures;
- `.env` contents;
- process environment dump.

No frontend build variable may contain exchange credentials.

## 3.2 API Key Policy

Use demo-specific Testnet credentials.

Permissions must follow least privilege:

- account read;
- trading permissions required by VenueWire;
- **no withdrawal permission**;
- no unrelated account-management permission.

Document the required permission set per venue.

---

# 4. Public Demo Authentication Hardening

The V3 `.env`-based single-user login remains valid.

Example:

```env
WEB_USERNAME=
WEB_PASSWORD=
WEB_SESSION_SECRET=
```

No registration or user database is required.

## 4.1 Session Requirements

Mandatory:

- HttpOnly cookie;
- Secure cookie when served through HTTPS;
- explicit session expiration;
- logout endpoint;
- session validation on every protected REST endpoint;
- session validation during Browser WebSocket upgrade;
- WebSocket `Origin` validation;
- constant-time comparison where appropriate;
- failed-login rate limiting;
- generic login error response.

Do not expose whether username or password was wrong.

## 4.2 Browser Errors

Never expose raw:

- panic;
- stack trace;
- Go internal error;
- exchange secret;
- upstream request body containing credentials;
- internal filesystem paths.

Return a safe public error plus an internal correlation ID.

Example:

```json
{
  "error": "trade_submission_uncertain",
  "message": "The exchange result could not be confirmed.",
  "requestId": "req_..."
}
```

## 4.3 Nginx Additional Gate

The Go application must not implement Nginx Basic Auth itself.

Document that the operator may optionally place one additional access gate at Nginx for interview links:

```text
Internet
  -> HTTPS / Nginx
  -> optional temporary access gate
  -> VenueWire login
  -> Go backend
```

This is operational guidance, not a mandatory app feature.

---

# 5. Public Demo Abuse Protection

Because interviewers can submit Testnet orders, authenticated access alone is insufficient.

Add configurable demo limits.

Recommended environment variables:

```env
WEB_TRADING_ENABLED=false

DEMO_MAX_TRADES_PER_SESSION=10
DEMO_MAX_TRADES_PER_HOUR=30
DEMO_MAX_CONCURRENT_TRADES=1

DEMO_MAX_BYBIT_BTC_QTY=0.01
DEMO_MAX_BYBIT_USDT_AMOUNT=1000
DEMO_MAX_DERIBIT_BTC_AMOUNT=0.01
DEMO_MAX_DERIBIT_ETH_AMOUNT=1
```

These source-asset spend caps are project policy defaults, not verified exchange limits. Apply the stricter of the V3 asset cap and the venue-specific demo cap, then validate current instrument rules, available funds and fee reserves. For BTC source routes, the spend cap includes any fee charged in BTC; it is not merely an order quantity cap.

## 5.1 Server-Side Enforcement

All limits must be enforced by the Go backend.

Frontend disable states are UX only and are not security controls.

## 5.2 Multiple Tabs / Repeated Clicks

The system must safely handle:

- double-click Confirm;
- repeated browser retries;
- refresh after submit;
- multiple tabs;
- a second trade attempt while the first is unresolved.

A logical trade intent may not create multiple exchange orders due only to frontend retry.

---

# 6. Trade Intent, Idempotency, and Correlation

Every quick trade must have three distinct identifiers where available:

```text
VenueWire Trade Intent ID
        |
        v
Exchange Client Order ID / Label
        |
        v
Exchange Order ID
```

Example normalized fields:

```go
type TradeIntent struct {
    ID                  string
    Venue               VenueID
    ClientOrderID       string
    VenueOrderID        string
    RequestedFromAsset  string
    RequestedToAsset    string
    RequestedAmount     Decimal
    Status              TradeStatus
    CreatedAt           time.Time
    UpdatedAt           time.Time
}
```

The server creates the VenueWire Trade Intent ID before sending any exchange order.

Retries from the same logical browser action must resolve to the existing intent rather than create a new one.

## 6.1 Exchange-Specific Caution

Do not assume every venue's client label is globally unique.

The adapter must document the real uniqueness/idempotency semantics for:

- Bybit `orderLinkId`;
- Deribit `label`.

VenueWire's internal Trade Intent ID remains authoritative for local correlation.

---

# 7. Order / Trade State Machine

Do not reduce trade outcome to only Success/Failed.

Minimum normalized state model:

```text
Created
  -> PendingSubmit
  -> Submitted
  -> Accepted
  -> PartiallyFilled
  -> Filled

Accepted / PartiallyFilled
  -> PendingCancel
  -> Cancelled

Any relevant state
  -> Rejected

Submission timeout / lost confirmation
  -> Unknown
```

`Unknown` is mandatory.

Example:

```text
HTTP timeout != order failed
```

If VenueWire cannot prove whether the venue accepted the order, the UI must show an uncertain state and reconciliation must determine the result.

A later authoritative Filled result must be able to supersede an in-flight cancel attempt where exchange behavior indicates the fill won the race.

---

# 8. Quick Trade Confirmation and Execution

V3 quick-trade behavior remains:

## Deribit Spot

```text
ETH -> BTC
BTC -> ETH
```

using the exchange-supported ETH/BTC Spot instrument.

## Bybit Spot

```text
USDT -> BTC
BTC -> USDT
```

using BTCUSDT Spot.

## 8.1 Order Type

Default:

> Marketable Limit IOC with configurable price protection.

Default protection:

```env
QUICK_TRADE_SLIPPAGE_BPS=50
```

50 bps = 0.50%.

The quote/review step must display:

- reference bid/ask;
- requested source amount;
- estimated destination amount;
- protection percentage;
- worst acceptable price;
- quote age.

## 8.2 Quote Freshness

The Confirm action must be rejected if the quote is stale.

Example configurable threshold:

```env
QUOTE_TTL=5s
```

If stale:

```text
Quote expired. Refresh the quote before submitting.
```

## 8.3 Modal States

Required:

```text
EDIT
 -> REVIEW
 -> SUBMITTING
 -> ACCEPTED / WAITING
 -> FILLED / PARTIAL / REJECTED / CANCELLED / UNKNOWN
```

A REST/RPC acknowledgement alone must not be presented as `Trade Completed`.

---

# 9. Partial Fills, Fees, and Net Received Amount

The result UI must distinguish:

- requested quantity;
- filled quantity;
- average execution price;
- gross received amount;
- fee;
- fee currency;
- net received amount.

Do not assume:

```text
gross received == account balance increase
```

because fees may be charged in either the source asset, destination asset, quote asset, or another venue-defined currency.

For IOC:

- zero fill must be visible;
- partial fill must be visible;
- remaining unfilled quantity is cancelled by IOC semantics;
- the UI must not label a partial fill as a full success.

---

# 10. Reconciliation

Reconciliation is a first-class feature, not a fallback implementation detail.

Run reconciliation:

1. at process startup;
2. after private exchange WebSocket reconnect;
3. after an uncertain trade submission;
4. after a completed quick trade before final balance confirmation where needed;
5. manually from an internal/debug action if implemented.

Conceptual flow:

```text
Load local state
   ->
Query venue order state
   ->
Query recent executions
   ->
Query balances / positions
   ->
Compare
   ->
Repair normalized local state
   ->
Resume live stream
```

Reconciliation must be idempotent.

Do not create replacement orders automatically during reconciliation.

---

# 11. Multi-Venue Architecture

VenueWire must not become a large set of frontend/backend conditionals.

Avoid architecture dominated by:

```go
if venue == "bybit" {
    ...
} else if venue == "deribit" {
    ...
}
```

Use venue adapters behind normalized capabilities.

A conceptual interface may look like:

```go
type Venue interface {
    ID() VenueID

    GetAccount(ctx context.Context) (AccountSnapshot, error)
    GetQuote(ctx context.Context, req QuoteRequest) (Quote, error)

    PlaceOrder(ctx context.Context, req PlaceOrderRequest) (OrderAck, error)
    CancelOrder(ctx context.Context, req CancelOrderRequest) (OrderAck, error)

    OpenOrders(ctx context.Context) ([]Order, error)
    Executions(ctx context.Context, q ExecutionQuery) ([]Execution, error)
}
```

Streaming capabilities may use separate interfaces if REST/WS/FIX asymmetry makes that cleaner.

Do not force all venue behavior into one artificial symmetric interface.

---

# 12. Normalized Domain Model

Exchange-specific payloads must terminate inside adapters.

Architecture:

```text
Bybit payload --------\
                       -> Venue Adapter -> VenueWire Domain Model
Deribit payload ------/
```

Normalization must cover at least:

- symbol/instrument;
- base asset;
- quote asset;
- side;
- order type;
- time-in-force;
- quantity;
- filled quantity;
- average price;
- fee;
- fee currency;
- venue status;
- normalized status;
- client order ID;
- venue order ID;
- execution ID;
- exchange timestamp;
- receive timestamp.

Preserve the raw venue status alongside normalized status for debugging.

---

# 13. Decimal / Precision Rules

Do not use `float64` as the authoritative representation for:

- price;
- quantity;
- fee;
- balance;
- notional;
- filled amount.

Use a decimal library or an exact integer/scale model.

All venue precision/tick/step normalization must happen before submission.

The system must explicitly validate:

- minimum quantity;
- quantity step;
- tick size;
- minimum notional where applicable.

This requirement applies to backend/domain correctness, not only display formatting.

---

# 14. Live Account and Valuation Model

Account quantity/equity state and USD valuation are separate concepts.

Recommended model:

```text
REST account snapshot
       +
Private account/order/execution WS
       ->
Normalized account quantities

Public market WS
       ->
Live asset prices

Quantities + Prices
       ->
Valuation Engine
       ->
Browser WebSocket
```

## 14.1 Authoritative Snapshot

REST/HTTP account snapshot is required:

- on startup;
- after relevant reconnect;
- after reconciliation.

Private WebSocket is not assumed to provide a complete startup snapshot.

## 14.2 Live Valuation

Market price updates should update displayed USD value without requiring balance quantity to change.

The UI should distinguish where practical:

- exchange-reported equity/value;
- locally calculated live mark value.

Do not silently present a locally calculated value as an exchange-authoritative value.

---

# 15. WebSocket Freshness / Stale Data

`Connected` is not sufficient.

Every important stream must track:

- connection state;
- last message receive time;
- last meaningful event time;
- reconnect count;
- stale/fresh state.

Examples:

```text
Connected
Last message: 430 ms ago
```

and:

```text
STALE
Last market event: 8.4 s ago
```

Thresholds should be configurable per stream/message class.

When critical trade/account state becomes stale:

- UI must surface the stale status;
- trade submission may be disabled when required inputs cannot be trusted.

---

# 16. System Status UI

Add a visible System Status section.

Example:

```text
System Status

Bybit
Public WS        Live
Private WS       Live
Account Sync     Synced
Market Age       82 ms
Reconnects       1
Last Reconcile   14 s ago

Deribit
Public WS        Live
Private WS       Live
Account Sync     Synced
Market Age       46 ms
Reconnects       0
Last Reconcile   8 s ago
```

Status values must come from backend runtime state, not hard-coded frontend labels.

Suggested states:

```text
LIVE
DEGRADED
STALE
RECONNECTING
UNAVAILABLE
SYNCING
SYNCED
ERROR
```

---

# 17. Observability

VenueWire does not require a full Grafana deployment for this version.

Expose enough runtime measurements through the backend and UI to demonstrate engineering visibility.

Minimum measurements:

- REST/HTTP health by venue;
- public WS state;
- private WS state;
- reconnect count;
- last event timestamp;
- event age;
- REST order request RTT;
- time from order ACK to first order event;
- time from order ACK to first execution event;
- request error count;
- reconciliation status;
- reconciliation discrepancy count;
- rate-limit state where available.

Example UI:

```text
REST Order RTT           82 ms
First order event       +104 ms
First execution event   +116 ms
```

These are measurements, not claims of ultra-low-latency performance.

Use monotonic clocks for elapsed-duration measurement where appropriate.

---

# 18. Recent Trades / Lifecycle UI

Add a Recent Trades section.

Each row should show:

- venue;
- direction;
- requested amount;
- final status;
- created time;
- short latency summary if available.

A detail modal/drawer should display the lifecycle.

Example:

```text
BTC -> USDT
Bybit

09:32:14.105   Intent Created
09:32:17.381   User Confirmed
09:32:17.426   Order Submitted
09:32:17.511   Exchange Accepted
09:32:17.583   Partial Fill
09:32:17.617   Filled
09:32:17.689   Account Synced
```

Also display:

```text
VenueWire Trade ID
Client Order ID
Venue Order ID
Filled Quantity
Average Price
Fee / Fee Currency
Net Received
```

Do not expose credentials or internal network information.

---

# 19. About / Architecture UI

Add a lightweight About VenueWire modal/drawer/page.

Required content:

```text
VenueWire

Multi-Venue Trading Connectivity Console

Venues
- Bybit
- Deribit

Capabilities
- REST / HTTP JSON-RPC
- WebSocket
- FIX 4.4 implementation where applicable
- Real-time market data
- Authenticated Testnet order execution
- Normalized order lifecycle
- Reconciliation
- Account state recovery
- Multi-venue normalization
```

Include a simple architecture diagram:

```text
Browser
   |
HTTPS / WebSocket
   |
Nginx
   |
VenueWire Go Backend
   |
   +-- Venue Abstraction
   +-- Normalized Domain Model
   +-- Order State Machine
   +-- Reconciliation
   +-- Valuation
   +-- Observability
          |
      +---+---+
      |       |
    Bybit   Deribit
```

## 19.1 Build Information

Display non-sensitive runtime/build metadata:

- environment: Testnet;
- backend: Go;
- frontend: Vue 3;
- build timestamp;
- Git commit;
- uptime.

Do not display:

- API key;
- account secret;
- internal IP;
- hostname;
- filesystem path;
- environment dump.

---

# 20. Nginx / Split-Host Deployment

Deployment assumption:

```text
Internet / Interviewer
        |
      HTTPS
        |
      Nginx
        |
  private network
        |
 VenueWire Go Backend
```

Nginx and VenueWire run on different hosts.

## 20.1 Go Bind

Go must bind to a configurable private interface:

```env
WEB_HOST=<private-ip>
WEB_PORT=8080
WEB_TRUSTED_PROXY_CIDRS=<nginx-private-ip>/32
```

Do not expose the Go port directly to the public Internet.

Firewall rules should allow the VenueWire port only from the Nginx host.

## 20.2 Proxy Headers

Only trust forwarding headers when the direct TCP peer is the configured trusted Nginx IP.

Relevant headers:

```text
X-Forwarded-For
X-Forwarded-Proto
X-Real-IP
Host
```

The Browser WebSocket route must support Nginx WebSocket upgrade proxying.

TLS terminates at Nginx.

---

# 21. Public Demo UI Layout

Recommended top-level view:

```text
VenueWire                       [Bybit v] [TESTNET] [Logout]

Account Value
$...

Assets
------------------------------------------------
BTC        qty       available       USD value
ETH        qty       available       USD value
USDT       qty       available       USD value

[Quick Trade]

System Status
------------------------------------------------
Bybit       Public WS    LIVE
            Private WS   LIVE
            Sync         SYNCED
            Market Age   82 ms

Deribit     Public WS    LIVE
            Private WS   LIVE
            Sync         SYNCED

Recent Trades
------------------------------------------------
Time       Venue     Direction      Status
...
```

The UI should be clean and compact. Do not turn V3.1 into a full exchange trading terminal.

---

# 22. Explicit Non-Goals

Do not add in V3.1:

- candlestick/K-line chart;
- full depth order-book visualizer;
- strategy signals;
- automated strategy trading;
- arbitrage engine;
- smart order router;
- cross-venue automatic failover order placement;
- portfolio analytics suite;
- third exchange;
- production/mainnet support.

These are out of scope because they weaken the project's primary message: connectivity, execution correctness, normalization, recovery, and observability.

---

# 23. Failure Scenarios Required for Demo / Tests

Codex must add tests or reproducible fixtures for:

1. User clicks Confirm twice.
2. Two tabs submit the same trade intent.
3. HTTP/RPC order submission times out with unknown exchange result.
4. Private WebSocket disconnects after order ACK.
5. VenueWire restarts while an order is active.
6. IOC order partially fills.
7. IOC order receives no fill.
8. Fill arrives while cancel is pending.
9. Balance/account stream is connected but stale.
10. Public market stream becomes stale.
11. Exchange rejects price/quantity due to instrument rules.
12. Rate limit is reached.
13. Browser refreshes during `Unknown` state.
14. Reconciliation changes an `Unknown` order to Filled/Cancelled/Rejected.
15. Trading is disabled through `WEB_TRADING_ENABLED=false`.

All cases must have deterministic local/unit/integration fixtures where practical; CI must not depend on external Testnet availability.

---

# 24. Acceptance Criteria

V3.1 is complete only when all are true:

- [ ] VenueWire branding appears in login/main UI.
- [ ] TESTNET hard guard validates actual venue endpoints.
- [ ] UI shows TESTNET prominently.
- [ ] no exchange credentials reach browser.
- [ ] demo API keys use documented least-privilege guidance.
- [ ] login rate limiting works.
- [ ] session expiration works.
- [ ] protected REST endpoints validate session.
- [ ] Browser WebSocket validates session and Origin.
- [ ] raw stack traces/internal errors are not exposed.
- [ ] backend trade-rate limits exist.
- [ ] double-click/retry cannot create duplicate logical trades.
- [ ] VenueWire Trade ID -> client order ID -> venue order ID correlation is stored.
- [ ] `Unknown` order state exists and is exercised by tests.
- [ ] restart reconciliation works.
- [ ] reconnect reconciliation works.
- [ ] venue-specific payloads are normalized behind adapters.
- [ ] monetary/quantity values do not use authoritative float64 arithmetic.
- [ ] stale stream state is detectable.
- [ ] stale status is visible in UI.
- [ ] System Status UI is implemented.
- [ ] Recent Trades lifecycle UI is implemented.
- [ ] About/Architecture UI is implemented.
- [ ] order RTT/event timings are measured.
- [ ] rate-limit/reconnect/reconciliation status is observable.
- [ ] IOC partial fill is displayed correctly.
- [ ] fee and net received amount are displayed correctly.
- [ ] account balances refresh after confirmed execution/reconciliation.
- [ ] split Nginx/Go deployment supports Browser WebSocket.
- [ ] Go port is documented as private-only.
- [ ] README uses accurate prototype wording.
- [ ] `go test ./...` passes.
- [ ] frontend tests/build pass.
- [ ] existing Bybit and Deribit functionality remains green.

---

# 25. Codex Execution Instructions

Before coding:

1. Read the original V3 specification.
2. Read this V3.1 supplement.
3. Inspect the existing repository and current tests.
4. Produce a short gap analysis against sections 1–24.
5. Implement incrementally; do not rebuild working exchange connectors.

Recommended order:

```text
Phase A
Safety / auth / demo-rate-limit hardening

Phase B
Trade intent / idempotency / Unknown state

Phase C
Reconciliation hardening

Phase D
Venue normalization / Decimal audit

Phase E
Freshness + observability backend

Phase F
System Status UI

Phase G
Recent Trades lifecycle UI

Phase H
About / Architecture / build info

Phase I
Failure tests / deployment verification
```

After each phase:

- run backend tests;
- run frontend tests/build;
- update `IMPLEMENTATION_STATUS.md`;
- record deviations rather than silently weakening requirements.

Final delivery must include:

- `CODEX_HANDOFF_V3_1.md`;
- build/start commands;
- required `.env` variables;
- Nginx/WebSocket deployment notes;
- tests executed;
- Testnet behaviors actually verified;
- behaviors tested only through mocks/fixtures;
- known limitations.

---

# 26. Confirmed Product Decisions

These decisions were confirmed with the user before implementation. They describe target behavior, not completed features.

## 26.1 Browser audience and shared history

- Interviewers use the browser. Login, account viewing, trading, lifecycle inspection and order rechecks must be available there.
- One configured login identity uses the shared demo exchange accounts. All authenticated viewers may see the shared Recent Trades, including trades created by other sessions.
- Relogin restores access to recent and unresolved intents. Session expiration/logout does not cancel exchange orders or discard recovery state.
- Preserve existing credential names: `DERIBIT_API_KEY` and `DERIBIT_API_SECRET`. Preserve existing read/trading/FIX gates. The project and executable name is `venuewire`.
- Reuse the existing locked JSON stores. One Web process owns submissions and reservations; multi-instance coordination and strong concurrent CLI/Web usage are out of scope.

## 26.2 Demo quotas and persistence

- Default limits: 10 trades per session, 30 trades across the rolling previous 60 minutes, and 1 concurrent trade.
- Hourly and concurrent limits are global across all browser sessions and both venues. Per-account locks remain isolated, but the global Demo quota may block new trades on either venue. This explicitly overrides V3's unconditional statement that another venue cannot be affected by an account being busy.
- A trade consumes quota when it passes local validation and its submission intent is durably saved, before any exchange order submission. Persist quota accounting with the intent so concurrent confirmation or a crash cannot bypass it.
- Exchange rejection, zero fill and `Unknown` each count as a trade. Duplicate confirmation/retries of the same intent count only once. Expired quotes and local validation failures before intent acceptance do not count.
- Hourly usage and unresolved-trade occupancy survive restart. A new login starts a new session quota but does not reset the global hourly quota or unresolved intents.
- `Unknown` retains its concurrent slot and applicable reservations until authoritative evidence resolves the result. Timeout, refresh, logout and restart cannot release it automatically. Do not create replacement orders.

## 26.3 Configuration contract

- Keep V3 names, including `WEB_TRUSTED_PROXY_CIDRS`, `QUICK_TRADE_SLIPPAGE_BPS` and `QUOTE_TTL`.
- Set `QUOTE_TTL=5s`; retain the default 50 bps price protection.
- Add `DEMO_MAX_TRADES_PER_SESSION`, `DEMO_MAX_TRADES_PER_HOUR`, `DEMO_MAX_CONCURRENT_TRADES` and the four venue-specific source-asset caps in section 5.
- Retain V3 `QUICK_TRADE_MAX_BTC`, `QUICK_TRADE_MAX_ETH` and `QUICK_TRADE_MAX_USDT`. Enforce both applicable caps, using the smaller value, plus venue rules and fee-aware capacity.
- Do not introduce alternate configuration aliases from earlier supplement examples. All examples must use this contract. The supplied amount defaults are policy ceilings and do not assert current Testnet instrument availability or validity.

## 26.4 Recheck from the browser

- Continue automatic reconciliation and add a protected, CSRF-validated “Recheck” action in trade details.
- Show in-progress, last-check time and the resulting status or a safe error. Bound and coalesce repeated checks through the backend's venue rate-limit controls.
- Recheck queries exchange state; it does not resubmit the order, force a failed status, clear reservations or bypass Demo limits.
- Preserve `Unknown` if the query cannot establish the outcome. The existing global concurrent limit continues to apply.

## 26.5 Failure demonstration and verification

- Cover section 23 with deterministic automated tests/fixtures. Use demonstration recordings for presenting failure scenarios.
- The public Web console shows actual Testnet lifecycle/runtime data; do not add an interactive fault simulator to this MVP.
- Any recording driven by mocks/fixtures must be labeled as simulated. Recordings are delivery artifacts to produce later, not evidence that live Testnet behavior has already been verified.
- No Testnet order, exchange account change or Nginx/firewall operation is authorized by these decisions; external operations retain their existing explicit authorization requirements.
