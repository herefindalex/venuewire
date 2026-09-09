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
