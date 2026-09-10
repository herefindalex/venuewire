# Bybit → Bybit + Deribit: Incremental Multi-Venue Upgrade Specification

**Version: 2.0 · Date: 2026-09-09 · Intended for: Codex**
**Applicable project: User's existing, completed, runnable Bybit Go project**
**Goal: Add Deribit to the existing project instead of creating two unrelated demo applications.**

> This document defines development requirements. It is not proof that the features have already been implemented or tested successfully.
> The user's Bybit implementation is known to be complete; the source code was not available when this document was written.
> Codex must first inspect the actual repository and confirm the current state of the CLI, dashboard, storage, REST/WebSocket/FIX implementations, and tests before making the minimum necessary changes.
> Requirements from the previous Bybit specification must not be assumed to have already been implemented.

---

## 0. Codex Execution Instructions

First read the repository's `AGENTS.md`, `README.md`, previous specifications, handoff documents, and this document. Then perform the Phase 0 baseline audit before implementing the remaining phases in order. Except where new user credentials, external authorization, or irreversible data operations are required, do not stop after delivering only a plan.

Mandatory rules:

1. **Extend incrementally; do not rewrite Bybit.** Preserve existing commands, configuration, UI, data, and previously corrected behavior.
2. **Use Testnet only.** Do not use credentials previously pasted in the conversation, do not connect to Mainnet, do not deposit or withdraw funds, and do not change account leverage or margin mode.
3. **Implement Deribit HTTP + WebSocket first, then Deribit FIX.** The core release must not be blocked by external FIX access issues.
4. **Distinguish implementation, simulated/local testing, and external validation.** When credentials are unavailable, continue local testing, but external tests must be marked `BLOCKED` or `NOT_RUN`, never passed.
5. **Do not automatically re-submit or hedge across venues.** Initial multi-venue functionality should provide explicit routing, independent recovery, and read-only aggregation.
6. Run relevant tests and Bybit regression tests at the end of each phase, and update `IMPLEMENTATION_STATUS.md`.
7. New paths and commands listed here are suggested interfaces. Prefer the existing repository structure, record the actual mapping, and do not move the entire project merely to match directory examples.

### Specification Priority

Safety and data-integrity requirements > verified official protocol behavior / actual responses > requirements in this upgrade specification > idealized architecture from older specifications.

If official documentation and Testnet behavior differ: retain sanitized evidence, document the discrepancy, and reduce support for the affected feature. Do not guess parameters, guess units, or silently fall back to production.

---

## 1. Release Goals and Scope

### 1.1 R1: Core Multi-Venue Release — Mandatory

- Existing Bybit functionality remains operational.
- Deribit Testnet: HTTP JSON-RPC queries plus place/edit/cancel orders.
- Deribit Testnet: WebSocket JSON-RPC queries, order submission, and private subscriptions.
- Public market data, order book, private orders, executions, positions, and account summaries.
- Shared order intents, events, execution deduplication, persistence, and recovery flows while preserving venue-specific behavior.
- Both venues can run at the same time; failure of one must not bring down the other.
- Read-only cross-venue views of orders, positions, accounts, and health.
- If an existing dashboard exists, incrementally add venue selection and data isolation. If none exists, do not build a full frontend solely for this release.
- Separate external validation results and evidence lists for each venue.

### 1.2 R2: Deribit FIX — Follow-On Mandatory Work

- Reuse FIX framing/codec/TLS/test utilities where reuse is appropriate.
- Implement a separate Deribit FIX dialect: Logon, heartbeat, order messages, reports, sequence handling, and recovery.
- Deribit-specific mocks, fixtures, and failure-scenario tests.
- When account/network access allows, perform real Testnet Logon, order, cancel, and report validation.
- If external conditions block validation, keep the implementation and local tests, explicitly record the blocker, and do not claim external FIX integration is complete.

### 1.3 Instrument Scope for the First Release

**Deribit trading support is initially limited to metadata-validated inverse perpetual contracts `BTC-PERPETUAL` and `ETH-PERPETUAL`.** Instruments outside the allowlist may be queried but not traded.

Dated futures: support discovery and persistence of metadata such as expiration, but trading is not required. Options, linear products, combination orders, spot-forward trades, equity/commodity derivatives, and other product types are out of scope for the first release. Do not apply inverse-perpetual quantity rules merely because another product has a similar name. [R4][R5]

Keep the currently supported Bybit product scope unchanged. Do not convert existing Spot functionality to Linear, and do not require this upgrade to complete support for every Bybit product category.

### 1.4 Explicit Exclusions

Do not implement strategies, automated arbitrage, automatic cross-venue re-submission, fund transfers, smart routing, portfolio margin engines, options pricing, a complete generic FIX engine, or HFT performance claims.

---

## 2. Phase 0: Audit the Existing Code First

Create `docs/upgrade/BASELINE_AUDIT.md` and record at least:

| Item | What to verify |
|---|---|
| Working tree | Current commit/branch and uncommitted changes; do not overwrite user work |
| Go | Module name, Go version, dependencies, actual main packages |
| CLI | `venuewire` or other entry points, flags, default venue, and output format |
| Bybit | Which REST/WS/FIX features have code and which have real Testnet evidence |
| Products | Actual Spot/Linear/Inverse scope, quantity units, fee handling |
| Orders | IDs, state updates, execution deduplication, cancel/fill race behavior |
| Storage | JSON/SQLite/other; schema version, writer model, locks, startup recovery |
| UI | Whether dashboard, API, push updates, and order forms exist |
| Operations | Configuration source, redaction, rate limiting, reconnect, shutdown flow |
| Tests | Unit/integration/race/build results and existing known failures |

**The baseline audit must not submit orders.** First run tests that require no credentials and do not modify account state.

Suggested commands; adapt them to the actual project:

```bash
git status --short
go list ./...
go test ./...
go vet ./...
# Run only when supported by the platform and toolchain; otherwise document the reason.
go test -race ./...
```

The build instructions must specify an explicit binary path rather than only `go build ./...`:

```bash
mkdir -p bin
go build -o ./bin/venuewire ./cmd/venuewire
```

Use the above main package only if it actually exists in the repository; otherwise use the real path and update the documentation.

### Baseline Protection

- Add or preserve regression tests for existing Bybit signing, order queries, executions, and fee calculations.
- If the existing dashboard already distinguishes execution volume from post-fee balance, preserve that distinction. Do not treat net credited amount as traded amount.
- Do not hard-code Testnet fee rates; use actual execution reports for fee amount and fee currency. [R28]
- When Deribit is disabled or credentials are missing, existing Bybit commands must continue to work independently.
- If category/account cannot be reliably inferred for old data, stop migration for those records and require an explicit mapping. Do not guess that everything is `linear`.

---

## 3. Architecture: Share Business Semantics, Not Incorrect Assumptions

### 3.1 Component Relationship

```text
Existing CLI / existing dashboard
            |
    Application services + explicit venue routing
            |
   Order intents / event processing / storage
        /                               \
Bybit Adapter                       Deribit Adapter
existing REST / WS                  HTTP JSON-RPC / WS JSON-RPC
existing FIX dialect                Deribit FIX dialect
        \                               /
     Shared health monitoring, test utilities,
              and read-only aggregation
```

Do not create one large interface that forces every transport to expose identical methods. Split by capability:

| Boundary | Responsibility |
|---|---|
| `InstrumentCatalog` | Discover instruments, metadata, valid units, and trading capabilities |
| `OrderCommands` | Place, edit, and cancel orders; return known state and any executions included in the response |
| `OrderQueries` | Individual orders, open orders, history, execution pagination |
| `AccountQueries` | Positions and balances while preserving currency and semantics |
| `MarketEventSource` | Public market data and order book |
| `PrivateEventSource` | Order, execution, and position updates |
| `VenueRecovery` | Recovery coordination for a venue/account |
| `Capabilities` | Support matrix by instrument × transport × operation |

Do not derive venue identity from symbols. Every write path must receive an explicit venue, environment, account, instrument, and transport.

### 3.2 Suggested New Areas

```text
internal/venue/                  # Shared boundaries; reuse existing domain if appropriate
internal/deribit/
    config.go
    rpc.go                       # JSON-RPC envelope, errors, request IDs
    http.go
    auth.go
    ws.go
    instruments.go
    orders.go
    accounts.go
    subscriptions.go
    normalize.go
    recovery.go
    fix/                         # Deribit-specific dialect
internal/deribitmock/
docs/upgrade/
testdata/deribit/                # Sanitized or synthetic data only
```

The existing Bybit directory structure may remain unchanged. Extract shared code only where both venues actually need it; do not build a framework for ten hypothetical future exchanges.

---

## 4. Configuration, Credentials, and Testnet Guardrails

### 4.1 New Configuration

Keep existing Bybit variables. Add separate Deribit configuration; do not share key/secret values:

```dotenv
# Example files must contain empty values only, never real credentials.
DERIBIT_ENABLED=false
DERIBIT_ENV=testnet
DERIBIT_ACCOUNT_ALIAS=deribit-test
DERIBIT_CLIENT_ID=
DERIBIT_CLIENT_SECRET=
DERIBIT_FIX_ENABLED=false
DERIBIT_FIX_SENDER_COMP_ID=venuewire
```

Recommended local defaults (project policy, not exchange requirements):

- New multi-venue order-entry paths default to preview-only; execution requires explicit confirmation.
- Only allowlisted `BTC-PERPETUAL` and `ETH-PERPETUAL` are tradable.
- A non-zero risk limit must be configured and validated before order submission; an unset value must not mean unlimited.
- Do not automatically modify account-level Cancel-on-Disconnect settings.
- When `DERIBIT_ENABLED=false`, do not initialize Deribit private connections.

Do not require existing Bybit users to rebuild their `.env` merely because these settings are added.

### 4.2 Allowed Remote Locations

| Purpose | Testnet endpoint |
|---|---|
| Deribit HTTP | `https://test.deribit.com/api/v2/{method}` |
| Deribit WS | `wss://test.deribit.com/ws/api/v2` |
| Deribit FIX | `fix-test.deribit.com:9883`, TLS |

Accounts and keys must belong to the separate Testnet environment. [R1][R2][R19]

Use exact hostname allowlists. Do not rely on checks such as `contains("test")`. Validate scheme, hostname, and port. Do not follow redirects that could send credentials to another host. Production endpoints, IP-address substitutions, and plaintext FIX connections must not be used as failure fallbacks.

Tests may inject `httptest` or a local TLS server; this is test dependency injection, not a production CLI flag that bypasses host safety.

### 4.3 Permissions and Secrets

Core query/trading operations should use minimum necessary permissions: account read and trade read/write. Only connection-scope COD modification should require the corresponding account-write permission. Withdrawal permissions must never be required. [R3][R13]

The following must never appear in logs, fixtures, error strings, dashboards, URL access logs, or reports: client secret, access token, refresh token, Authorization header, FIX Password, full Logon/auth frame.

FIX Logon responses may echo sensitive fields, so **inbound messages must also be redacted**. Secrets must not be passed through command-line flags. Bybit credentials previously pasted in conversation must not be copied into code or new documentation.

---

## 5. Identifiers, Instruments, and Quantity Units

### 5.1 Composite Keys

Do not use a standalone `order_id`, `symbol`, or `trade_id` as a global key.

At minimum use:

```text
MarketKey     = venue + environment + market_kind + native_instrument
AccountKey    = venue + environment + stable_account_alias
OrderKey      = AccountKey + native_order_namespace + native_order_id
ExecutionKey  = AccountKey + native_trade_namespace + native_trade_id
PositionKey   = AccountKey + MarketKey + native_position_side_or_index
```

`native_*_namespace` must cover the true identifier scope for that venue, for example currency/category/instrument. The account alias must be stable and map to one actual account. Rotating a key without changing the account must not create a second logical set of positions.

### 5.2 Instrument Metadata

Store raw metadata plus normalized fields for every instrument:

```text
venue / native_instrument / kind / contract_style
base_currency / quote_currency / settlement_currency
native_order_amount_unit / position_size_unit
contract_size / quantity_increment / minimum_order_amount
price_tick / tick_size_steps / expiration_timestamp
is_active / native_state / fetched_at / source
```

Discover instruments through `public/get_instruments` / `public/get_instrument`. Do not hard-code quantity steps or ticks. Metadata availability does not automatically grant trading capability; unsupported contract styles must reject order submission. [R5]

**`contract_size`, minimum order amount, and quantity increment are different concepts.** Do not reuse one field for all three just because values happen to coincide for one instrument. Preserve official semantics and create explicit tests for target instruments.

### 5.3 Do Not Apply Bybit Quantity Semantics to Deribit

For this release's target inverse `BTC-PERPETUAL` / `ETH-PERPETUAL` contracts, HTTP/WS `amount` is expressed in USD notional, not in BTC/ETH units. Other product types are not guaranteed to use the same unit. [R4]

Therefore this is forbidden:

```text
Bybit qty=1 -> send Deribit amount=1 unchanged
```

Internal requests must carry units, for example:

```text
instrument = BTC-PERPETUAL
native_amount = 100
native_amount_unit = USD
```

This is a semantic example only, not a fixed order value. Actual quantity and price must be validated against current metadata, balances, and local risk limits.

For the first release, do not send both `amount` and `contracts`. Choose one clearly validated representation. Enable other representations only after conversion and protocol tests are complete.

### 5.4 Precision

Reuse the project's existing reliable decimal type. If none exists, adopt fixed precision or a mature decimal implementation. Do not use `float64` for quantity validation, monetary values, fees, or position accumulation.

Deribit JSON numbers must retain precision via `json.Number` or equivalent decimal decoding. When transmitting, emit valid JSON numbers; do not send all numeric parameters as JSON strings merely because internal storage uses strings.

When quantity is invalid, reject it and provide a valid suggestion. Do not silently round upward into a larger order. For stepped tick schedules, add tests immediately above and below each boundary.

### 5.5 Fees and Executions

Persist `gross_fill_amount`, `fee_amount`, `fee_currency`, and `liquidity_role`. Fees may include rebates, so do not arbitrarily take absolute values.

Spot net balance change is not equal to execution volume; derivatives execution volume is also not equivalent to acquiring that amount of spot asset. Keep fees in different currencies separate. Without a timely FX rate, do not output a seemingly precise aggregate USD fee. [R4][R28]

---

## 6. Deribit JSON-RPC and Authentication

### 6.1 Shared RPC Layer

Deribit provides JSON-RPC over HTTP/WebSocket. HTTP supports GET and POST; this project should prefer POST JSON bodies for private operations so credentials or trading parameters do not appear in URLs. [R1][R2]

Use the official method path `/api/v2/{method}`. Fixtures and external tests must confirm the actual envelope. Do not blindly reproduce GET-body examples from documentation generators.

RPC requirements:

- Parse `jsonrpc`, `id`, `result`, `error.code`, `error.message`, and `error.data`.
- HTTP 200 does not imply business success.
- Limit maximum response/frame size; oversized input is a protocol error.
- WS request IDs must be unique within a connection generation; the pending-request map must be bounded and cleaned up on timeout.
- Responses on the same WS may arrive out of order; correlate by ID, not FIFO.
- Route `method=subscription`, `method=heartbeat`, and normal RPC responses separately.
- Increment connection generation after reconnect; late responses from an old connection must not complete requests on a new connection.
- Define typed errors: `Auth`, `Permission`, `Validation`, `RateLimited`, `Unavailable`, `OutcomeUnknown`, `Unsupported`.

### 6.2 Token Management

The first release should use `public/auth` with `client_credentials` and support renewal through `refresh_token`. HTTP uses a Bearer header; WS uses the authenticated connection/token scope according to documented behavior. Do not assume an HTTP token can always be reused across every connection. [R3][R6]

Requirements:

1. Calculate expiry from returned `expires_in`; do not hard-code a one-year lifetime.
2. Token refresh for the same scope must be single-flight to avoid every goroutine refreshing at once.
3. Atomically replace new access/refresh tokens; secrets stay only in controlled memory.
4. If refresh fails, re-authentication is allowed, but it must not automatically re-submit an order whose outcome is unknown.
5. Inspect granted scopes and surface missing permissions immediately; do not mask permission errors by reconnecting repeatedly.
6. Explicitly distinguish connection scope, session scope, and the lifetime of the token in use.

### 6.3 Rate Limits

Deribit uses credit-based rate limits; `10028 / too_many_requests` may also cause disconnects. Do not apply Bybit rate-limit headers or requests-per-second assumptions. [R7]

Use an independent limiter per venue/account covering HTTP, WS RPC, and other activity consuming the same account quota. Preserve capacity for subscriptions, heartbeat, and recovery control traffic. After rate limiting, use cooldown + backoff + jitter; do not create a login storm. Cache metadata instead of fetching instrument lists on every tick.

---

## 7. Deribit HTTP Features

The following method mapping is required for this release. Confirm parameters, scope, pagination, and product support against the official documentation and record them in `docs/upgrade/PROTOCOL_NOTES.md`. [R4][R5][R14–R18][R29–R33]

| Capability | Method |
|---|---|
| Connectivity/time diagnostics | `public/test`, `public/get_time` |
| Instrument information | `public/get_instruments`, `public/get_instrument` |
| Market snapshot | `public/ticker`, `public/get_order_book` |
| Authentication/refresh | `public/auth` |
| Buy/sell | `private/buy`, `private/sell` |
| Cancel/edit | `private/cancel`, `private/edit` |
| Order | `private/get_order_state`, `private/get_open_orders_by_instrument` |
| Search by label | `private/get_order_state_by_label` |
| Order history | `private/get_order_history_by_instrument` |
| Execution queries | `private/get_user_trades_by_instrument`, `private/get_user_trades_by_order` |
| Positions | `private/get_positions` |
| Account | `private/get_account_summary` |

Core demo: start private WS -> place a small limit order via HTTP -> process order/execution data in the HTTP response -> receive private events -> edit or cancel -> query to confirm.

A Deribit order response may already contain both `order` and `trades`; do not reduce every venue response to a Bybit-style pure ACK. Executions embedded in the response and later push events must pass through the same deduplication pipeline. [R4][R27]

---

## 8. WebSocket Market Data, Trading, and Private Data

### 8.1 Connection Separation

Use two Deribit WS connections by default: one for public market data and one for private trading/events. This is a project isolation choice to prevent high-volume order-book traffic from blocking private execution reports. Account rate limits remain shared; do not treat additional connections as additional quota.

### 8.2 Public Subscriptions

Use `public/subscribe` for first-release instruments:

```text
ticker.BTC-PERPETUAL.100ms
trades.BTC-PERPETUAL.100ms
book.BTC-PERPETUAL.100ms
```

ETH may be added through the allowlist. Use `100ms` by default; do not make permission-gated `raw` feeds the default. [R1][R8]

### 8.3 Order Book Correctness

`book.{instrument}.{interval}` provides an initial snapshot and incremental updates. Deltas contain `change_id` / `prev_change_id`, and entries include `new`, `change`, `delete`. [R8]

Mandatory behavior:

- Snapshot atomically replaces the entire local book for that instrument.
- Apply a delta normally only when `prev_change_id == last_change_id`; do not require IDs to increment by exactly one.
- Duplicate messages may be dropped and counted; out-of-order or gapped data must mark the book untrusted.
- After a gap, stop using the book for order preview/risk validation; resubscribe and wait for a fresh snapshot.
- On reconnect, discard deltas belonging to the previous connection generation.
- Do not attach post-gap deltas to an arbitrary HTTP snapshot unless a verified sequence-bridging algorithm has been implemented.
- Set per-instrument book size and memory limits; if the full book exceeds limits, stop or degrade the display rather than silently truncating while claiming it is complete.

### 8.4 Heartbeat

Use `public/set_heartbeat` for application-layer heartbeat. On `test_request`, call `public/test`. WS control ping/pong does not replace this behavior. [R9]

Heartbeat, RPC responses, and private control messages need reserved capacity so market-data queue saturation cannot block them. Lack of executions does not mean the private connection is stale; health checks should combine heartbeat, read/write state, and recovery results instead of looking only at the last trade time.

### 8.5 Private Subscriptions

For the first release, use `private/subscribe` with:

```text
user.changes.BTC-PERPETUAL.100ms
user.changes.ETH-PERPETUAL.100ms
```

Subscribe only to enabled instruments. Parse the `orders`, `trades`, and `positions` arrays in each message and do not assume only one element per array. [R10]

Use `private/get_account_summary` for startup and periodically throttled balance snapshots. Add `user.portfolio.{currency}` only if real-time balance updates are needed. Do not treat derivatives positions as spot balances. [R18][R26]

### 8.6 WS Order Entry

WS place/edit/cancel is mandatory for R1, not just market-data consumption. Reuse the methods/business validation from §7, but correlate requests by WS request ID. [R2][R4]

Private events may arrive before the RPC response. The same order may also be observed through HTTP queries, WS notifications, and FIX. All sources must be safe to replay into the same event reducer.

### 8.7 Cancel on Disconnect (COD)

Deribit COD applies to orders created on a specific connection; it is not an account-wide guarantee across all transports. HTTP orders do not automatically gain the same protection because another WS disconnects. A normal `private/logout` should not be assumed to trigger COD. [R13]

Rules for this release:

- Query and display the effective COD setting at startup.
- Enable connection-scope COD only when explicitly requested by the user; do not automatically modify account scope.
- Automated WS trading tests may require COD to be enabled; if permission is missing, mark the test blocked rather than silently modifying account permissions.
- Record the transport and connection generation used to create every order.
- Even with COD enabled, disconnect does not guarantee synchronous cancellation; still query orders and executions after disconnect.
- On shutdown, if cancellation is desired, explicitly cancel orders owned by this application and confirm the result. Do not rely on Logout side effects.

---

## 9. Order Intents, Idempotency, and State Machines

### 9.1 Keep Three Identifier Types Separate

```text
IntentID      = unique persisted business intent owned by this application
RequestID     = correlation ID for one HTTP/WS/FIX attempt
NativeOrderID = exchange-issued order ID
```

Create new IntentIDs using short non-PII values such as `cx-` + a 26-character random ID. Map them to Bybit `orderLinkId` or Deribit `label` while preserving the native values.

**Deribit `label` may map to multiple orders. It is not guaranteed by the venue to be unique and is not an exactly-once mechanism.** If a label query returns multiple orders, mark the result anomalous and do not arbitrarily choose the first record. [R11][R12]

### 9.2 Write-Ahead Intent

Persist before submission: venue, account, instrument, side, native quantity and unit, price, options, IntentID, attempt, and creation time.

The same IntentID must not be submitted concurrently by different goroutines/CLI processes. Depending on the existing architecture, use a single writer, file lock, or database transaction. Do not pretend an in-memory mutex provides cross-process safety.

### 9.3 Unknown Outcomes

```text
persist intent -> submission attempted -> timeout/disconnect -> OutcomeUnknown
                                                |
                                      query order/execution/history
```

A timeout must not be treated as rejection. Do not automatically retry a WS order over HTTP, and do not submit the same economic intent to another venue.

Recovery priority: NativeOrderID; if unknown, use label plus full-parameter matching; combine private events and recent/history queries. Temporary absence from queries does not prove the order was never accepted. If bounded reconciliation cannot determine the result, remain in `OutcomeUnknown / NeedsReview` and block further writes for that intent.

Do not automatically create a second order for an unknown outcome. The user must resolve the state before explicitly creating a new intent.

### 9.4 Separate ExchangeState and CommandState

```text
ExchangeState:
    Unknown / Open / PartiallyFilled / Filled / Cancelled / Rejected

CommandState:
    Idle / Submitting / Amending / Cancelling / OutcomeUnknown
```

A Deribit order that is `open` with `0 < filled_amount < amount` may normalize to `PartiallyFilled`; preserve `raw_order_state`. Unsupported untriggered/other states should be stored and marked unsupported, not incorrectly mapped to Open. [R4][R29]

A rejected cancellation does not mean the original order is Rejected. Fills may occur while cancellation is pending; successful cancellation may still preserve partial executions. `Cancelled` must not reset already-filled quantity to zero.

### 9.5 Event Merge

- Deduplicate executions by stable ExecutionKey and persist dedup records/cursors.
- Cumulative order filled quantity and the sum of individual executions are two observations; never add them together again.
- If an HTTP response already contains an execution, persist it immediately; the same WS/FIX execution must not add volume or fees again.
- Determine event recency using native revision/time/event type together; do not sort only by state name.
- Amendments can change total quantity; `max(cum_qty)` alone is not a complete order reducer.
- If conflicting observations have no reliable ordering, mark a discrepancy and query the venue instead of silently overwriting.
- Keep account-position snapshots separate from locally derived execution deltas to avoid applying the same execution twice.

---

## 10. Recovery and Pagination

### 10.1 Recover Each Venue/Account Independently

Recovery may be triggered by startup, private WS reconnect, FIX session changes, or manual commands. Only one recovery run per account may be active; new triggers may be coalesced but must not queue indefinitely.

States:

```text
Disconnected -> Authenticating -> Subscribing -> Recovering -> Ready
                                    \---------> Degraded / NeedsReview
```

During recovery, block new orders that increase exposure for that account. Allow cancellation only when it has a known ID, supported capability, and sufficiently fresh data. A circuit breaker on one venue must not block a healthy venue.

### 10.2 Do Not Rely Only on Open Orders

An order missing from the open-order list may have been filled, cancelled, expired, skipped by pagination, or not yet indexed. It must not be automatically marked Cancelled.

Recovery should use at least: local unfinished intents, open orders, individual order state, recent/history orders, trade history, positions, and account summary. [R14–R18]

### 10.3 Bridging Snapshots and Live Events

1. After login and subscription succeed, begin buffering private events in a bounded recovery buffer and record connection generation.
2. Fetch query snapshots and execution data starting from the last persisted cursor.
3. Persist deduplicated executions first, merge order/position observations, then replay buffered live events.
4. Re-query still-conflicting active orders; do not assume several HTTP calls form an atomic snapshot.
5. Enter Ready only when required queries succeed, the buffer is caught up, and there are no unknown states that affect safe order submission.
6. If the recovery buffer overflows or another disconnect occurs, remain Degraded and restart recovery. Do not drop private events and still display Ready.

This release does not claim a cross-venue atomic snapshot. Aggregated output must include each venue's `as_of`, completeness, and data age.

### 10.4 Pagination and Historical Data

Deribit has recent and `historical=true` query paths, and historical indexing may be delayed. Long outages cannot be recovered using only default recent windows. [R16]

- Implement method-specific pagination; do not invent one universal cursor for every API.
- For executions, prefer instrument-scoped `trade_seq` or an officially supported cursor and persist the cursor scope; do not treat it as globally continuous across the exchange. [R15]
- Time-range pagination must handle multiple executions in the same millisecond; do not page with only `last_timestamp + 1`.
- Query overlapping boundaries and deduplicate by TradeID until `has_more` / continuation completes.
- Order history queries must include cancelled-without-fill orders where supported by the method. [R14]
- If all data cannot be retrieved, return `Partial` plus the missing range; never label incomplete data as complete.
- Advance recovery cursors only after durable persistence succeeds.
- Recovery must not create new orders, auto-close positions, or cancel orders not owned by this application.

---

## 11. Backward-Compatible Storage Migration

Reuse the existing storage technology. Do not force Kafka, a database, or a new service merely because a second venue is added.

If the current JSON structure has no version, add `schema_version`. Migration must:

1. Back up existing data; never overwrite the only copy.
2. Dry-run the venue/environment/account/category values that will be added.
3. Populate Bybit identity fields only from verifiable baseline information; stop on unknown values.
4. Atomically write the new format and keep a rollback path.
5. Re-running migration must produce the same result without duplicating orders/executions.
6. Reject unsupported future schema versions explicitly instead of starting with empty state.

The following must be persisted: intents, native order-ID mappings, executions, fees, recovery cursors, required order state, account mappings, and schema version. Public market data does not need persistence.

Multiple CLI/dashboard writers must not cause lost updates. Atomic rename of a single file is not a cross-process transaction lock; use an explicit single-writer model or lock protocol.

---

## 12. Multi-Venue Views and Risk Boundaries

### 12.1 Required Read-Only Views for R1

Display venue, environment, account, native instrument, contract style, native quantity, unit, direction, mark/index, currency, last update, connection state, and recovery state.

`--venue all` is read-only and may be used for queries, aggregation, and recovery. It must be rejected for writes such as place/amend/cancel. If a cancellation ID belongs to another venue, return an explicit error and do not try the same ID on the other venue.

### 12.2 Do Not Add Incompatible Values

Do not add Bybit BTC quantity directly to Deribit USD notional. Likewise, do not combine BTC balance, USDT balance, and USD PnL into one generic `balance`.

Default display should show native values and grouped subtotals. If estimated notional exposure is added:

- Each instrument must have a validated conversion rule.
- Display valuation price, currency, timestamp, FX source, and completeness.
- Without a valid USDT/USD or other FX rate, do not silently assume 1:1 and output an authoritative-looking total.
- For inverse contracts, `USD notional / price` may be shown only as a clearly labeled base-asset-equivalent estimate, not as full delta or margin that can be netted across venues.
- Venue margin is independent; offsetting notional directions do not imply one venue cannot liquidate.
- Unsupported positions must remain visible; do not exclude them and still claim exposure is complete.

This is a read-only observability feature, not an automated hedge/risk engine.

### 12.3 Failure Isolation

Authentication/rate-limit/queue/reconnect failure on one venue must affect only that venue's state. `all` queries may return partial success but must mark the aggregate `Partial`; never display failed-venue positions as zero.

---

## 13. CLI and Existing Dashboard

### 13.1 Compatibility

Keep the existing binary name. You may add a global `--venue bybit|deribit`; existing commands without the flag must preserve previous Bybit behavior. Generic aliases are optional and must not break existing scripts.

The following are target capability examples and do not imply these subcommands already exist:

```bash
./bin/venuewire --venue deribit doctor
./bin/venuewire --venue deribit instruments --currency BTC --kind future
./bin/venuewire --venue deribit market trades --instrument BTC-PERPETUAL
./bin/venuewire --venue deribit market orderbook --instrument BTC-PERPETUAL
./bin/venuewire --venue deribit private-stream --instrument BTC-PERPETUAL
./bin/venuewire --venue deribit orders list --instrument BTC-PERPETUAL
./bin/venuewire --venue deribit positions --currency BTC
./bin/venuewire --venue deribit reconcile
./bin/venuewire --venue all status
./bin/venuewire --venue all portfolio
```

`doctor` may perform read/connectivity diagnostics but must not place orders, modify settings, change COD, cancel, or close positions.

### 13.2 New Order Submission Flow

New multi-venue order entry should use two stages: `plan` -> explicit `execute`. Reuse an existing safe confirmation interface when available rather than duplicating it.

A plan should display at least: venue/Testnet/account, instrument, quantity and unit, price, valid tick, estimated notional, limits, transport, COD coverage, metadata/price timestamp, and expiration.

A `min-valid` quantity strategy and passive-price strategy may be provided, but calculations must use metadata and current market data. Do not hard-code today's BTC price or promise a passive order will not fill. Limit prices must remain within exchange rules.

`execute` must revalidate plan freshness, instrument status, data health, and risk limits. Reject expired plans or ambiguous quantity conversion. Explicitly choose HTTP or WS; never switch transports and re-submit because of failure.

### 13.3 When a Dashboard Exists

- Preserve the existing Bybit default and add venue/account selection.
- Every order, execution, and position row must include venue, and frontend row keys must use composite identifiers.
- Forms must display the actual unit, e.g. `amount (USD)`, rather than labeling every field as "BTC quantity."
- `all` mode is read-only; every write requires reconfirming a single venue.
- Display `Recovering / Stale / Partial / NeedsReview`, not only a green Connected indicator.
- Reuse the existing push architecture; do not create a complete set of venue-private connections for every browser tab.
- Logs, internal errors, and tokens must not leak through UI APIs.

If no dashboard exists, completing the CLI is sufficient; do not expand scope by building a new one.

---

## 14. Deribit FIX: Separate Dialect — Do Not Just Change the Host

### 14.1 Version and Documentation

This release targets the official currently documented **production/classic FIX 4.4 subset**, while all actual connections remain Testnet-only. `production` here refers to the current protocol-documentation branch and does not authorize real-money endpoints. [R19]

Do not mix `/upcoming/` or Starbase protocol details into the current codec. If Testnet has already migrated to a different version, first verify through non-trading diagnostics, record the version/documentation mapping, then implement the correct dialect. When version is unknown, order entry is prohibited.

### 14.2 What May Be Shared and What Must Not Be Shared

May be shared: SOH framing, BodyLength, CheckSum, TLS transport, injectable clock/randomness, session test utilities.

Must not be directly reused: Bybit RSA authentication, Bybit custom tags, OrderQty units, ClOrdID report semantics, resend behavior, sequence-reset rules, or cancel-on-disconnect behavior. [R19–R25][R34]

### 14.3 Logon

Use TLS at `fix-test.deribit.com:9883`; `TargetCompID=DERIBITSERVER`. Minimum authentication logic: [R19][R20]

```text
nonce_bytes = cryptographically_secure_random(32 bytes)
nonce_b64   = Base64(nonce_bytes)
timestamp   = strictly_increasing_unix_ms
RawData(96) = decimal(timestamp) + "." + nonce_b64
Username(553) = client_id
Password(554) = Base64(SHA256(bytes(RawData) || bytes(client_secret)))
```

This is not Bybit RSA authentication and is not HMAC-SHA256. Do not encode the SHA256 digest as a hex string before Base64 when the protocol expects raw digest bytes.

Use injectable clock and randomness in tests. Test retries within the same millisecond, clock rollback, key changes, rejected login, and redaction of sensitive responses. A safe timestamp watermark may be persisted but must never contain secrets.

Explicitly configure `HeartBtInt` and COD policy. Do not assume omitted-tag defaults are identical across all accounts.

### 14.4 Session and Recovery

Current Deribit documentation includes `ResendRequest(2)` and `SequenceReset(4)`, unlike Bybit's no-standard-resend recovery path. Deribit reset responses and sequence semantics are venue-specific. [R21][R22]

Requirements:

- Implement a separate `DeribitSessionPolicy`; do not apply Bybit's "reconnect on gap and reset everything to 1" logic to the same session model.
- Recognize Heartbeat, TestRequest, Logout, Reject, ResendRequest, and SequenceReset.
- Validate actual outbound sequence behavior, server resend requests, and the rule that reset may only advance sequence as applicable.
- Do not assume inbound sequence semantics equal outbound sequence semantics; record the Logon options and ordering guarantees in use.
- Do not assume Deribit provides permanent historical replay across sockets; after re-login, still reconcile business state through HTTP.
- On sequence anomalies, fail closed: pause new orders, record the gap, perform verified session recovery or reconnect/re-login, then reconcile orders.
- Do not automatically replay a `NewOrderSingle` whose result is unknown. If safe resend handling is not implemented, explicitly reject that recovery branch and re-login/reconcile instead of claiming full FIX replay support.
- This limited support scope must appear in the capability matrix and README, not be hidden behind generic FIX-engine defaults.

### 14.5 Orders and Reports

At minimum handle: [R23–R25]

```text
NewOrderSingle       35=D
OrderCancelRequest   35=F
OrderCancelReplace   35=G
ExecutionReport      35=8
OrderCancelReject    35=9
```

Important differences:

- In Deribit current ExecutionReport semantics, `ClOrdID(11)` must not always be treated as the original client ID; correlate using `OrderID(37)`, `OrigClOrdID(41)`, and other relevant fields. [R25]
- JSON `amount` and FIX `OrderQty(38)` must not be assumed to share the same unit. Obtain multiplier/contract details from FIX SecurityList or the corresponding instrument specification and prove consistent conversion between input, reports, and JSON reconciliation before enabling FIX order entry. [R23][R25]
- Reports may serve both order-state and fill-notification purposes. Fully implement the selected fill-reporting mode. The officially supported `ReportFillsAsExecReports(9015)` mode may be used, but fixtures and external responses must confirm it; do not lose fills simply because the parser does not support groups. [R20]
- The codec must not collapse repeating groups into a simple `map[tag]value`. Preserve unknown tags; unknown groups must be explicitly rejected/degraded rather than silently misparsed.
- If there is no verified one-to-one trade-ID mapping between FIX and JSON, do not guess identity from price + quantity + millisecond. Treat FIX as order-state evidence and use canonical JSON trade IDs for accounting deduplication where reconciliation requires it.

### 14.6 Validation Levels

```text
IMPLEMENTED           code exists
LOCAL_TESTED          fixture/mock tests passed
TESTNET_LOGON         real Testnet login succeeded
TESTNET_ORDER_FLOW    real order/cancel/report flow reconciled successfully
```

Track every level separately. Successful Logon does not prove order entry works; mock success does not prove Deribit accepts the dialect.

---

## 15. Observability, Failure Isolation, and Shutdown

Every structured log must include venue, environment, account alias, transport, connection generation, method/topic, IntentID/order ID, duration, and error class. Sensitive fields must always be redacted.

At minimum observe:

- Per-venue RPC success/failure/latency, rate limiting, and token refresh.
- WS reconnects, pending RPC count, queue depth, order-book gaps.
- Private-data state: Recovering/Ready/Degraded.
- Duplicate executions, unknown submission outcomes, reconciliation discrepancies, and last successful reconciliation.
- FIX session state, sequence anomalies, rejects, Logon validation level, and order-flow validation level.

Queues, goroutines, pending maps, and recovery buffers must be bounded. Private data must never be silently dropped; overload must mark the venue unreliable and trigger recovery/degradation.

Measure RPC round-trip using the local monotonic clock. Differences between exchange timestamps and local receipt time may be labeled estimated event age, not precise network latency unless clocks are synchronized.

SIGINT/SIGTERM flow: stop new intents -> persist unknown in-flight write states -> cancel application-owned orders only if explicitly configured -> persist state/cursors -> cleanly close connections, all under a bounded timeout. Failure to cancel or flush must not be reported as "all orders cleared."

---

## 16. Development Phases and Gates

| Phase | Work | Exit criteria |
|---|---|---|
| 0 | Audit, baseline, regression tests | Audit complete; existing failures distinguished from new failures |
| 1 | Venue routing, composite keys, config, required migration | No Bybit regression; disabled Deribit does not affect Bybit |
| 2 | Deribit metadata, HTTP RPC, auth, account queries | Precision/permission/token/rate-limit tests pass; read-only smoke test |
| 3 | Public WS, book, heartbeat, private WS | Gap/reconnect/event-routing/multi-element-message tests pass |
| 4 | HTTP + WS orders, intents, deduplication | Limit/edit/cancel/unknown-outcome tests; controlled external validation |
| 5 | Persistence, restart, historical pagination, recovery | Recoverable across disconnect/index delay/pagination overlap/same-ms trades |
| 6 | Both venues running, CLI/existing UI | Correct isolation + read-only aggregation with units/freshness |
| 7 | R1 acceptance | Core tests green; external evidence present or blockers explicitly recorded |
| 8 | Deribit FIX dialect/mock | Auth/session/message/unit tests pass |
| 9 | FIX Testnet validation | Complete when possible; otherwise record exact blocker and never fake success |
| 10 | Final docs and handoff | Build/demo/capability matrix/known limits/results complete |

At the end of every phase update status with: changed files, test commands, results, failures, reasons for deviations, and next steps. Missing external access must not block offline implementation, and it must not make external acceptance count as passed.

---

## 17. Mandatory Test Matrix

CI must not use external credentials; external Testnet tests are separate opt-in suites.

| ID | Scenario | Required result |
|---|---|---|
| B01 | Deribit disabled / missing key | Existing Bybit commands/tests still work |
| B02 | Existing Spot/fee case regression | Execution volume, net balance, and fee currency remain distinct |
| B03 | Old JSON/DB data upgrade | Backup, repeatable migration, no duplicates, reversible |
| N01 | Same numeric order ID from both venues | No overwrite and no cross-venue cancellation |
| N02 | Same instrument but different account/category | Data and positions fully isolated |
| A01 | Token expiry with concurrent requests | Single refresh, atomic replacement, no auth storm |
| A02 | Auth/refresh/FIX response enters logs | Secret/token/password absent |
| A03 | Wrong environment/redirect/malicious hostname | Rejected before credentials are sent |
| R01 | HTTP 200 + RPC error | Business failure, not false success |
| R02 | Out-of-order WS responses mixed with notifications | Correct pending-request correlation |
| R03 | Late response from old connection | Cannot complete a new-connection request |
| R04 | Private order event arrives before ACK | One local order only, merged later |
| M01 | USD amount confused with BTC quantity | Rejected locally, not sent |
| M02 | Stepped tick / invalid increment | Correct validation, no silent upward rounding |
| M03 | High-precision JSON number | No float64 rounding affecting monetary values |
| M04 | Unsupported contract / expired metadata | Query allowed, trading rejected |
| W01 | Heartbeat test_request | `public/test` called promptly |
| W02 | Snapshot + delta + duplicate | Correct book without duplicate application |
| W03 | Broken prev_change_id chain | Book becomes stale until fresh snapshot |
| W04 | Private queue/recovery buffer full | Venue degraded, no silent loss |
| O01 | Same IntentID submitted by two processes | At most one write path proceeds |
| O02 | Venue accepts order then socket disconnects | OutcomeUnknown; no automatic resubmit |
| O03 | Label query returns multiple orders | NeedsReview; do not pick first |
| O04 | Same execution appears in RPC response and WS | Execution/fee accounted once |
| O05 | Partial fill then cancel | Cancelled while preserving filled quantity |
| O06 | Fill races with cancel/amend | Preserve latest reliable venue state; do not zero fill |
| O07 | Amend fails | Original order not changed to Rejected |
| C01 | Crash after submission before ACK persisted | Restart reconciles persisted intent; does not create second order |
| C02 | Pagination overlap + same-ms multiple trades | No missed or duplicated executions |
| C03 | Removed from recent index but not yet in history | Remain unknown/partial with bounded retry |
| C04 | Order cancelled externally while application offline | Restart query repairs local state |
| C05 | Execution/reconnect during recovery | Old snapshot cannot overwrite newer reliable data |
| C06 | Reconcile run repeatedly | Same result; no extra trading |
| D01 | WS COD enabled while HTTP order exists | Do not assume HTTP order is also cancelled |
| D02 | Graceful logout vs actual disconnect | Distinguish policy from actual cancellation result |
| V01 | Bybit network/auth failure | Deribit remains running and isolated |
| V02 | Deribit rate-limit reconnect | Does not reconnect Bybit and does not create login storm |
| V03 | One venue incomplete | Portfolio marked Partial, not zero |
| V04 | BTC/USD/USDT native values | Not directly added; no aggregate when FX source is insufficient |
| F01 | Deribit FIX Logon fixture | Correct digest, timestamp, nonce, and tags |
| F02 | Bybit + Deribit FIX tests together | Dialects do not contaminate each other |
| F03 | Split/combined frames/checksum/groups | Parsed or explicitly rejected; no panic or missed fill |
| F04 | ResendRequest/SequenceReset | Deribit policy used, not Bybit policy |
| F05 | ExecReport tag11 replaced | Still correlates local intent through tag37/other mapping |
| F06 | JSON/FIX quantity conversion unclear | Live FIX order prohibited and marked blocked |
| F07 | FIX execution reconciled with JSON | Deduplicate only with verified IDs, never price/time guessing |
| S01 | SIGTERM with in-flight orders | Persist unknown state; no unbounded wait |
| S02 | Mainnet/withdrawal/`all` venue writes | Explicitly rejected |

Run `go test -race ./...` on supported environments. Add fuzz seeds and bounded fuzz tests for FIX framing/RPC decode; tests must not create unbounded external network connections.

---

## 18. External Testnet Validation and Evidence

### 18.1 Separate Opt-In Flags

Recommended additions following existing integration-test conventions:

```dotenv
RUN_DERIBIT_READ_TESTS=0
RUN_DERIBIT_TRADING_TESTS=0
RUN_DERIBIT_FIX_TESTS=0
```

The read flag must never place orders. Enabling FIX tests does not automatically authorize trading; write operations still require the trading flag and explicit confirmation. Users provide new credentials locally; this document provides no credential values.

### 18.2 R1 External Validation

1. Instrument metadata, time, token, account summary, and positions can be read successfully.
2. Public WS and private WS operate simultaneously; heartbeat and resubscription are demonstrated.
3. Place one metadata-valid small limit order via HTTP, receive private events, edit/cancel it, and query to confirm.
4. Create a separate new intent via WS and complete the same lifecycle.
5. Within configured Testnet risk/price limits, observe at least one real Testnet execution and reconcile it against order/trade history and local accounting.
6. Modify an order through another explicit action while the application is offline, restart, and repair state.
7. Run Bybit and Deribit together; inject a single-venue fault and confirm the other remains available.

If Testnet liquidity prevents step 5, mark it `BLOCKED_LIQUIDITY`; mock fill tests remain mandatory but cannot replace real execution evidence. Do not disable risk controls, bypass self-trade protection, increase notional excessively, switch to Mainnet, or chase prices without limit just to obtain a fill.

Cleanup may only touch orders explicitly owned by this test. If a position is accidentally created, report it first. A bounded reduce-only cleanup may be executed only with prior authorization; never close the entire account.

### 18.3 Evidence Format

Create `docs/upgrade/VALIDATION_REPORT.md`. For each item record:

```text
Test ID / UTC time / commit / build
Venue / Testnet / account alias / transport
Instrument / amount + unit / metadata timestamp
Command / expected result / actual result
Native order ID / trade ID (may be masked before sharing)
Result: PASS | FAIL | BLOCKED | NOT_RUN
Evidence: sanitized log / fixture / screenshot (if UI exists)
```

Never attach full auth frames, tokens, or account-identifying secrets.

### 18.4 Definition of Completion

**R1 implementation complete:** required offline tests and Bybit regressions pass; HTTP, WS, intent/recovery, and multi-venue isolation are implemented; docs and build instructions are usable.

**R1 external validation complete:** the core Testnet validation above has real evidence. If any item is BLOCKED, state "implementation complete, external validation incomplete" rather than saying everything is complete.

**R2 FIX external integration complete:** requires more than TCP/TLS. It must include real Logon and a real order/report flow that can be reconciled through JSON queries.

---

## 19. Deliverables

Codex final delivery:

```text
Incremental code and tests in the existing project
Executable build command and binary path
Updated README.md
Updated .env.example / .gitignore
Updated IMPLEMENTATION_STATUS.md

docs/upgrade/BASELINE_AUDIT.md
docs/upgrade/PROTOCOL_NOTES.md
docs/upgrade/MIGRATION.md
docs/upgrade/DEMO.md
docs/upgrade/VALIDATION_REPORT.md
docs/upgrade/CAPABILITY_MATRIX.md
docs/upgrade/CODEX_HANDOFF.md
```

`CODEX_HANDOFF.md` must be written in Traditional Chinese and explain: what changed, how to build/start, whether Bybit remained intact, which Deribit features were tested against real Testnet, the FIX validation level, blockers, and next steps. Do not automatically mark every template checkbox complete.

**The final project should demonstrate two real Testnet integrations with order lifecycles plus explainable and testable failure recovery. Merely connecting to two URLs is not enough to call it a multi-venue trading platform.**

---

## 20. Official Sources and Verification Rules

The following sources were reviewed on 2026-09-09. Re-check the relevant method during implementation and preserve the verification date, current/upcoming documentation branch, and sanitized response evidence. Use official documentation for protocol confirmation; example values are not permanent instrument specifications.

- **[R1]** Deribit Quickstart: interfaces, independent Testnet, endpoints, and examples.
  https://docs.deribit.com/articles/deribit-quickstart
- **[R2]** Deribit JSON-RPC protocol: HTTP/WS, envelope, transport constraints.
  https://docs.deribit.com/articles/json-rpc-overview
- **[R3]** Authentication/public auth: tokens, scope, and refresh.
  https://docs.deribit.com/articles/authentication
  https://docs.deribit.com/api-reference/authentication/public-auth
- **[R4]** Buy: amount semantics, order/trades response, order options.
  https://docs.deribit.com/api-reference/trading/private-buy
- **[R5]** Instrument metadata.
  https://docs.deribit.com/api-reference/market-data/public-get_instruments
  https://docs.deribit.com/api-reference/market-data/public-get_instrument
- **[R6]** Connection management: scope, heartbeat, lifecycle.
  https://docs.deribit.com/articles/connection-management-best-practices
- **[R7]** Rate limits: credit model, 10028, account quota.
  https://docs.deribit.com/articles/rate-limits
- **[R8]** Order book snapshot/delta/change ID.
  https://docs.deribit.com/subscriptions/orderbook/bookinstrument_nameinterval
- **[R9]** WebSocket heartbeat/test_request.
  https://docs.deribit.com/api-reference/session-management/public-set_heartbeat
- **[R10]** Private changes: orders/trades/positions.
  https://docs.deribit.com/subscriptions/user/userchangesinstrument_nameinterval
- **[R11]** Query multiple recent orders by label.
  https://docs.deribit.com/api-reference/trading/private-get_order_state_by_label
- **[R12]** Edit by label: valid only when exactly one open order matches.
  https://docs.deribit.com/api-reference/trading/private-edit_by_label
- **[R13]** Cancel-on-Disconnect settings and scope.
  https://docs.deribit.com/api-reference/session-management/private-enable_cancel_on_disconnect
  https://docs.deribit.com/api-reference/session-management/private-get_cancel_on_disconnect
- **[R14]** Order history, cancelled-unfilled orders, pagination.
  https://docs.deribit.com/api-reference/trading/private-get_order_history_by_currency
- **[R15]** Execution query by instrument and pagination.
  https://docs.deribit.com/api-reference/trading/private-get_user_trades_by_instrument
- **[R16]** Recent/historical queries and indexing delay.
  https://docs.deribit.com/articles/accessing-historical-trades-orders
- **[R17]** Derivatives positions.
  https://docs.deribit.com/api-reference/account-management/private-get_positions
- **[R18]** Account summary.
  https://docs.deribit.com/api-reference/account-management/private-get_account_summary
- **[R19]** Current FIX overview, Testnet TLS, headers.
  https://docs.deribit.com/fix-api/production/overview
- **[R20]** Deribit FIX Logon, authentication, options.
  https://docs.deribit.com/fix-api/production/logon
- **[R21]** Deribit FIX Resend Request.
  https://docs.deribit.com/fix-api/production/resend-request
- **[R22]** Deribit FIX Sequence Reset.
  https://docs.deribit.com/fix-api/production/sequence-reset
- **[R23]** Deribit FIX New Order Single.
  https://docs.deribit.com/fix-api/production/new-order-single
- **[R24]** Deribit FIX Cancel/Replace.
  https://docs.deribit.com/fix-api/production/order-cancel-request
  https://docs.deribit.com/fix-api/production/order-cancel-replace
- **[R25]** Deribit FIX Execution Reports, IDs, and quantity fields.
  https://docs.deribit.com/fix-api/production/execution-reports
- **[R26]** Currency parameters and spot/derivatives account differences.
  https://docs.deribit.com/articles/currency-parameter
- **[R27]** Bybit order ACK, orderLinkId, and product parameters.
  https://bybit-exchange.github.io/docs/v5/order/create-order
- **[R28]** Bybit execution/fees.
  https://bybit-exchange.github.io/docs/v5/websocket/private/execution
- **[R29]** Deribit cancel.
  https://docs.deribit.com/api-reference/trading/private-cancel
- **[R30]** Deribit edit.
  https://docs.deribit.com/api-reference/trading/private-edit
- **[R31]** Deribit open orders by instrument.
  https://docs.deribit.com/api-reference/trading/private-get_open_orders_by_instrument
- **[R32]** Deribit trades by order.
  https://docs.deribit.com/api-reference/trading/private-get_user_trades_by_order
- **[R33]** Deribit ticker.
  https://docs.deribit.com/api-reference/market-data/public-ticker
- **[R34]** Bybit FIX, for comparison only; do not apply to Deribit.
  https://bybit-exchange.github.io/docs/fix-api/guide

---

## Appendix: Starting Prompt for Codex

```text
First read this repository's AGENTS.md, README, existing specifications,
handoff documents, and DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md.

This is an incremental upgrade to an existing Bybit implementation.
Do not create a separate unrelated project.
First complete the Phase 0 baseline audit to determine the actual
features and test status, then add Deribit in order.
Preserve existing Bybit commands, configuration, stored data, and
dashboard if one exists.

First complete Deribit HTTP JSON-RPC + WebSocket, order-intent /
deduplication / recovery, multi-venue isolation, and read-only aggregation.
Then continue with the Deribit FIX dialect and tests.
Do not treat Deribit label as a venue idempotency key.
Do not mix USD notional with BTC quantity.
Do not reuse Bybit FIX authentication and recovery rules.

All external connectivity must use Testnet only.
Do not use old keys previously pasted in conversation.
When new credentials are unavailable, continue local implementation
and testing and mark external validation BLOCKED.
Do not place orders without explicit authorization and test flags.
Never automatically retry across transports or venues.

Run tests and update IMPLEMENTATION_STATUS.md after every phase.
Finally deliver executable build/start instructions, incremental code,
tests, validation report, capability matrix, and a Traditional Chinese
CODEX_HANDOFF.md clearly separating mock success from real Testnet success.
```
