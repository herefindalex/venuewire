# VenueWire V3.1 gap analysis

This is a pre-implementation assessment, not a completion report. Read the V3 specification together with V3.1 supplement section 26 for the confirmed product decisions.

## Baseline and evidence

- Current application: Go CLI at `cmd/venuewire`; no Web server, browser API, sessions or frontend yet.
- `internal/config/config.go` already provides Testnet endpoint validation and separate credential/trading gates. Bybit public WS configuration currently targets linear products; Spot requires a validated endpoint extension.
- `internal/domain/order.go` already includes decimal-string amounts, raw order status, executions, fees, `Unknown` and pending states. This is useful foundation, not evidence of a complete browser trade-intent lifecycle.
- `internal/intent`, `internal/orderstate`, `internal/reconcile` and `internal/deribitreconcile` provide persistent state and recovery logic. Web quote consumption, shared quotas, source-asset reservations and recheck orchestration remain to be added.
- The existing baseline audit is `BASELINE_AUDIT.md`. It records 238 passing tests, one repository security-example failure and two skipped external tests, with the same failure under the race detector. Those are prior audit results, not tests rerun for this documentation update.
- Root V2 implementation status records earlier verified venue behavior. It does not establish V3 Spot or Web acceptance.

## Gaps against supplement sections 1–24

| Sections | Existing foundation | Required V3/V3.1 work |
|---|---|---|
| 1 — Branding | Project/binary already named VenueWire | Login/navigation branding and accurate prototype wording |
| 2 — Testnet guard | Existing endpoint allowlists | Apply before Web exchange use; extend Bybit public Spot endpoint without weakening guards |
| 3 — Credentials | Backend configuration and existing gates | Web secret boundary, safe DTOs, documented per-venue least privilege |
| 4 — Authentication | No Web authentication | Dotenv/WebConfig, sessions, expiry/logout, CSRF, Origin/Host/proxy checks, login limits, safe correlated errors |
| 5 — Abuse controls | Existing venue controls | Durable global rolling-hour/concurrent quotas, session quota and venue source-asset caps |
| 6–7 — Intents/state | Persistent intents, identifiers and normalized order states | Browser idempotency, quote consumption, explicit submission uncertainty, full lifecycle correlation and cancel/fill ordering |
| 8–9 — Quick Trade/results | REST/RPC and order/execution clients | Four Spot directions, metadata and fee-aware decimal planning, frozen Limit IOC quote, 5-second TTL, partial/zero fill and net-received results |
| 10 — Reconciliation | Existing venue reconcilers | Startup/private reconnect/uncertain submit/post-trade triggers; browser Recheck; recover quotas and reservations without resubmission |
| 11–12 — Architecture/model | Venue identity, capabilities, orders/executions | Narrow adapter interfaces for account/Spot quotes/trading; normalized browser models, complete timing and fee fields |
| 13 — Precision | Decimal strings and existing exact planning | Audit all new account, quote, fee, valuation and protocol paths; validate ticks, steps and minimums |
| 14 — Account/valuation | Account reads and private event clients | Authoritative snapshots, capacity checks, reconnect recovery, live local valuation with explicit source and quality |
| 15–17 — Freshness/status/metrics | Existing WS/recovery/logging components | Per-stream freshness, account sync status, request/event timings, errors, reconnects, reconciliation discrepancies and backend-fed System Status |
| 18 — Recent Trades | Persistent order/execution data | Shared history, lifecycle details, relogin/refresh recovery and bounded Recheck action |
| 19 — About | Existing Go application and connector capabilities | Architecture diagram, truthful FIX verification labels, non-sensitive build information and uptime |
| 20 — Deployment | Nginx reference snippets | Implement trusted proxy and browser WS boundary; test locally; actual split-host deployment remains NOT_RUN |
| 21–22 — UI/scope | Reference asset and specifications | Vue 3 console, account/assets/Quick Trade/status/history/About; no interactive fault simulator or extra terminal features |
| 23–24 — Acceptance | Existing backend tests and fixtures | All 15 failure scenarios, browser/API/restart tests, frontend checks/build and regressions; separately label simulated and live verification |

## Confirmed behavior and implementation order

The browser is the interview interface. All authenticated sessions share history and exchange accounts. Default Demo quotas are 10 per session, 30 globally per rolling hour, and 1 globally concurrent across both venues. Accepted durable submission intents count once, including later rejection, zero fill or Unknown; invalid/expired requests do not. Unknown retains its active slot until authoritative reconciliation resolves it. Hourly usage and unresolved occupancy survive restart.

Keep V3 configuration names, set `QUOTE_TTL=5s`, and enforce the smaller of each existing asset cap and its new venue-specific Demo cap. Preserve the existing credential names, gates and locked JSON persistence. There is one Web writer process; no SQLite or multi-instance coordination is planned.

Implement in dependency order:

1. Configuration, baseline security-example correction, Web server/auth/proxy boundaries and Demo policy definitions.
2. Exact normalized Spot planning and persistent browser intents, atomic quota/reservation accounting, idempotency and Unknown recovery. Monetary correctness is required before enabling submission, even though the supplement lists the Decimal audit later.
3. Account snapshots/private stream recovery, reconciliation triggers, Recheck, market freshness and observable runtime state.
4. Vue console, Browser WS, Quick Trade, shared Recent Trades, System Status and About; build-tagged embedded assets.
5. Deterministic failure coverage, browser regressions, deployment checks, labeled demonstration recordings and handoff.

Run relevant backend checks per phase; run frontend checks once the frontend exists. Before that, record frontend checks as NOT_APPLICABLE rather than passing. Only explicitly authorized external runs can establish Testnet or deployed-environment verification.

## Current verification and remaining work

This update changes specifications and configuration examples only. No runtime feature, test fixture, recording, deployment or live exchange validation has been completed by this update. The baseline security-example failure still needs implementation work. Completion must be tracked in `../../IMPLEMENTATION_STATUS.md` without replacing historical V2 evidence.
