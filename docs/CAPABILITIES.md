# VenueWire Capability Matrix

Updated: 2026-09-12

VenueWire implements separate Bybit and Deribit connectors and normalizes their observable account and order state without erasing venue-specific protocols, units, or validation levels. All remote evidence is Testnet-only.

## Venue and protocol support

| Capability | Bybit | Deribit |
|---|---|---|
| Environment | Testnet | Testnet |
| Request transport | REST V5 | HTTP JSON-RPC |
| Public WebSocket | Implemented; Testnet verified | Implemented; Testnet verified |
| Private WebSocket | Implemented; Testnet verified | Implemented; Testnet verified |
| Account balances | Testnet verified | Testnet verified |
| Order create/amend/cancel | Testnet verified | Testnet verified over HTTP and WebSocket |
| Independent business-state reads | REST, private WS, reconciliation | Canonical HTTP JSON-RPC and private WS |
| Reconciliation | Implemented and locally regression tested; Testnet evidence recorded | Implemented and locally regression tested; Testnet evidence recorded |
| Browser Spot routes | `BTC/USDT`, `ETH/USDT`, both directions | `BTC/USDC`, both directions |
| Browser Spot order lifecycle | Small Limit IOC fill, fee, and balance sync Testnet verified | Small Limit IOC fill, fee, restart recovery, and balance sync Testnet verified |
| FIX implementation | FIX 4.4 codec/session/order fixture | Distinct FIX 4.4 dialect, session, SecurityList, D/G/F, and execution reports |
| FIX real Testnet logon | Blocked by external whitelist/RSA gate | Verified |
| FIX real Testnet order flow | Blocked by external whitelist/RSA gate | Verified with independent JSON-RPC identity/state reads |

## Browser execution contract

| Capability | Current behavior |
|---|---|
| Quote | Five-second frozen quote using fresh market data, exact decimals, venue metadata, fee/capacity checks, and 0.5% protection |
| Order type | No-borrow Limit IOC |
| Persistence | Trade intent, client order ID, quota reservation, and active slot persist before submission |
| Idempotency | Duplicate confirmation cannot create a second logical trade |
| Uncertain result | A post-write transport uncertainty becomes `Unknown`; no automatic resubmission occurs |
| Recovery | Startup, private-stream reconnect, periodic recovery, and manual Recheck share serialized reconciliation |
| Fill accounting | Partial fill, zero fill, average price, source/destination fees, net received amount, and balance sync remain distinct |
| Browser state | Venue-scoped account revisions, stream sequence/instance checks, resync, and stale-data indicators |

## CLI surface

The CLI exposes Bybit REST/WebSocket/FIX engineering commands, Deribit JSON-RPC/WebSocket/FIX commands, persistence and reconciliation, and read-only multi-venue status/portfolio commands. `--venue all` does not permit writes. Run `./bin/venuewire help` for the current command reference.

## Safety and non-capabilities

- No Mainnet endpoints, withdrawals, or transfers.
- No automatic cross-venue routing, smart order router, or failover execution.
- No strategy engine, arbitrage system, or automated trading strategy.
- No production or HFT performance claim.
- No aggregation that silently treats a failed or unpriced venue as zero.
- No claim that account valuation reconstructs portfolio margin or liquidation risk.

## Evidence

- [Current status](STATUS.md)
- [V3/V3.1 test report](v3/TEST_REPORT_V3.md)
- [Multi-venue validation report](upgrade/VALIDATION_REPORT.md)
- [Deployment boundary and verification](v3/DEPLOYMENT.md)
