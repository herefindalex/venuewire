# Protocol notes

Verified against the official Bybit documentation on 2026-09-09.

- REST Testnet is `https://api-testnet.bybit.com`; HMAC plaintext is timestamp + API key + receive window + exact query/body bytes.
- Public Linear WS is `wss://stream-testnet.bybit.com/v5/public/linear`.
- Private WS is `wss://stream-testnet.bybit.com/v5/private`; categorized topics are `order.linear`, `execution.linear`, and `position.linear`.
- REST execution records preserve `execFee`, `feeRate`, and `feeCurrency`; Spot buy quantities are gross and the received wallet amount may be lower when fees are charged in the base asset.
- FIX order entry is Spot-only at `fix-oe-testnet.bybit.com:9000` over TLS and requires self-generated RSA credentials plus whitelist access.
- FIX create is `NewOrderSingle(D)`.
- FIX cancel is `OrderCancelRequest(F)`. Acceptance is `OrderCancelAck(XCA)` with `ExecType/OrdStatus=6`; terminal cancel arrives in `ExecutionReport(8)` when ResponseMode is Everything.
- FIX amend is Bybit `AtomicReplaceRequest(XAR)`, not standard `OrderCancelReplaceRequest(G)`. Acceptance is `OrderAmendAck(XAA)` with `ExecType/OrdStatus=E`; amended state follows as `ExecutionReport(8)` with `ExecType=5` and current `OrdStatus`.
- Rejected XCA/XAA may omit order identifiers and `150/39`; the router retains send-order FIFO correlation and surfaces `103/58`.
- Application requests carry `Timestamp(30002)`, `RecvWindow(30001)`, optional `ReqId(30006)`, and `Category(30010)=spot`.
- Bybit FIX starts every new session at sequence 1 and provides no standard resend/gap-fill recovery. REST reconciliation is mandatory after reconnect.

References: the official integration guide and create, cancel, amend, and execution-report pages linked from `03_PROTOCOL_SPEC.md`.
