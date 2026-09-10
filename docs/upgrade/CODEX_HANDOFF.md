# Bybit + Deribit 多交易所升級交接

更新日期：2026-09-09

## 完成內容

本專案在既有 Bybit Testnet connector 中增量加入 Deribit，沒有另開不相干的程式，也沒有移除 Bybit。未指定 `--venue` 時，舊 Bybit CLI 行為仍是預設。

- Phase 0–7：完成 R1 的 venue routing、複合 identity、Deribit HTTP/WS、plan→execute、持久化、恢復、唯讀彙總與外部生命週期。
- Phase 8–9：完成獨立 Deribit FIX dialect、本機 mock、真實 Testnet Logon 與 D/G/F order flow。
- Phase 10：補齊 COD、分段 tick、private-event reducer、完整 WS create/edit/cancel、命令級 E2E 與共享文件。

主要正確性保證：

- identity 使用 venue/environment/account/namespace/native ID，兩所同號 order/trade 不會互相覆蓋。
- Deribit JSON-RPC 驗證 `jsonrpc`、request ID、result/error；token refresh 採 single-flight，只有唯讀呼叫可在 auth/rate-limit 條件下有界重試。
- Deribit HTTP/WS 數量與價格以 JSON number 傳送，內部驗證使用精確 decimal，不用 `float64` 做交易計算。
- `BTC-PERPETUAL`/`ETH-PERPETUAL` 的 HTTP/WS amount 是 USD notional，結算與費用仍是原生 BTC/ETH。Deribit 帳戶雖有 USDT，也不能把這兩個反向永續合約假裝成 USDT 結算。
- metadata 保留 `tick_size_steps`；plan 依價格所在區間選有效 tick，在邊界不默默放大或四捨五入。
- Deribit 新單必須先 plan，再於 TTL 內 `execute --confirm`。HTTP、WS、FIX transport 都是明確選擇，失敗時不換 transport 重送。
- HTTP、WS、FIX 的 create/edit/cancel 都以另一條 HTTP JSON-RPC read 驗證；ACK 本身永遠不算完成證據。
- private WS 解析單一 `user.changes` 中所有 orders/trades/positions；orders 與 canonical `trade_id` 走 reconciliation 使用的同一 reducer，重複費用不會再入帳。
- cleanup 只處理本次 connector-owned exposure，使用 reduce-only，最後再次讀取 position 為零。

## Build 與設定

```bash
go build -o ./bin/venuewire ./cmd/venuewire
cp .env.example .env
chmod 600 .env
set -a
source .env
set +a
./bin/venuewire help
```

Deribit 使用 `DERIBIT_API_KEY` 與 `DERIBIT_API_SECRET`；JSON-RPC 與 FIX 共用這組 Testnet client credentials。不要把值寫進程式、文件、log 或 commit。`.env` 已由 `.gitignore` 排除，本次工作沒有修改或刪除 `.env`。

`.env.example` 包含對稱 gates：

- `RUN_BYBIT_READ_TESTS`、`RUN_BYBIT_TRADING_TESTS`、`RUN_BYBIT_FIX_TESTS`
- `RUN_DERIBIT_READ_TESTS`、`RUN_DERIBIT_TRADING_TESTS`、`RUN_DERIBIT_FIX_TESTS`
- 所有 E2E 寫入另外要求 `RUN_MULTI_VENUE_E2E=1`

不保留舊 `RUN_BYBIT_INTEGRATION` / `RUN_BYBIT_WS_INTEGRATION` 相容名稱。

## COD 行為

`doctor` 會唯讀顯示 account-scope Cancel-on-Disconnect。`private-stream` 會在同一 WS connection 查詢 connection-scope COD，預設不修改。

如要啟用，只能明確執行：

```bash
RUN_MULTI_VENUE_E2E=1 RUN_DERIBIT_TRADING_TESTS=1 \
  ./bin/venuewire --venue deribit private-stream \
  --duration 10s --enable-connection-cod --confirm
```

這不會修改 account scope。COD 也不是同步取消保證；斷線後仍須查詢 order/trade。HTTP 或其他 connection 建立的訂單不會被錯誤標為受這條 connection 保護。

## 真實 Testnet 驗證

最後完整 runner 在 2026-09-09T19:37Z–19:39Z 通過，build 基於 `d7f0031`，migration assertion 修正為 `d65c1cc`：

```json
{
  "result": "PASS",
  "independentReads": true,
  "privateEvents": true,
  "positionsZero": true,
  "fix": { "bybit": "BLOCKED_GATE", "deribit": "PASS" }
}
```

最新 minimum-fill 費用：

- Bybit ETHUSDT linear：entry/cleanup 都是 `0.01 ETH`；費用 `0.01367245` / `0.01367234 USDT`。
- Deribit BTC-PERPETUAL：entry/cleanup 都是 `10 USD` notional；費用 `0.00000006` / `0.00000006 BTC`。

額外獨立證據：

- Deribit WS amend read：`verified=true`、state `open`；WS cancel read：`verified=true`、state `cancelled`。
- private stream：connection COD query `enabled=true`、ready generation 1、收到 10 個 notifications。
- private reducer 在隔離 state 中保存 5 張 Deribit orders、2 筆 canonical executions。
- runner 後另以 read API 核對：Bybit/Deribit open orders 都是 0，nonzero positions 都是 0。
- migration dry-run/apply/restore 通過，backup mode `0600`。

## FIX 驗證層級

| 層級 | Deribit | 證據 |
|---|---|---|
| `IMPLEMENTED` | PASS | 獨立 auth/session/codec/order dialect |
| `LOCAL_TESTED` | PASS | fixture/mock、heartbeat、resend/reset、SecurityList、D/G/F/8/9 |
| `TESTNET_LOGON` | PASS | `fix-test.deribit.com:9883` 真實 TLS Logon/Logout |
| `TESTNET_ORDER_FLOW` | PASS | 10 USD BTC-PERPETUAL D/G/F，JSON-RPC 獨立核對 |

Deribit FIX 使用 `TargetCompID=DERIBITSERVER`、32-byte nonce、strictly increasing timestamp，以及 `Base64(SHA256(RawData || client_secret))`，不是 Bybit RSA。SecurityList 先證明 JSON amount 與 FIX contracts/multiplier 的換算才允許 live order。

Bybit FIX 本機 mock 為 PASS，但 live Bybit FIX 仍是 `BLOCKED_GATE`：沒有啟用獨立 RSA credentials/whitelist gate。不得把這項寫成 live PASS。

## 已知限制與下一步

- 沒有 dashboard/UI；Phase 0 已確認唯一既有使用者介面是 CLI，因此沒有 UI 可遷移。
- ShellCheck 未安裝，狀態記為 `BLOCKED_TOOLING`；`bash -n` 已通過，不能把 ShellCheck 寫成 PASS。
- 支援商品刻意限定 BTC/ETH perpetual；不含 options、dated futures、portfolio margin orchestration、cross-venue smart routing、wallet transfer 或 Mainnet。
- Bybit live FIX 需使用者另行提供並啟用該 venue 的 RSA/whitelist access；這不影響 Deribit R2 已達 `TESTNET_ORDER_FLOW`。

完整命令見 `docs/upgrade/DEMO.md`；逐項測試與外部證據見 `docs/upgrade/VALIDATION_REPORT.md`。
