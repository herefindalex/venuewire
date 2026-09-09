# Capability matrix

This matrix describes implemented capability declarations, not external-validation claims.

| Venue | Metadata | Account read | Order read/write | Public/private WS | FIX order write |
|---|---:|---:|---:|---:|---:|
| Bybit Testnet | Yes | Yes | Yes | Yes | Yes; live access may be blocked by whitelist |
| Deribit Testnet | Yes | Yes | Yes | Yes | `TESTNET_ORDER_FLOW`: metadata-minimum D/G/F passed with independent JSON-RPC verification |

Capabilities are scoped by venue, environment, account alias, instrument/category, transport, and operation. A URL being reachable never grants a write capability.
