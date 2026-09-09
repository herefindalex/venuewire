# Capability matrix

This matrix describes implemented capability declarations, not external-validation claims.

| Venue | Metadata | Account read | Order read/write | Public/private WS | FIX order write |
|---|---:|---:|---:|---:|---:|
| Bybit Testnet | Yes | Yes | Yes | Yes | Yes; live access may be blocked by whitelist |
| Deribit Testnet R1 target | Yes | Yes | Yes | Yes | No; enabled only after the separate R2 dialect is validated |

Capabilities are scoped by venue, environment, account alias, instrument/category, transport, and operation. A URL being reachable never grants a write capability.
