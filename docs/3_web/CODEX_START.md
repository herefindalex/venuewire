# V3 Codex Entry Point

Canonical specification: `TRADING_CONSOLE_V3_CHANGE_SPEC.md`.

This is an incremental change to the existing Bybit + Deribit program, not a new project. Read the repository's `AGENTS.md`, README, V2 specification, and handoff documents before starting Phase 0.

```text
Implement the changes defined in TRADING_CONSOLE_V3_CHANGE_SPEC.md and
VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md.
Read CODEX_START_V3_1.md first. Where the documents conflict, the confirmed
decisions in §26 of the supplement take precedence.

Preserve the existing adapters, CLI, FIX support, order tracking, execution
deduplication, data, and tests.
Add dotenv support, fixed-credential login configured through environment
variables, a Vue Web UI, live account/valuation data, four-direction Spot
Quick Trade modals, Review/Confirm, and Limit IOC orders with 0.5% protection.

Nginx already terminates SSL and runs on a different machine from Go. Bind Go
to a private IP. Do not make Go serve TLS. Trust only the specified Nginx and
implement secure sessions, CSRF protection, and WSS support.

Execute Phases 0–7, test each phase, and update IMPLEMENTATION_STATUS.md.
When credentials or a deployment environment are unavailable, complete the
local work and mark external verification BLOCKED/NOT_RUN.
Do not place Testnet orders, modify Nginx/firewall configuration, or change
account settings without separate explicit authorization.

The final delivery must include the actual build/start commands, test and
deployment documentation, TEST_REPORT_V3.md, and CODEX_HANDOFF_V3.md.
```

## Package Contents

| File | Purpose |
|---|---|
| `TRADING_CONSOLE_V3_CHANGE_SPEC.md` | The single canonical V3 incremental specification, including flows, contracts, constraints, and acceptance criteria |
| `.env.example` | New configuration example; secrets are empty and it does not overwrite the original file |
| `deploy/nginx-http-map.conf` | WebSocket `Connection` map for the Nginx `http {}` scope |
| `deploy/nginx-proxy-common.conf` | Shared headers, timeout, and no-retry settings for each location |
| `deploy/nginx-https-locations.conf` | Example locations to merge into the existing SSL server |
| `deploy/DEPLOYMENT_NOTES.md` | Cross-machine addressing, ACL, credential, and demo-mode guidance |
| `reference/account-balance-reference.png` | User-provided asset-page reference; it is not real API data and must not be presented as implemented behavior |

The actual repository had not been inspected when this package was prepared. This package did not modify or run user code, log in, or trade.
