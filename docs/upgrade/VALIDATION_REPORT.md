# Multi-venue validation report

No credentials, tokens, signatures, or full authentication payloads are included.

## D-R2-001 — Deribit HTTP reads

- UTC time: 2026-09-09T15:32:22Z
- Build: local `master` worktree after commit `2657a27`
- Venue/environment/account: Deribit/Testnet/`deribit-test`
- Transport: HTTP JSON-RPC
- Commands: `venue deribit time`; `instrument --name BTC-PERPETUAL`; `account balances --currency all`; `positions --currency BTC --kind future`
- Expected: strict JSON-RPC reads succeed; metadata retains amount units; account-scoped aggregate returns the dashboard assets; positions are readable
- Actual: time succeeded; BTC-PERPETUAL active, BTC-settled/USD-quoted, tick 0.5, minimum amount 10, contract size 10; aggregate returned 14 currencies (`BNB,BTC,BUIDL,ETH,EURR,MATIC,PAXG,SOL,STETH,USDC,USDE,USDT,USYC,XRP`); BTC future positions count 0
- Result: PASS
- Evidence: sanitized CLI JSON observed locally; no write gate enabled

## Write/FIX validation

NOT_RUN. A read result does not satisfy any order lifecycle, WebSocket, reconciliation, fee, cleanup, or FIX acceptance item.

## D-R3-001 — Deribit public/private WebSocket reads

- UTC time: 2026-09-09T15:47Z–15:51Z
- Build: local `master` worktree after commit `64769d4`
- Venue/environment/account: Deribit/Testnet/`deribit-test`
- Transport: WebSocket JSON-RPC
- Commands: bounded `public-stream` and `private-stream`
- Expected: public trade/book notifications, authenticated private subscribe readiness, heartbeat test response, no reconnect in healthy window
- Actual: 8-second public run delivered 26 notifications from `trades.BTC-PERPETUAL.100ms` and `book.BTC-PERPETUAL.100ms`; private stream reached one authenticated ready generation; 22-second public run answered one `test_request`; all healthy runs used one connection and zero reconnects
- Result: PASS for read/subscription layer
- Limitation: no private order/execution event was generated in this read-only phase; that evidence remains required in the write E2E

## D-R3-002 — Raw public subscription behavior

- Expected: determine whether unauthenticated `.raw` can be a public default
- Actual: server returned typed code `13778 raw_subscriptions_not_available_for_unauthorized`
- Result: PASS (behavior identified); public defaults changed to `.100ms`, authenticated callers may request `.raw`

## D-R4-001 — Deribit HTTP minimum-size order lifecycle

- UTC time: 2026-09-09T16:03Z–16:06Z
- Venue/environment/account/transport: Deribit/Testnet/`deribit-test`/HTTP write + HTTP read + private WS
- Instrument/amount/unit: `BTC-PERPETUAL`, `10`, USD notional (metadata minimum), BTC settlement
- Expected: plan passes nonzero risk limits; execute requires confirmation; independent HTTP state is open; amend is independently visible; cancel is independently terminal; private events correlate by order ID/label; no residual position
- Actual: two connector-owned post-only orders were created within the 2% price band. Create and amend returned `verified=true`; cancel read returned `cancelled`; private `user.changes.any.any.raw` emitted matching `open` then `cancelled`; trade count and fee were 0; BTC future position count was 0
- Native IDs: sanitized in this report; retained only in local command evidence
- Result: PASS

## D-R4-002 — Deribit WebSocket minimum-size order lifecycle

- UTC time: 2026-09-09T16:12Z
- Venue/environment/account/transport: Deribit/Testnet/`deribit-test`/WebSocket write + canonical HTTP read
- Instrument/amount/unit: `BTC-PERPETUAL`, `10`, USD notional, BTC settlement
- Expected: no transport fallback/retry; HTTP read matches WS order ID and intent label; connector-owned cancel reaches terminal state
- Actual: WebSocket create was independently HTTP-verified, then HTTP cancel was independently read as `cancelled`; no fill was targeted
- Result: PASS

Real execution/fee reconciliation remains NOT_RUN here. Zero trades and zero fee are correct for the post-only cancellation lifecycle but do not satisfy the required live-fill evidence.

## D-R5-001 — Migration and reconciliation

- UTC time: 2026-09-09T16:20Z–16:22Z
- Expected: legacy state migrates only after dry-run; exact backup exists; reconciliation is idempotent; private-ready callback can recover
- Actual: dry-run validated 8 Bybit orders and 2 executions; apply created `state/orders.json.v1.bak`; Deribit reconciliation independently found and applied 3 connector-owned terminal orders; two consecutive reports had zero unresolved/multiple matches and zero new trades; private stream reached ready while running reconciliation
- Result: PASS
- Recovery: `./bin/venuewire state restore-v1` restores the retained backup; it was not invoked because v2 is the active schema

Crash/ambiguity fixture evidence: local tests cover accepted-before-local-ACK recovery, no-match staying `OutcomeUnknown`, multiple label matches becoming `NeedsReview`, duplicate pages, same-millisecond cursor ordering, and pagination no-progress failure. No unresolved intent is automatically resubmitted.

## MV-E2E-001 — Full command-level non-FIX E2E

- UTC time: 2026-09-09T16:46Z
- Gates: `RUN_MULTI_VENUE_E2E=1`, both venue read gates, and both venue trading gates
- Build: local worktree after commit `b332b9c`
- Commands: help; both venue time/doctor/metadata/ticker/account/portfolio/positions/orders; timed public/private streams; plan/execute; HTTP and WS creates; status/amend/cancel; executions/trades; reconciliation
- Independent evidence: every mutation was queried separately; private stream files contained the same client correlation IDs; ACK trade amount/fee sums matched canonical execution/trade-history reads; final positions were independently queried
- Result: PASS

### Minimum live fills and fees

| Venue | Instrument | Entry / cleanup | Native unit | Entry fee | Cleanup fee | Final position |
|---|---|---:|---|---:|---:|---:|
| Bybit Testnet | ETHUSDT linear | 0.01 / 0.01 | ETH, USDT collateral | 0.01385461 USDT | 0.01385456 USDT | 0 |
| Deribit Testnet | BTC-PERPETUAL | 10 / 10 | USD notional, BTC settlement | 0.00000006 BTC | 0.00000006 BTC | 0 (`direction=zero`) |

The cleanup orders were connector-owned and reduce-only. The E2E trap now tracks both open passive orders and residual filled exposure. An interrupted earlier run exposed an owned post-only order; it was matched to its persisted intent, cancelled by exact native ID, and independently read as `cancelled` before the passing rerun.

## D-R2-FIX-001 — Distinct dialect and local order lifecycle

- UTC time: 2026-09-09T17:41Z
- Build: local `master` worktree after commit `6e785ea`
- Venue/environment/account/transport: Deribit/local Testnet fixture/`deribit-test`/FIX 4.4
- Command: `venuewire --venue deribit fix mock-demo`
- Expected: Deribit SHA-256 Logon dialect, heartbeat, actual resend, forward reset, SecurityList multiplier proof, D/G/F and 8 lifecycle all pass without exchange credentials or external writes
- Actual: `LOCAL_TESTED`; authenticated fixture session; TestRequest echoed; original outbound sequence replayed with `43=Y` and `122`; reset recovered through callback; multiplier 10 converted minimum 10 USD to one contract; mock order accepted, replaced and ended `OrdStatus=4`
- Native order ID: local fixture `mock-order-1`
- Result: PASS
- Evidence: sanitized CLI JSON; `tradingWritePerformed=false`; targeted normal tests passed

## D-R2-FIX-002 — Real Deribit Testnet Logon

- UTC time: 2026-09-09T17:22Z
- Build: local `master` worktree after commit `6e785ea`
- Venue/environment/account/transport: Deribit/Testnet/`deribit-test`/TLS FIX 4.4
- Command: `venuewire --venue deribit fix connect-testnet --duration 3s`
- Gates: `DERIBIT_ENABLED=true`, `DERIBIT_FIX_ENABLED=true`, `RUN_DERIBIT_FIX_TESTS=1`; no trading gate enabled
- Expected: exact Testnet TLS endpoint accepts authenticated Logon and bounded clean Logout; no order message is sent
- Actual: authenticated Logon succeeded with `TargetCompID=DERIBITSERVER`; command exited 0 after requested duration; `tradingWritePerformed=false`
- Result: PASS (`TESTNET_LOGON` only)
- Evidence: sanitized CLI JSON and structured logs; no credential, nonce, RawData or password field retained

`TESTNET_LOGON` is not `TESTNET_ORDER_FLOW`. Phase 9 must still prove a real metadata-minimum FIX create/amend/cancel lifecycle using independent JSON-RPC order/trade/position reads and connector-owned cleanup.

## D-R2-FIX-003 — Real Deribit Testnet order flow

- UTC time: 2026-09-09T18:12Z–18:14Z
- Build: local `master` worktree after commit `22f6044`
- Venue/environment/account/transport: Deribit/Testnet/`deribit-test`/TLS FIX 4.4 with canonical HTTP JSON-RPC reads
- Instrument/amount/unit: BTC-PERPETUAL, 10 USD units, one FIX contract proven by `ContractMultiplier(231)=10`, BTC native collateral
- Commands: `order plan --transport fix`; confirmed execute (D); `order amend --transport fix` (G); `order cancel --transport fix` (F); independent status/trades/positions/open-order reads
- Expected: all three mutations require global + trading + FIX gates; no transport fallback; FIX report correlation survives server-replaced tag 11 and optional cancel fields; every mutation is independently visible through JSON-RPC; no trade or exposure remains
- Actual: D independently read `open` at 78000.0; G independently read `open` at 77999.5; F independently read terminal `cancelled`; trade history and open orders were empty; BTC future position was `direction=zero`, size 0
- Native IDs: exact IDs retained only in local command evidence; report uses sanitized identity
- Result: PASS (`TESTNET_ORDER_FLOW`)
- Evidence: each CLI mutation returned `verified=true`; final separate read commands agreed

Two earlier fail-closed probes caused no exposure and were independently checked: one stopped before D when FIX/JSON currency semantics were too strict; one accepted F but treated its sparse cancellation report as unknown. In both cases the connector did not retry, exact JSON order state was read, and open orders/position were zero before continuing. These observations became regression fixtures.

## MV-E2E-002 — Full command-level E2E including FIX

- UTC time: 2026-09-09T18:21Z–18:23Z
- Build: local `master` worktree after commit `22f6044`
- Gates: global Bybit/Deribit read and trading gates plus `RUN_DERIBIT_FIX_TESTS=1`; `RUN_BYBIT_FIX_TESTS=0`
- Expected: both local FIX command surfaces pass; unavailable Bybit live FIX is explicit; Deribit live Logon and passive D/G/F join the existing fully verified HTTP/WS/fill lifecycle; all cleanup is independently confirmed
- Actual: result `PASS`, `independentReads=true`, `privateEvents=true`, `positionsZero=true`; Deribit FIX `PASS`; Bybit live FIX `BLOCKED_GATE`
- Minimum-fill fees: Bybit ETHUSDT 0.01 ETH entry/cleanup fees `0.01364116` and `0.0136411` USDT; Deribit BTC-PERPETUAL 10 USD entry/cleanup fees `0.00000006` and `0.00000006` BTC
- Result: PASS, with Bybit live FIX separately BLOCKED_GATE
- Evidence: runner final JSON; canonical trade-history totals matched ACK totals; private correlation IDs observed; final order/position reads were zero

## MV-E2E-003 — Final command-level E2E and independent-read audit

- UTC time: 2026-09-09T19:37Z–19:39Z
- Build: code commit `d7f0031`; migration assertion correction `d65c1cc`
- Venue/environment/account: Bybit/Testnet/`bybit-test` and Deribit/Testnet/`deribit-test`
- Transports: Bybit REST/public and private WebSocket/local FIX; Deribit HTTP JSON-RPC/public and private WebSocket/local and live FIX
- Gates: `RUN_MULTI_VENUE_E2E=1`, both venue read/trading gates, and `RUN_DERIBIT_FIX_TESTS=1`; `RUN_BYBIT_FIX_TESTS=0`
- Instruments and live metadata: ETHUSDT linear, minimum/step `0.01 ETH`, tick `0.01`; BTC-PERPETUAL, minimum `10 USD`, contract size `10`, tick `0.5`, BTC settlement
- Expected: every command surface runs; every mutation is verified by a separate business-state read; private events correlate; migration is reversible; cleanup is connector-owned and reduce-only; final orders and positions are zero; unavailable live FIX is explicit
- Actual: runner returned `PASS`, `independentReads=true`, `privateEvents=true`, and `positionsZero=true`; Deribit FIX returned `PASS`; Bybit live FIX returned `BLOCKED_GATE`
- HTTP lifecycle: create/edit/cancel reads matched native identity, amended state, and terminal cancellation
- WebSocket lifecycle: amend independently read `open` at price `77968`; cancel independently read `cancelled`; no HTTP mutation fallback occurred
- Private recovery: connection COD was queried and enabled on the same connection; generation 1 delivered 10 `user.changes` notifications; the canonical reducer persisted five Deribit orders and two Deribit executions
- Migration: isolated v1 fixture dry-run reported 1 order, apply changed v1→v2 with a backup, and restore independently read version 1
- Minimum fills and fees: Bybit entry/cleanup `0.01367245`/`0.01367234 USDT`; Deribit entry/cleanup `0.00000006`/`0.00000006 BTC`
- Final independent reads: both venues had zero open orders and zero nonzero positions
- Result: PASS; Bybit live FIX remains separately `BLOCKED_GATE`
- Evidence: retained sanitized artifacts at `/tmp/tmp.wbIHm8XRRH` for this local run; no credentials, auth frames, account identifiers, or full native IDs are copied into this report

## Mandatory scenario matrix audit

Each row below names the authoritative regression test or external evidence. `MV-E2E-003` means the final Testnet run above, including its separate read artifacts—not merely a successful mutation response.

### Baseline, namespace, and authentication

| ID | Result | Evidence |
|---|---|---|
| B01 | PASS | `TestNormalizeVenueArgsPreservesLegacyBybitAndAddsSharedRouting`, `TestRunAllowsDeribitFIXMockWhenVenueIsDisabled`, and the full Bybit regression suite |
| B02 | PASS | `TestExecutionsDecodeFeeDetails`; MV-E2E-003 separately compares gross fill quantities and native-currency fee totals for entry and cleanup |
| B03 | PASS | `TestMigrationDryRunDoesNotWrite`, `TestMigrationBacksUpCanRestoreAndIsIdempotent`, `TestMigrationRefusesAmbiguousCategoryAndMissingMapping`, and MV-E2E-003 dry-run/apply/restore |
| N01 | PASS | `TestServiceIsolatesIdenticalOrderAndExecutionIDsAcrossVenues` and `TestCompoundKeysIsolateVenuesAndAccounts` |
| N02 | PASS | `TestCompoundKeyEscapesDelimiters`, `TestAccountKeyValidation`, and `TestIndependentServicesDoNotLoseEachOthersOrders` |
| A01 | PASS | `TestTokenIsCachedAcrossConcurrentPrivateReads` and `TestPrivateReadRefreshesOnceAfterAuthError` |
| A02 | PASS | `TestLoggerRedactsSensitiveKeysAndKnownSecrets`, `TestLoggerRedactsPEMInMessage`, `TestLoggerRecursivelyRedactsAnyValues`, and `TestServerRejectsWrongSecretWithoutEchoingSensitiveData` |
| A03 | PASS | `TestDeribitRejectsHostConfusionMainnetAndZeroRisk`, `TestMainnetAndHostConfusionAreRejected`, `TestTLSConfigDialerRejectsWrongHostname`, and both REST/JSON-RPC `TestClientRejectsRedirectWithoutForwardingCredentials` tests |

### RPC, metadata, and WebSocket correctness

| ID | Result | Evidence |
|---|---|---|
| R01 | PASS | `TestRPCBusinessAndMalformedErrorsAreTypedAndSanitized` and `TestRPCErrorRemainsTypedOnHTTP400` |
| R02 | PASS | `TestPrivateWSCODHandlesReorderedResponsesAndEarlyEvent` and `TestPlaceWSUsesPrivateRPCAndReturnsResult` |
| R03 | PASS | `TestWSReconnectResubscribesAndRunsRecoveryBeforeEvents`; pending requests are scoped to one connection generation |
| R04 | PASS | `TestPrivateWSCODHandlesReorderedResponsesAndEarlyEvent`, `TestPrivateUserChangesUseCanonicalOrderAndTradeReducer`, and persisted-state counts from MV-E2E-003 |
| M01 | PASS | `TestPlanRejectsMetadataAndRiskViolations` and `TestQuantityMetadataDistinguishesUSDSettlFromNativeCommissionCurrency` |
| M02 | PASS | `TestPlanUsesEffectiveSegmentedTick`, `TestEffectiveTickSizeUsesHighestStrictlyLowerStep`, and `TestEffectiveTickSizeRejectsMalformedMetadata` |
| M03 | PASS | `TestPlaceParamsEncodeDecimalsAsJSONNumbers`; planner and FIX conversions use exact rational/decimal text rather than `float64` arithmetic |
| M04 | PASS | `TestPlanRejectsMetadataAndRiskViolations` rejects inactive, unsupported-settlement, below-minimum, and invalid-risk plans before transport |
| W01 | PASS | `TestWSAnswersHeartbeatTestRequest` verifies `test_request` → `public/test` |
| W02 | PASS | `TestOrderBookSnapshotDeltaDuplicateAndGap` verifies snapshot, delta, and duplicate idempotence |
| W03 | PASS | `TestOrderBookSnapshotDeltaDuplicateAndGap` verifies a change-ID gap remains stale until a fresh snapshot |
| W04 | PASS | `TestWSQueueOverflowFailsClosed`, `TestPrivateQueueOverflowFailsConnectionForReconciliation`, and `TestRecoveryBufferOverflowFailsClosed` |

### Order intents, state, and recovery

| ID | Result | Evidence |
|---|---|---|
| O01 | PASS | `TestConcurrentExecutionClaimsOnce` and the cross-process locked intent store |
| O02 | PASS | `TestPlaceWSClassifiesPostWriteDisconnectUnknown` and `TestPrivateWriteDoesNotRetryAmbiguousTransportFailure` |
| O03 | PASS | `TestRecoveryNeverGuessesZeroOrMultipleMatches` |
| O04 | PASS | `TestDuplicateExecutionDoesNotDoubleCount`, `TestPrivateUserChangesUseCanonicalOrderAndTradeReducer`, and canonical trade-ID persistence in MV-E2E-003 |
| O05 | PASS | `TestRequiredStateTransitions` and `TestMockOrderScenarios` preserve cumulative fills when cancellation becomes terminal |
| O06 | PASS | `TestCancelFillRaceEndsFilled`, `TestCancelledThenConfirmedFullFillEndsFilled`, and `TestMockCancelAcceptedAndCancelFillRace` |
| O07 | PASS | `TestLateRESTAckCannotRegressWebSocketState` and `TestAmendRejectWithoutIdentifiersUsesSendOrderCorrelationWithoutDisconnectError` |
| C01 | PASS | `TestRecoveryAdoptsExactlyOneOrderAndDeduplicatesTrades`; the durable `Executing` claim is recovery input and is never automatically resubmitted |
| C02 | PASS | `TestPaginationSameMillisecondCursorAndNoProgressGuard` and `TestDuplicateExecutionDoesNotDoubleCount` |
| C03 | PASS | `TestRecoveryNeverGuessesZeroOrMultipleMatches` plus bounded pagination/no-progress handling |
| C04 | PASS | `TestRestartLoadsSnapshotThenRepairs` and the restart reconciliation fixture in `TestReconciliationRequiredOrderScenarios` |
| C05 | PASS | `TestLateRESTAckCannotRegressWebSocketState`, `TestWSReconnectResubscribesAndRunsRecoveryBeforeEvents`, and `TestPrivateUserChangesUseCanonicalOrderAndTradeReducer` |
| C06 | PASS | `TestRepeatedReconciliationProducesSameStateAndNoSecondRepair` and repeated Deribit recovery in D-R5-001 |

### COD, venue isolation, FIX, and shutdown

| ID | Result | Evidence |
|---|---|---|
| D01 | PASS | `TestCancelOnDisconnectReadsRequestedScope`, `TestDeribitConnectionCODRequiresGeneralTradingGates`, and MV-E2E-003 explicitly cancels/reads HTTP orders instead of treating connection COD as protection |
| D02 | PASS | `TestWSReconnectResubscribesAndRunsRecoveryBeforeEvents`, `TestRunCancellationSendsLogoutAndJoinsReader`, and final independent order reads in MV-E2E-003 distinguish configured policy from actual exchange state |
| V01 | PASS | `TestAllVenueStatusReportsFailuresIndependently`; the healthy venue result remains present beside the failed venue error |
| V02 | PASS | `TestRequestLimiterHonorsContextCancellation` and per-connector reconnect/rate-limit state in `TestWSReconnectResubscribesAndRunsRecoveryBeforeEvents` |
| V03 | PASS | `TestAllVenueStatusReportsFailuresIndependently` and the `--venue all portfolio` partial-result assertion in MV-E2E-003 |
| V04 | PASS | `TestAllVenueStatusReportsFailuresIndependently`; MV-E2E-003 retains BTC, USD-notional, ETH, and USDT in native units without fabricated totals |
| F01 | PASS | `TestLogonAuthenticationVectorAndExplicitPolicies`, `TestLogonRejectsMissingConfigurationAndRandomFailure`, and `TestLogonSurvivesClockRollbackAndChangesDigestWithSecret` |
| F02 | PASS | separate `internal/fix` and `internal/deribitfix` suites plus both local FIX demos in MV-E2E-003 |
| F03 | PASS | FIX codec/framer tests, `FuzzFIXParser`, and `TestParseSecurityListPreservesGroupsAndValidatesMetadataConversion` |
| F04 | PASS | `TestSessionLogonHeartbeatResendResetAndApplication`, `TestResendRequestReplaysApplicationWithOriginalSequence`, and `TestSequenceResetIgnoresHeaderSequenceAndMovesToNewSequence` |
| F05 | PASS | `TestExecutionReportCorrelatesOrigClOrdIDNotReplacedTag11` and live D/G/F JSON identity reads in D-R2-FIX-003 |
| F06 | PASS | `TestSecurityListRejectsUnknownNestedGroupAndMismatchedMultiplier`; live FIX order entry is blocked until JSON/FIX quantity proof succeeds |
| F07 | PASS | D-R2-FIX-003 and MV-E2E-003 use canonical HTTP JSON-RPC order/trade IDs for independent verification and fee deduplication |
| S01 | PASS | `TestExpiredClaimPersistsTerminalStatus`, `TestPlaceWSClassifiesPostWriteDisconnectUnknown`, `TestRunCancellationSendsLogoutAndJoinsReader`, and root signal-context shutdown leave a durable recovery state with bounded connection teardown |
| S02 | PASS | `TestMainnetAndHostConfusionAreRejected`, `TestDeribitFIXTradingRequiresAllIndependentGates`, and CLI routing rejects `--venue all` writes; no transfer/withdrawal commands exist |

All 48 mandatory matrix IDs are covered. The only external capability not enabled is Bybit live FIX; the specification permits its whitelist/RSA-dependent validation to remain explicitly `BLOCKED_GATE`, while its codec/session/order lifecycle remains locally tested.
