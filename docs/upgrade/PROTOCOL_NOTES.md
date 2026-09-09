# Multi-venue protocol notes

Validated against official Bybit/Deribit Testnet behavior on 2026-09-09. These are connector decisions, not assumptions that one venue's semantics apply to the other.

## Identity and routing

- Routing always receives explicit venue/environment/account/instrument/transport. Symbols never imply a venue.
- Order and execution keys include venue, Testnet environment, stable account alias, native namespace, and native ID.
- Bybit remains the default only for legacy commands without `--venue`. `--venue all` supports read-only `status` and `portfolio`; writes are rejected.

## Testnet and secrets

- Exact allowlists are `api-testnet.bybit.com`, `stream-testnet.bybit.com`, `fix-oe-testnet.bybit.com:9000`, `test.deribit.com/api/v2`, `test.deribit.com/ws/api/v2`, and `fix-test.deribit.com:9883`.
- Redirects, host/path substitutions, IP fallbacks, plaintext FIX, Mainnet, missing limits, and malformed gates fail before authenticated requests.
- Server error data and FIX free text are not logged because they may reflect credentials or request data.

## Deribit HTTP JSON-RPC

- Requests are POST JSON with monotonically increasing IDs. Responses require `jsonrpc=2.0`, the expected ID, and exactly a usable result or typed error.
- HTTP 200 with JSON-RPC error remains failure. A JSON-RPC error on non-200 HTTP remains typed; malformed non-200 bodies report HTTP failure.
- Client-credential tokens are cached under a mutex with a refresh margin. Concurrent reads produce one refresh. Only explicit `private/get_*` reads retry once after auth failure.
- Code `10028` uses bounded cooldown for reads. Writes are never retried after an ambiguous transport result.
- `account balances --currency all` uses account-scoped `private/get_account_summaries`; it does not infer account assets from the public currency catalog.

## Amounts, ticks, and fees

- BTC-PERPETUAL/ETH-PERPETUAL HTTP and WebSocket order `amount` is USD notional; settlement and fee currency are native BTC/ETH.
- `tick_size`, `tick_size_steps`, `min_trade_amount`, and `contract_size` remain distinct. The highest `tick_size_steps.above_price` strictly below the requested price selects the effective tick.
- Plan validation uses exact rationals. Invalid increments/ticks are rejected with the legal metadata value; the connector never rounds up.
- HTTP/WS create/edit amounts and prices are encoded as JSON numbers using `json.Number`, not quoted strings or `float64` calculations.
- Trades retain gross amount, signed fee, fee currency, and canonical native `trade_id`. Rebates are not converted to absolute values. Cross-currency fee totals are not fabricated.

Final public Testnet metadata observed:

| Instrument | tick | tick steps | minimum amount | contract size | settlement |
|---|---:|---|---:|---:|---|
| BTC-PERPETUAL | 0.5 | empty | 10 USD | 10 | BTC |
| ETH-PERPETUAL | 0.05 | empty | 1 USD | 1 | ETH |

These values are evidence, not constants; every plan refetches/caches official metadata and records its timestamp.

## Deribit public/private WebSocket

- Public subscriptions default to `.100ms`. Testnet returned `13778 raw_subscriptions_not_available_for_unauthorized` for public `.raw`.
- Application heartbeat uses `public/set_heartbeat`; `test_request` triggers `public/test`. WebSocket ping/pong does not replace it.
- Book deltas apply only when `prev_change_id` matches the last trusted change. Duplicates are counted; gaps mark the book stale until a new snapshot on a new generation.
- Private startup authenticates, optionally enables connection COD, queries connection COD, and subscribes. Responses correlate by request ID and may be reordered.
- Early subscription events enter a bounded recovery buffer. Reconciliation runs before delivery. Queue/recovery overflow fails the venue connection instead of dropping private data.
- `user.changes` consumes all order/trade/position array elements. Orders and canonical trades enter the same persistent reducer used by HTTP reconciliation.

## Cancel-on-Disconnect

- `doctor` reads account-scope COD. Every private stream reads connection-scope COD without modifying it.
- `--enable-connection-cod --confirm` requires `RUN_MULTI_VENUE_E2E=1` and `RUN_DERIBIT_TRADING_TESTS=1`; it sends only `scope=connection` and reads the result back on the same socket.
- Connection COD protects only orders created by that connection. It does not protect HTTP or other sockets, is not a synchronous cancellation guarantee, and does not make Logout proof of cancellation.
- Shutdown/connection loss always leads to independent order/trade/position reconciliation. Connector-owned explicit cancellation/cleanup is preferred over relying on COD side effects.

## Planned HTTP/WS order lifecycle

- `plan` records venue/account/instrument, USD amount, mark, effective tick, metadata/price timestamps, limits, transport, and COD protection status. It expires after the configured TTL.
- A cross-process lock allows one execution claim. The claim is persisted before the write. Crash/timeout after write becomes `OutcomeUnknown`, never an automatic retry.
- HTTP and WebSocket support create, edit, and cancel. The requested transport is used for every mutation; no fallback occurs.
- WebSocket write RPC handles heartbeat messages, ignores unrelated IDs/notifications, returns typed business errors, and classifies post-write disconnect/malformed result as `OutcomeUnknown`.
- Every mutation reads `private/get_order_state` independently. Identity, amount, price, and terminal status must agree or the intent/order becomes reviewable rather than verified.

## Intent recovery and pagination

- Recovery searches by connector label, then deduplicates native order IDs. Zero matches remains unresolved; exactly one is adopted; multiple matches become `NeedsReview`.
- Labels are reconciliation evidence, not exchange idempotency guarantees. No uncertain intent is submitted again automatically.
- Trade pages sort by `(timestamp, trade_id)`, overlap the cursor timestamp, deduplicate canonical IDs, and fail when `has_more` makes no progress. This preserves same-millisecond late records.
- Reconciliation is repeatable and cannot regress newer private-stream/order state with an older ACK or snapshot.

## Deribit FIX 4.4 dialect

- TLS endpoint: `fix-test.deribit.com:9883`; `TargetCompID=DERIBITSERVER`.
- Logon uses a cryptographically random 32-byte nonce, a strictly increasing Unix-millisecond timestamp, `RawData(96)=timestamp.base64(nonce)`, and `Password(554)=Base64(SHA256(RawData || client_secret))`. It is not Bybit RSA or HMAC.
- Heartbeat/COD/fill-reporting policies are explicit. Sensitive auth material and free-text rejects are redacted.
- Deribit has a separate sequence policy. Gaps pause application writes, out-of-order frames use a bounded buffer, server ResendRequest replays a bounded journal with `43=Y` and `122`, and SequenceReset ignores header 34 but requires strictly forward `NewSeqNo(36)`.
- Recovery resumes only after canonical JSON-RPC reconciliation. Cross-socket permanent replay is never assumed.
- SecurityList repeating groups preserve unknown fields and reject unsupported nested groups. Live order entry requires JSON `contract_size` to equal FIX multiplier and JSON minimum USD to equal `MinTradeVol × multiplier`.
- D/G requests use USD `OrderQty(38)` with `QtyType(854)=Units(0)`; ExecutionReport contract quantities are converted with the proven multiplier before JSON comparison.
- Correlation uses `OrigClOrdID(41)`, label tag `100010`, and native `OrderID(37)`. Server-replaced tag 11 is never assumed to be the original client ID.
- FIX execution IDs are protocol evidence only. Accounting and fee deduplication use independently read canonical JSON trade IDs.

Live observations now covered by fixtures:

- BTC-PERPETUAL FIX `SettlCurrency(120)` may describe USD quote/contract semantics while JSON settlement and commission currency remain BTC.
- Cancel reports may omit optional `OrderID(37)`/`OrderQty(38)` and initially report pending cancel. The connector retains only independently pre-read connector-owned identity and waits for fresh JSON terminal state.

## Observability and shutdown

- Metrics expose RPC success/error/latency, read rate limits/token refresh, WS generation/reconnect/queue/readiness/test requests/COD, reconciliation differences, and FIX validation/sequence status.
- Signal cancellation stops new calls, closes bounded sockets, joins readers, and leaves claimed writes durably `Executing`/`OutcomeUnknown` for recovery.
- Local FIX mocks use isolated temporary state and normalize peer EOF only after their context is cancelled. Production/unexpected EOF remains an error.
