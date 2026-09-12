# VenueWire v0.1.0 Release Notes Draft

Suggested release title:

```text
VenueWire v0.1.0 - Bybit and Deribit Multi-Venue Testnet Console
```

This draft prepares a release description. It is not a published release and should not be published until a license is selected and the repository settings checklist is reviewed. Hosted CI and secret scanning passed on `master` at commit `8c03731986b0095c88d152a1362f45ef5f6399b9`.

## Highlights

VenueWire is a Testnet-only trading-connectivity and execution prototype for Bybit and Deribit. It combines a Go backend, an authenticated Vue 3 Web Console, venue-specific REST or JSON-RPC clients, public and private WebSockets, persistent order state, reconciliation, and FIX 4.4 implementations.

- Switch between normalized Bybit and Deribit account views.
- Review and submit protected Testnet Spot Limit IOC orders through the Web Console.
- Persist durable trade intents and idempotency state before venue submission.
- Preserve uncertain transport outcomes as `Unknown` and reconcile without automatic resubmission.
- Track partial fills, zero fills, fees, net received amounts, account synchronization, stream freshness, and rate-limit health.
- Retain the CLI for engineering validation of venue transports, state, and recovery.

## Supported validation level

- Bybit REST V5 and public/private WebSocket Testnet flows are verified.
- Deribit HTTP JSON-RPC and public/private WebSocket Testnet flows are verified.
- Small Bybit `BTCUSDT` and Deribit `BTC_USDC` browser Spot flows reconciled to terminal fills with fee and balance evidence.
- Deribit FIX 4.4 logon and a real Testnet D/G/F order flow are verified through independent JSON-RPC reads.
- Bybit FIX 4.4 is locally tested; live Testnet FIX remains blocked by the external whitelist/RSA gate.
- The public HTTPS/WSS Nginx path was exercised. Privileged host-firewall state was not directly inspected.

See [Current status](../STATUS.md), [Capability matrix](../CAPABILITIES.md), and [V3 test report](../v3/TEST_REPORT_V3.md) for the evidence boundaries.

## Safety boundary

- Testnet only; no Mainnet or real funds.
- No withdrawals or transfers.
- No automatic routing, strategy engine, arbitrage system, or HFT claim.
- Browser writes remain disabled unless the operator explicitly enables the required independent gates.
- Local and CI verification do not require exchange credentials or place orders.

## Before publishing

- [ ] Select MIT or Apache-2.0 and add the standard license text.
- [x] Verify hosted [CI](https://github.com/herefindalex/venuewire/actions/runs/34677964751) and [Gitleaks](https://github.com/herefindalex/venuewire/actions/runs/34677964735) results in GitHub.
- [ ] Complete the manual [GitHub settings checklist](GITHUB_SETTINGS.md).
- [ ] Review the public screenshot and release text for private identifiers.
- [ ] Confirm any demo access language and distribution process.

Do not publish this release automatically.
