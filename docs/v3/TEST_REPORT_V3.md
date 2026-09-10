# VenueWire V3/V3.1 test report

Date: 2026-09-10
Scope: local V3.1 implementation through deterministic failure scenarios and fixture UI
External Browser Testnet orders and split-host deployment: `NOT_RUN`

## Result summary

| Layer | Command | Result |
|---|---|---|
| Go full regression | `go test ./... -count=1 -timeout=180s` | PASS — 390 tests, 23 packages |
| Go race regression | `go test -race ./... -count=1 -timeout=240s` | PASS — 390 tests, 23 packages |
| Static analysis | `go vet ./...` | PASS |
| Frontend typecheck | `npm --prefix web run typecheck` | PASS |
| Frontend tests | `npm --prefix web test` | PASS — 6 tests, 2 files |
| Frontend production build | `npm --prefix web run build` | PASS |
| Embedded Web binary | `make build` | PASS — `bin/venuewire` |
| Whitespace | `git diff --check` | PASS |
| Current V3.1 Bybit/Deribit Testnet reads/streams/orders | — | `NOT_RUN` |
| Current HTTPS/WSS split-host Nginx deployment | — | `NOT_RUN` |

All local commands above are credential-free or use mocks/fixtures and do not place exchange orders. Earlier V2 Testnet/FIX results in `docs/upgrade/VALIDATION_REPORT.md` are historical connector regression evidence, not evidence that the V3.1 Browser flow was deployed.

## Original V3 requirements R01–R13

| Requirement | Local evidence | Verdict |
|---|---|---|
| R01 application-owned dotenv | `internal/config/dotenv_test.go`; executable-relative binary/parent discovery, precedence, atomic parse, explicit `--env-file` parsing | `LOCAL_VERIFIED` |
| R02 per-Browser Bybit/Deribit selection | Vue local selection/store; venue-scoped account API; old revisions rejected | `LOCAL_VERIFIED` |
| R03 normalized account/assets/status home | `internal/accountstate/*_test.go`, `internal/webconsole/account_api_test.go`, `web/src/App.test.ts` | `LOCAL_VERIFIED` |
| R04 market-driven valuation | `internal/accountstate/valuation_test.go`, `cmd/venuewire/web_streams_test.go`, Browser valuation snapshot event tests | `LOCAL_VERIFIED` |
| R05 edit/review/confirm/wait/result modal | Vue implementation, quote/confirm API tests and component fixture | `LOCAL_VERIFIED` |
| R06 terminal result and account sync separation | `internal/tradereconcile/service_test.go`; shared revision-aware account store | `LOCAL_VERIFIED` |
| R07 Deribit ETH↔BTC Spot | `TestQuoteServiceMapsAllFourSpotDirectionsWithExactProtection`, Deribit adapter tests | `LOCAL_VERIFIED` |
| R08 Bybit BTC↔USDT Spot | same route and Bybit adapter tests | `LOCAL_VERIFIED` |
| R09 Limit IOC and 0.5% protection | quote tests plus exact Bybit/Deribit submitted-payload assertions | `LOCAL_VERIFIED` |
| R10 fixed Web login | authentication, generic failure, rate-limit, absolute-expiry and logout tests | `LOCAL_VERIFIED` |
| R11 HTTPS/WSS trusted Nginx boundary | Host/Origin/trusted-peer/XFF/CSRF/WebSocket tests and reference snippets | `LOCAL_VERIFIED`; deployment `NOT_RUN` |
| R12 Go private HTTP bind, no Go TLS | strict Web config tests and deployment documentation | `LOCAL_VERIFIED`; host ACL `NOT_RUN` |
| R13 preserve existing connector/CLI/FIX behavior | complete 390-test Go normal/race suites, ordinary CLI and embedded Web builds | `LOCAL_VERIFIED` |

## V3.1 section 23 failure scenarios

Every scenario has deterministic CI-safe evidence. Test names are intentionally explicit where the scenario previously had only indirect coverage.

| # | Scenario | Evidence | Verdict |
|---:|---|---|---|
| 1 | User clicks Confirm twice | `TestApplicationConcurrentConfirmSubmitsExactlyOnce` | PASS |
| 2 | Two tabs submit the same intent | `TestConfirmQuickTradeIsDurablyIdempotentAcrossRequestsAndExpiry` uses a second request ID for the consumed quote | PASS |
| 3 | HTTP/RPC submission times out with unknown result | `TestApplicationUncertainSubmissionSurvivesRestartAndIsNotResent`; adapter uncertain-error tests | PASS |
| 4 | Private WS disconnects before order ACK | `TestApplicationPrivateDisconnectBeforeAckDoesNotLoseSubmission`; reconnect/recovery tests | PASS |
| 5 | VenueWire restarts while an order is active | uncertain submission reload test; `TestStartupRecoveryNeverSubmitsCreatedIntent` | PASS |
| 6 | IOC partially fills | `TestBybitPartialIOCReconcilesFeesAndTerminalBalance` | PASS |
| 7 | IOC receives no fill | `TestDeribitZeroFillIOCIsTerminalAndReleasesSlot` | PASS |
| 8 | Fill arrives while cancel is pending | `TestFilledSupersedesPendingCancel`; `TestLaterFillEvidenceSupersedesCancelledState` | PASS |
| 9 | Balance stream connected but account stale | `TestSystemStatusKeepsHeartbeatLiveWhileSurfacingStaleAccountAndMarket`; account stale-capacity tests | PASS |
| 10 | Public market stream becomes stale | `TestStreamState`; API/component stale presentation fixture | PASS |
| 11 | Venue rejects instrument price/quantity rules | `TestTradeQuoteCapsAndFreshnessFailClosed`; adapter explicit rejection/metadata tests | PASS |
| 12 | Exchange rate limit reached | `TestSubmissionRateLimitedRecognizesVenueSignals`; `TestRecordSubmissionObservationPublishesRateLimitHealth` | PASS |
| 13 | Browser refreshes during Unknown | `TestRecentTradesAreSharedWithoutSessionIdentifiers`; authenticated `App.test.ts` Unknown fixture | PASS |
| 14 | Reconciliation changes Unknown to Filled/Cancelled/Rejected | `TestUnknownReconcilesToEveryAuthoritativeTerminalOutcome` | PASS |
| 15 | `WEB_TRADING_ENABLED=false` | `TestTradingDisabledBlocksConfirmBeforeApplication` | PASS |

## V3.1 section 24 acceptance audit

| Acceptance area | Evidence | Verdict |
|---|---|---|
| VenueWire/Testnet branding and prototype wording | Vue component fixture; README | `LOCAL_VERIFIED` |
| Exact Testnet endpoint guards | config/security tests for both venues; redirect/allowlist regression | `LOCAL_VERIFIED` |
| No exchange credentials/internal errors in Browser | venue/account/trade DTO and repository secret tests | `LOCAL_VERIFIED` |
| Least-privilege demo credential guidance | `.env.example`, deployment/handoff docs; no secret values | `DOCUMENTED` |
| Login/session/REST/WS/CSRF/Origin safety | `internal/webconsole/server_test.go` | `LOCAL_VERIFIED` |
| Backend quotas and durable idempotency | `internal/intent/quicktrade_test.go`, application concurrency tests | `LOCAL_VERIFIED` |
| ID correlation and Unknown lifecycle | intent/application/reconciliation/API tests | `LOCAL_VERIFIED` |
| Startup and reconnect reconciliation without resubmit | reconciliation and exchange WS reconnect tests | `LOCAL_VERIFIED` |
| Venue monetary units and four routes | quote and adapter payload tests | `LOCAL_VERIFIED` |
| Account normalization, capacity and refresh | account/provider/adapter/reconciliation tests | `LOCAL_VERIFIED` |
| Live valuation does not mutate quantity | `TestLiveValuationChangesMarksWithoutChangingQuantities` | `LOCAL_VERIFIED` |
| Receive/event freshness and stale UI | WS metric, system API and component tests | `LOCAL_VERIFIED` |
| System Status, Recent Trades and About | Vue component/build plus Browser REST/WS tests | `LOCAL_VERIFIED` |
| Order/event timings, rate limit, reconnect, discrepancy | account metrics, submission and system-status tests | `LOCAL_VERIFIED` |
| Partial/zero fill, fee/net amount, balance refresh | trade reconciliation tests | `LOCAL_VERIFIED` |
| Nginx WebSocket and private Go port | code boundary, reference snippets and docs | `IMPLEMENTED`; real deployment `NOT_RUN` |
| README accurate public-demo wording | current README | `DOCUMENTED` |
| Existing Bybit/Deribit/CLI/FIX regression | 388-test normal/race suites and builds | PASS |

## External acceptance still required

The following are not failures and are not claimed complete:

1. Current authorized Bybit Testnet account/read/public/private stream observation through the Web process.
2. Current authorized Deribit Testnet account/read/public/private stream observation through the Web process.
3. Explicitly authorized small Web Spot trade in each required venue/direction chosen by the operator, including independent terminal/fee/balance evidence.
4. Actual Nginx `nginx -t`, reload, firewall/ACL and HTTPS/WSS split-host verification.
5. Sanitized screenshots/recordings from the real deployed UI for login, both venue balances, Review, waiting, terminal/partial, balance syncing/synced and degraded states.

Until those are performed, the correct release label is: **local implementation complete; external verification pending**.
