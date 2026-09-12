# VenueWire Current Status

Updated: 2026-09-12

This is the authoritative summary of VenueWire's current implementation and validation level. Historical phase records remain in [`IMPLEMENTATION_STATUS.md`](../IMPLEMENTATION_STATUS.md), while detailed evidence remains in the linked test and deployment reports.

## Status definitions

| Status | Meaning |
|---|---|
| `IMPLEMENTED` | The capability exists in the repository, but the evidence cited here does not establish execution in the target environment. |
| `LOCAL_TESTED` | Automated tests, fixtures, or local builds exercised the capability without claiming a real venue or deployment result. |
| `TESTNET_VERIFIED` | Sanitized evidence records an exercised Bybit or Deribit Testnet flow or the deployed Testnet browser path. |
| `BLOCKED` | Validation requires an external prerequisite that is not currently available. |
| `NOT_RUN` | The relevant validation has not been performed or directly inspected. |

## Current capability status

| Capability | Status | Evidence and boundary |
|---|---|---|
| Bybit REST V5 reads and order lifecycle | `TESTNET_VERIFIED` | Multi-venue E2E and V3 browser Testnet evidence in [`docs/upgrade/VALIDATION_REPORT.md`](upgrade/VALIDATION_REPORT.md) and [`docs/v3/TEST_REPORT_V3.md`](v3/TEST_REPORT_V3.md). |
| Bybit public and private WebSocket | `TESTNET_VERIFIED` | Testnet streams and browser freshness state were exercised; local reconnect and ordering behavior also has deterministic tests. |
| Deribit HTTP JSON-RPC reads and order lifecycle | `TESTNET_VERIFIED` | Read, create, amend, cancel, fill, fee, and independent-read evidence appears in the multi-venue validation report. |
| Deribit public and private WebSocket | `TESTNET_VERIFIED` | Bounded Testnet subscriptions, private order events, and deployed browser WSS were exercised. |
| Browser Spot order lifecycle | `TESTNET_VERIFIED` | Small Bybit `BTCUSDT` and Deribit `BTC_USDC` Limit IOC trades reconciled to terminal fills with fees and balance sync. |
| Durable intents, idempotency, and uncertain-outcome recovery | `LOCAL_TESTED` | The 419-test suite and all 15 V3.1 failure scenarios cover duplicate confirmation, restart, reconnect, `Unknown`, and terminal reconciliation. |
| Browser HTTPS/WSS through Nginx | `TESTNET_VERIFIED` | Headless browser verification exercised login, venue switching, account views, HTTPS, and WSS against the public origin. |
| Split-host Nginx to private Go service | `TESTNET_VERIFIED` | The public browser/API/WSS path passed and the operator reported a successful Nginx syntax test and reload. Privileged configuration was not directly inspected. |
| Go-host firewall ACL | `NOT_RUN` | The required ACL is documented, but privileged firewall state and direct-port denial were not directly inspected by the validation process. |
| Bybit FIX codec, session, and order lifecycle | `LOCAL_TESTED` | Local authenticated FIX fixtures cover logon, resend/reset, create, replace, cancel, and recovery. |
| Bybit FIX real Testnet flow | `BLOCKED` | Live validation remains behind the external Bybit FIX whitelist/RSA gate. Local FIX coverage is not presented as Testnet verification. |
| Deribit FIX real Testnet flow | `TESTNET_VERIFIED` | Real Testnet FIX logon plus D/G/F order flow was independently verified through canonical JSON-RPC reads. |
| Repository CI definition | `IMPLEMENTED` | `.github/workflows/ci.yml` runs Go tests/vet/race, frontend typecheck/lint/tests/build, and the full embedded build without credentials. The hosted [CI run](https://github.com/herefindalex/venuewire/actions/runs/34677964751) passed on `master` at commit `8c03731986b0095c88d152a1362f45ef5f6399b9`. |
| Repository secret scanning definition | `IMPLEMENTED` | `.github/workflows/secret-scan.yml` performs a full-history checkout and Gitleaks scan. The hosted [Gitleaks run](https://github.com/herefindalex/venuewire/actions/runs/34677964735) passed on `master` at commit `8c03731986b0095c88d152a1362f45ef5f6399b9`; push protection is a separate repository setting. |

## Current local verification

The pre-polish baseline on 2026-09-12 passed 419 Go tests, the full race suite, `go vet`, frontend typecheck, 13 frontend tests, and the frontend build. See [`docs/public-repo/BASELINE.md`](public-repo/BASELINE.md).

## Claims deliberately not made

- Mock coverage is not Testnet verification.
- A verified public reverse-proxy path is not proof that the host firewall ACL is correct.
- A FIX logon is not a FIX order lifecycle.
- Passing hosted workflow runs do not prove that branch protection, push protection, Dependabot, or other account-level or repository-level settings are enabled.
- This Testnet evidence does not establish Mainnet or production readiness.
