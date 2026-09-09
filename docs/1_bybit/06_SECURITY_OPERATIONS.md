# 06 — Security and Operations Requirements

## 1. Credential Policy

The project must use environment variables.

Recommended names:

```text
BYBIT_ENV=testnet
BYBIT_API_KEY=
BYBIT_API_SECRET=

BYBIT_FIX_API_KEY=
BYBIT_FIX_PRIVATE_KEY_PATH=
```

The ordinary REST HMAC key and FIX RSA key are different credential models.

### Important

Credentials previously pasted into a chat must be considered exposed.

They must **not** be copied into this repository, test fixtures, docs, shell history examples, source code, or generated files.

Create fresh Testnet credentials before live integration testing.

## 2. `../../.env.example`

Allowed:

```text
BYBIT_ENV=testnet
BYBIT_API_KEY=
BYBIT_API_SECRET=
BYBIT_FIX_API_KEY=
BYBIT_FIX_PRIVATE_KEY_PATH=
```

Not allowed:

```text
BYBIT_API_KEY=actual-key
BYBIT_API_SECRET=actual-secret
```

## 3. Git Ignore

At minimum:

```text
.env
.env.*
!.env.example
*.pem
*.key
secrets/
data/
```

Adjust `data/` if non-sensitive fixtures need tracking; keep live state snapshots untracked.

## 4. Mainnet Guard

This project is Testnet-only.

Authenticated commands must fail fast if:

- base URL resolves to mainnet;
- WebSocket host is mainnet;
- `BYBIT_ENV != testnet`.

Do not add a casual `--mainnet` switch.

If mainnet support is ever added, it must be a separate deliberate change outside this specification.

## 5. Logging

Never log:

- API secret;
- RSA private key;
- full signatures;
- raw private authentication frames if they expose credential material.

Safe identifiers:

- truncated/hashed API key fingerprint;
- order ID;
- orderLinkId;
- FIX SenderCompID;
- FIX message type;
- sequence number;
- rate-limit headers.

## 6. Clock

Authenticated REST/FIX requests depend on time validity.

Requirements:

- use UTC;
- allow server-time diagnostic command;
- warn on material clock skew;
- document that host should be NTP-synchronized.

Do not silently compensate for massive clock drift without surfacing it.

## 7. Retry Policy

### Safe to retry carefully
- public GET;
- read-only authenticated GET;
- connection establishment;
- subscription after reconnect.

### Dangerous
Order placement is not blindly retry-safe.

If an order submission times out after bytes may have reached the exchange:

1. do not immediately submit a second logical order;
2. query by `orderLinkId` / reconcile;
3. only submit again after establishing the original was not accepted.

This behavior should be visible in code and tests.

## 8. Rate Limits

Use response-provided rate-limit information where available.

On limit exhaustion:

- do not busy-loop;
- honor reset/backoff;
- emit structured warning;
- keep critical private stream handling alive.

## 9. Shutdown

On SIGINT/SIGTERM:

1. stop accepting new commands;
2. cancel contexts;
3. flush/persist local state;
4. close WS cleanly when possible;
5. send FIX Logout if session is active;
6. close network transports;
7. exit within a bounded timeout.

## 10. Operational Runbook

`demo.md` should include troubleshooting for:

- REST invalid signature;
- timestamp outside recv window;
- insufficient test balance;
- invalid tick size / quantity;
- WS authentication failure;
- rate limit;
- FIX TLS failure;
- FIX RSA key mismatch;
- FIX `access denied` due to whitelist;
- state discrepancy after reconnect.
