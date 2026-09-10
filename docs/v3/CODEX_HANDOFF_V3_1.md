# VenueWire V3.1 公開 Testnet Web Console 交接

更新日期：2026-09-10
交付狀態：**本機實作完成，外部驗證待辦**

## 已完成內容

VenueWire 現在以登入式 Vue 3 Web Console 作為面試展示主介面，Go 後端繼續沿用既有 Bybit／Deribit HTTP、JSON-RPC、WebSocket、FIX、持久化資料與 CLI。沒有重建 connector，也沒有把 V2 的合約／FIX 語意誤套到新的 Spot Quick Trade。

- 程式可用 `--env-file` 自行載入 dotenv；已存在的 OS 變數（包含明確空值）優先。
- 固定帳密登入、絕對 session expiry、server-side logout、Secure/HttpOnly/SameSite cookie、CSRF、Host/Origin 與 trusted proxy 邊界已完成。
- Browser 可獨立切換 Bybit／Deribit，讀取後端 cache 的 normalized account snapshot；Browser 數量不直接觸發交易所 API。
- 首頁分開顯示 exchange-reported USD 與 public-price local USD mark。部分定價只顯示 priced subtotal；USDT 不假定等於一美元。
- Browser WebSocket 提供初始 snapshot、連續 sequence、process instance ID、revision 防倒退、bounded resync，以及 account／valuation／trade／health events。
- Quick Trade 固定支援 Bybit BTC↔USDT 與 Deribit ETH↔BTC Spot。Review → Confirm 使用五秒 quote、0.5% 保護、Limit IOC、exact decimal、metadata／fee／capacity 驗證及 no-borrow submission。
- Confirm 先持久化 intent、冪等索引、quota 與 reservation，再只送一次。`Unknown` 不重送且保留唯一 concurrent slot；重啟、private reconnect、週期與 Recheck 共用 serialized reconciliation。
- Filled、partial-cancel、zero-fill、Rejected、Unknown、實際 fee、net received 與 balance sync 分離語意已完成。
- System Status 顯示 public/private receive/event age、reconnect、REST order RTT、ACK→order/execution event、rate limit、reconciliation 狀態與 discrepancy。
- Recent Trades 為共用 demo history；重新登入或 Browser reload 可看到未完成交易。Recheck 只查詢，不會 resubmit／force resolve。
- V3.1 §23 的 15 個故障情境皆有 deterministic local fixtures。

## 已驗證結果

```text
go test ./... -count=1 -timeout=180s                 PASS：390 tests / 23 packages
go test -race ./... -count=1 -timeout=240s           PASS：390 tests / 23 packages
go vet ./...                                          PASS
npm --prefix web run typecheck                        PASS
npm --prefix web test                                 PASS：6 tests / 2 files
npm --prefix web run build                            PASS
make build                                             PASS：bin/venuewire
```

完整逐項證據見 `docs/v3/TEST_REPORT_V3.md`；階段歷史見根目錄 `IMPLEMENTATION_STATUS.md`。

## 建置與啟動

```bash
make build
sha256sum ./bin/venuewire
./bin/venuewire --env-file /absolute/path/to/venuewire.env web
```

未指定 `--env-file` 時，程式依 executable 位置先讀 `bin/.env`，再讀上一層 `.env`；同名檔案設定以 binary 同目錄優先，OS environment 仍高於兩者。因此也可直接執行 `./bin/venuewire web`，不受目前 working directory 影響。

`make build` 會執行 locked frontend install、typecheck/build 與 `webui` embed Go build。單純 CLI 仍可獨立建置：

```bash
go build -o ./bin/venuewire ./cmd/venuewire
```

不要先 `source .env`。從根目錄 `.env.example` 建立 mode-600 的未追蹤設定檔；`docs/3_web/.env.example` 是相同內容的規格附件。填入：

- Go 主機自己的 private `WEB_HOST` 與 `WEB_PORT`。
- 唯一 HTTPS `WEB_PUBLIC_ORIGIN`。
- Go 實際看到的 Nginx `/32` `WEB_TRUSTED_PROXY_CIDRS`。
- `WEB_USERNAME`、至少 12 字元的 `WEB_PASSWORD`、至少 32 bytes entropy 的 `WEB_SESSION_SECRET`。
- enabled venues 的 Testnet credentials；Deribit 沿用 `DERIBIT_API_KEY`／`DERIBIT_API_SECRET`。

先保持 `WEB_TRADING_ENABLED=false`。V3.1 canonical quote 設定為 `QUOTE_TTL=5s`，Demo limits 使用文件中的 session 10、rolling hour 30、global concurrent 1，以及各 venue source-asset caps。

## 部署邊界

正式拓撲必須是 Browser HTTPS/WSS → 既有 Nginx → private HTTP → VenueWire Go。Go 不做 TLS，也不能將 port 直接公開至 Internet。

參考檔案：

- `docs/3_web/deploy/nginx-http-map.conf`
- `docs/3_web/deploy/nginx-proxy-common.conf`
- `docs/3_web/deploy/nginx-https-locations.conf`
- `docs/v3/DEPLOYMENT.md`

Nginx 必須 overwrite XFF/Real-IP、保留公開 Host、禁止 upstream retry 重送 trade POST，並支援 `/api/ws` upgrade。只有部署人員核對真實 IP、ACL、既有 locations 與憑證後才能執行 `nginx -t`／reload。

## 尚未執行的外部驗證

本輪沒有獲得下列外部副作用授權，因此全部正確標為 `NOT_RUN`：

- V3.1 Web process 的 Bybit Testnet account/public/private stream 實測。
- V3.1 Web process 的 Deribit Testnet account/public/private stream 實測。
- 透過 Browser 執行 Bybit／Deribit Spot Testnet 訂單。
- 真實 Nginx、憑證、防火牆、ACL、systemd 或 split-host HTTPS/WSS 操作。
- 真實部署 UI 的去敏感化 screenshots／failure recordings。

舊 V2 已完成的 Testnet/FIX evidence 保留在 `docs/upgrade/VALIDATION_REPORT.md`，但不能拿來宣稱新的 Browser order flow 已驗證。

## 外部驗證時的操作原則

1. 先以 read-only 模式驗證兩家 account snapshot、public/private stream freshness 與 WSS。
2. 核對帳戶現有 open orders、balances、API permissions 與 Demo caps。
3. 取得明確 Testnet 下單授權後才切換 `WEB_TRADING_ENABLED=true` 及必要 venue gate。
4. 每次只做已選定的小額方向，保存 VenueWire/client/venue ID 與獨立 reconcile 證據。
5. 如果結果為 `Unknown`，保留它並用 Recheck 查詢；絕不可再次 Confirm 或自行解除 slot。
6. 截圖／錄影前移除帳密、cookie、account ID、hostname、private IP、request auth、API/FIX secret。
7. 測完將 Web trading 關閉；任何 cleanup 都必須先確認 connector ownership 與獨立終態。

## MVP 的明確限制

- 面試者只使用 Web；CLI 是工程測試介面，不做高強度 DX 投資。
- 單一 shared login 與 shared Recent Trades 是已確認的 Demo 設計。
- 不提供 public fault simulator。
- 不做 CLI/Web 強一致、多 process/multi-instance quota coordination。
- 不支援 Mainnet、轉帳、提款、策略、自動交易、套利線、完整 order book、第三交易所、smart routing 或 cross-venue failover。
- 外部清單完成前，不能將交付名稱改成「全部完成」；正確描述仍是「本機實作完成，外部驗證待辦」。
