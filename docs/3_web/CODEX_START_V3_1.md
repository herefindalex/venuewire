# CODEX START — VenueWire V3.1

Read these files first, in order:

1. `TRADING_CONSOLE_V3_CHANGE_SPEC.md`
2. `VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md`

Include section 26 of the supplement: it records the confirmed decisions and overrides conflicting draft requirements. Then read `../v3/V3_1_GAP_ANALYSIS.md` for the current baseline and implementation gaps. Use the canonical V3 names in `.env.example`, with `QUOTE_TTL=5s` and the added V3.1 Demo limits.

Then inspect the current repository before editing code.

This is an incremental upgrade. Do not recreate working Bybit/Deribit connectors or break existing CLI/FIX/tests.

Primary V3.1 priorities:

1. public Testnet demo hardening;
2. idempotent trade intents and explicit `Unknown` state;
3. startup/reconnect/uncertain-order reconciliation;
4. venue abstraction + normalized domain model;
5. exact Decimal handling for trading values;
6. WebSocket freshness detection;
7. System Status UI;
8. Recent Trades order lifecycle UI;
9. About/Architecture UI;
10. observability and split Nginx/Go deployment safety.

Before implementation, write a short gap analysis comparing the current codebase to the V3 and V3.1 requirements.

After each implementation phase:
- run tests;
- run frontend build/tests;
- update `IMPLEMENTATION_STATUS.md`.

Do not mark real Testnet integration behavior as verified unless it was actually tested against the venue.
