# Capability matrix

This matrix separates implemented capability from external validation. All remote capabilities are Testnet-only.

| Venue/surface | Read | Create | Amend | Cancel | Independent business-state proof | External level |
|---|---:|---:|---:|---:|---|---|
| Bybit REST V5 | Yes | Yes | Yes | Yes | REST/private WS/reconciliation | PASS |
| Bybit public/private WS | Yes | N/A | N/A | N/A | REST/reconciliation | PASS |
| Bybit FIX 4.4 | Session/order mock | Yes | Yes | Yes | REST semantics after reconnect | `LOCAL_TESTED`; live `BLOCKED_GATE` |
| Deribit HTTP JSON-RPC | Yes | Yes | Yes | Yes | distinct `private/get_*` reads and private events | PASS |
| Deribit public/private WS | Yes | Yes | Yes | Yes | canonical HTTP JSON-RPC reads | PASS |
| Deribit FIX 4.4 | SecurityList/session | D | G | F | canonical HTTP JSON-RPC reads | `TESTNET_ORDER_FLOW` |
| `--venue all` | status/portfolio | No | No | No | per-venue result and error | PASS |

## Deribit product and unit scope

| Instrument | HTTP/WS order amount | FIX request/response conversion | Settlement/fees | Dynamic metadata |
|---|---|---|---|---|
| BTC-PERPETUAL | USD notional | SecurityList contract multiplier proves contracts ↔ USD | BTC | tick, tick steps, minimum, contract size |
| ETH-PERPETUAL | USD notional | same proof required before live FIX | ETH | tick, tick steps, minimum, contract size |

Unsupported contract styles, expired/inactive instruments, unproved FIX conversion, invalid segmented ticks, and non-native settlement fail before order entry.

## Safety and recovery capabilities

- Deribit plans persist `Planned/Executing/Submitted/Rejected/OutcomeUnknown/Expired/NeedsReview`; only one process can claim an intent.
- HTTP/WS/FIX writes never cross-retry. Unknown transport results remain `OutcomeUnknown` and reconcile by label plus native identity.
- Private WS queries connection COD by default. Explicit enablement is connection-scoped, gated, confirmed, and independently read back.
- `user.changes` decodes all order/trade/position array entries. Orders and canonical JSON `trade_id` values enter the same persistent reducer used by reconciliation.
- Aggregation never sums BTC/USD/USDT without a current conversion source and never converts a failed venue into zero.
- Cleanup is connector-owned, reduce-only, and followed by independent zero-position/open-order reads.
