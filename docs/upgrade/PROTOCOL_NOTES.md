# Multi-venue protocol notes

## Identity and storage

Native order and trade identifiers are not globally unique. Non-legacy records use escaped compound keys containing venue, environment, account alias, namespace, and native identifier. Market and position identities also include category/instrument. The default archived Bybit account retains its old map keys, but lookup and deduplication are account-scoped so an identical Deribit identifier cannot merge with it.

## Testnet configuration

Deribit endpoints are exact allowlisted values: `https://test.deribit.com/api/v2`, `wss://test.deribit.com/ws/api/v2`, and `fix-test.deribit.com:9883`. Host suffixes, path changes, mainnet, zero risk limits, invalid booleans, and non-positive plan TTL fail closed. Deribit remains disabled by default.

## External test gates and evidence

Read tests use venue-specific `RUN_<VENUE>_READ_TESTS=1`. Every write requires both `RUN_MULTI_VENUE_E2E=1` and `RUN_<VENUE>_TRADING_TESTS=1`; FIX has a separate venue-specific gate. The removed legacy Bybit integration flags are not aliases.

A successful write response is only an acknowledgement. HTTP mutations must be checked through private events and an independent read API; WS/FIX mutations must be checked through canonical HTTP JSON-RPC/REST reads. Fees and execution totals come from trade history, and cleanup must independently prove the position is zero. Missing or ambiguous evidence is failure, `OutcomeUnknown`, or `NeedsReview`.

## Deribit HTTP JSON-RPC

Requests use monotonic IDs and require matching `jsonrpc=2.0` response IDs. JSON-RPC errors remain typed even when HTTP status is non-200, while server error data is discarded because it may echo sensitive request material. Client-credentials tokens are cached under a mutex and refreshed with a safety margin; only explicitly read-only `private/get_*` calls retry once after an auth error.

The CLI `account balances --currency all` uses the account-scoped `private/get_account_summaries` method. It must not enumerate `public/get_currencies`, because that catalog contains currencies that may not be valid account-summary scopes.

## Deribit WebSocket

Public unauthenticated subscriptions default to `.100ms`. Testnet returned `13778 raw_subscriptions_not_available_for_unauthorized` for `.raw`; reconnect diagnostics retain that typed cause rather than reporting a successful empty smoke test. The session negotiates heartbeat, answers `test_request` with `public/test`, uses bounded queues, and treats overflow as a connection-recovery condition.

Subscription data is rejected before the subscribe acknowledgement. Every ready generation invokes the recovery callback. Book duplicates are idempotent; a `prev_change_id` gap marks the book stale, and deltas remain blocked until a new snapshot.

## Deribit plan and order writes

Plans persist the venue/account, native collateral, explicit USD-notional amount unit, metadata mark/tick/minimum, open-order risk snapshot, transport, and 30-second expiry. Execution revalidates live metadata and risk, atomically claims the intent once, and requires `--confirm`. HTTP and WebSocket writes never fall back to each other. Any failure after a WS write or ambiguous HTTP transport is `OutcomeUnknown` and is not automatically resent.

Create, amend, and cancel commands perform a separate `private/get_order_state` read. A create response must match both native order ID and connector intent label; missing or mismatched evidence becomes `NeedsReview`. Deribit Testnet returned `private/get_user_trades_by_order` as a direct array, so the decoder accepts both the direct and documented wrapped shapes.

## Intent recovery and trade cursors

The state file records `Planned`, `Executing`, `Submitted`, `Rejected`, `OutcomeUnknown`, `Expired`, and `NeedsReview`. Recovery searches open and historical orders by the connector label and deduplicates native order IDs: zero stays unresolved, one is adopted, and more than one requires review. This search is reconciliation evidence, never permission to send again.

Trade pages sort by `(timestamp, trade_id)`, deliberately overlap the cursor timestamp, and rely on canonical trade-ID persistence for deduplication. This preserves late same-millisecond records. Cursors move only forward, pagination is capped, and `has_more` without progress fails visibly.

## Shared CLI and E2E

`--venue bybit` routes to the preserved commands; `--venue deribit` selects the JSON-RPC connector; `--venue all status|portfolio` returns per-venue results without merging native currencies or hiding a failed venue. The E2E runner requires the global write gate plus both venue read/trading gates. It derives quantities and prices from current metadata/tickers and fails unless separate reads, private-event correlation, canonical fee totals, and zero-position cleanup all agree.

## Operational limits and shutdown

Each Deribit account client bounds concurrent RPC activity. Read-only calls apply a capped cooldown for code `10028`; writes are never retried. Instrument metadata is cached for five minutes, while every plan records metadata/price timestamps and execution still rechecks within its short TTL. WebSocket metrics expose ready generations, reconnects, queue depth, test requests, and sanitized last cause.

Signals cancel root contexts, close sockets, stop new calls, and leave claimed writes durably marked `Executing`/`OutcomeUnknown` for startup recovery. `--venue all` retains each venue result and error separately, so one failure does not falsify the other venue's health.
