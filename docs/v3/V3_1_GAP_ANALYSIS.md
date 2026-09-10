# VenueWire V3.1 gap analysis

Updated: 2026-09-10
Baseline: existing Bybit/Deribit Testnet CLI, HTTP/JSON-RPC, WebSocket, FIX, persistence and reconciliation implementation
Target: `docs/3_web/TRADING_CONSOLE_V3_CHANGE_SPEC.md` plus `VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md` §26

## Product decisions applied

- Interviewers use the Web Console; CLI remains a low-usage engineering test interface.
- One configured login and shared Recent Trades are intentional because every session represents the same demo accounts.
- `QUOTE_TTL=5s`; V3 configuration names remain canonical and V3.1 session/hour/concurrent plus per-venue source-asset caps apply.
- Global rolling-hour and concurrent limits span sessions and venues. Durable intent acceptance consumes quota; rejected, zero-fill and Unknown count. Invalid/expired pre-confirm requests do not.
- `Unknown` retains the only concurrent slot until authoritative terminal evidence. Recheck only queries and never resubmits or force-resolves.
- Existing `DERIBIT_API_KEY`/`DERIBIT_API_SECRET`, CLI/FIX/read/trading gates and locked JSON stores remain. No SQLite rewrite or strong CLI/Web/multi-instance coordination is added for this MVP.
- There is no public fault simulator. Failure behavior is demonstrated with deterministic fixtures/recordings.

## Baseline-to-target matrix

| Sections | Existing foundation | Required V3/V3.1 work | Current evidence |
|---|---|---|---|
| 1 — Branding | Project/binary already named VenueWire | Login/navigation branding and accurate prototype wording | Local Vue component fixture and README complete |
| 2 — Testnet guard | Exact connector allowlists | Apply guards before Web exchange use; include Bybit public Spot endpoint | Config/security regression complete |
| 3 — Credentials | Backend env configuration and gates | Web secret boundary, safe DTOs, least-privilege guidance | Local API/repository tests and docs complete |
| 4 — Authentication | No prior Web auth | Dotenv, fixed login, sessions, logout, CSRF, Origin/Host/proxy checks, login limit, safe errors | Local server tests complete |
| 5 — Abuse controls | Existing venue gates | Persistent global hourly/concurrent, session and venue source-asset caps | Intent-store tests complete |
| 6–7 — Intents/state | Persistent plans, IDs and normalized order states | Browser idempotency, quote consumption, uncertainty, complete lifecycle correlation | Local application/intent tests complete |
| 8–9 — Quick Trade/results | REST/RPC order/execution clients | Four Spot directions, metadata/fee-aware exact planning, frozen IOC quote, partial/zero fill and net result | Quote/adapter/reconcile tests complete |
| 10 — Reconciliation | Existing venue reconcilers | Startup/private reconnect/uncertain/post-trade recovery plus bounded Browser Recheck; never resubmit | Local recovery fixtures complete |
| 11–13 — Architecture/model/precision | Venue identity, capabilities, decimal-string planning | Narrow account/Spot adapters, normalized Browser DTOs, timing/fee fields, precision audit | Local package/race tests complete |
| 14 — Account/valuation | Account reads and private event clients | Authoritative snapshots, capacity, live valuation with explicit source/quality | Local account/valuation/WS fixtures complete |
| 15–17 — Freshness/status/metrics | WS recovery/logging | Receive vs meaningful event age, stream/account state, RTT/events/errors/reconnect/reconcile metrics | API/UI fixtures complete |
| 18 — Recent Trades | Persistent orders/executions | Shared Browser history, lifecycle, relogin/reload recovery and Recheck | Local server/component fixtures complete |
| 19 — About | Existing connector capability evidence | Architecture, truthful FIX labels, non-sensitive build/uptime | Vue implementation and build metadata tests complete |
| 20 — Deployment | Nginx reference fragments | Trusted proxy/WSS boundary, private bind, ACL and rollback docs | Local code/config tests and docs complete; real deployment `NOT_RUN` |
| 21–22 — UI/scope | Reference visual/specification | Focused account/Quick Trade/status/history/About UI; no terminal/fault simulator | Vue implementation/component fixtures complete |
| 23–24 — Acceptance | Existing backend fixtures | All 15 failure scenarios, Browser/API/restart/frontend/build regression and evidence labels | Local suite complete; current Testnet/deployment evidence `NOT_RUN` |

## Remaining gaps

No known local implementation requirement remains unaddressed after the final requirement audit. The remaining work requires external environment or current operator evidence and must not be inferred from mocks:

1. Bybit Testnet Web account/public/private stream verification.
2. Deribit Testnet Web account/public/private stream verification.
3. Explicitly authorized Browser Spot order lifecycle evidence for both venues/directions selected by the operator.
4. Real split-host Nginx HTTPS/WSS, private Go bind and firewall/ACL verification.
5. Sanitized screenshots and failure recordings from that real deployed UI.

Until those run, delivery is labeled **local implementation complete; external verification pending**, not globally complete.
