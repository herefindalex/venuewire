# Multi-Venue Trading Console V3 — 正式增量變更規格

**變更編號：CR-003 · 版本：3.0 · 日期：2026-09-10**  
**交付對象：Codex · 適用範圍：既有 Go Bybit + Deribit 專案**  
**主要目標：在已完成的交易所串接之上，加入可經 Nginx HTTPS 對外展示的登入式 Web 交易主控台。**

> 本文件是開發與驗收要求，不是現有程式已通過測試的證明。使用者回報兩家交易所已接通，但本次未提供儲存庫原始碼。Codex 必須以 Phase 0 盤點確認實際功能；不得把舊規格中的所有項目當成已完成，也不得為了 Web 改版重新建立另一套交易核心。
>
> 舊版參考：`DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md`。本文件明確覆蓋「不新建完整 Web 前端」及「Deribit 第一版只支援反向永續合約寫入」的範圍限制：**V3 新增登入式 Web，以及指定 Spot 商品的快速交易能力；既有合約／FIX／CLI 功能保留，不能混用商品單位。**

---

> V3.1 實作需一併閱讀 `VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md` 與其 §26 已確認決策。衝突處以 §26 為準，特別是全站 Demo 並行限額；設定範例沿用 V3 名稱，`QUOTE_TTL=5s`。目前差異分析見 `../v3/V3_1_GAP_ANALYSIS.md`。

## 0. Codex 起始指令與實作原則

先讀取儲存庫的 `AGENTS.md`、`README.md`、現有規格、交接文件與本文件。完成 Phase 0 後，依序實作，不要只交付計畫便停止。

1. **增量修改。** 沿用現有 Bybit／Deribit adapters、訂單追蹤、成交去重、持久化、恢復與測試。既有 dashboard 若可沿用就整合，不另建互不相干的後端。
2. **Testnet only。** 新 Web 交易只做指定 Spot，不使用真錢、不提款、不轉帳、不自動借幣、不調整槓桿／保證金／帳戶設定，不以 Mainnet 作為 fallback。
3. **不得使用曾貼在對話中的憑證。** 範例及測試只用空值或合成資料；實際憑證由使用者在部署主機設定。
4. **先盤點，再修改。** 不覆寫未提交修改，不刪除舊資料，不強制更名既有 binary／CLI／環境變數。
5. **交易真相在後端。** 前端不能自行決定下單數量、價格、交易所、成功狀態或可用資金，也不能持有交易所憑證。
6. **區分三種完成度：** `IMPLEMENTED`、`LOCAL_VERIFIED`、`TESTNET_VERIFIED`。缺憑證／外部網路／實際部署資訊時標 `BLOCKED` 或 `NOT_RUN`，繼續可做的本機開發與測試。
7. **不自動執行外部副作用。** 開發中的測試、server 啟動與開頁面不會下單。外部下單驗證須另外有使用者授權及明確測試開關；部署範例不得自動修改實際 Nginx／防火牆。
8. 每階段執行相關測試與兩家交易所回歸測試，更新 `IMPLEMENTATION_STATUS.md`，最後交付繁體中文 `CODEX_HANDOFF_V3.md`。

優先順序：安全與資料完整性 > 已核對的官方協定／實際回應 > 本文件已確認需求 > 舊提案。官方行為與文件不同時保存去識別化證據，明列阻擋原因，不偷偷改成另一種交易。

---

## 1. 已確認需求與範圍

### 1.1 決策清單

| ID | 已確認內容 | 實作要求 |
|---|---|---|
| R01 | 程式自行讀取 `.env` | 不再要求使用者先 `source .env`；支援指定檔案路徑 |
| R02 | Web 介面切換 Bybit／Deribit | 選擇屬於每個瀏覽器，不得修改全伺服器的「目前交易所」 |
| R03 | 首頁顯示 account balance 與資產 | 大型總額區、資產列表、可用餘額、資料時間與連線狀態 |
| R04 | 資產估值隨行情更新 | 初始查詢 + 私人 WS + 公開價格 WS + 後端估值 + Browser WS |
| R05 | 同一頁的快速交易 Modal | 輸入 → 檢視 → 明確確認 → 等待 → 結果；不跳到另一個交易頁 |
| R06 | 成交後自動更新 | Modal 的餘額區與遮罩下的首頁使用同一份帳戶 store，同步更新 |
| R07 | Deribit BTC ↔ USDC | `BTC_USDC` Spot，一張商品的 Buy／Sell 兩個方向；metadata、tick、amount step、minimum amount、contract size fallback 與 fee currency 只能取自該商品 API |
| R08 | Bybit BTC／ETH ↔ USDT | `BTCUSDT` 與 `ETHUSDT` Spot，各自提供 Buy／Sell 兩個方向 |
| R09 | 限價 IOC + 0.5% 價格保護 | 不改成裸市價單；不承諾全額成交；0.5% 不含手續費 |
| R10 | 簡單登入 | 單一固定帳號／密碼從 `.env` 或 OS environment 讀取；無使用者資料庫、註冊與重設密碼流程 |
| R11 | 面試展示可連入 | 透過既有 Nginx HTTPS，不直接公開 Go port |
| R12 | Nginx 和 Go 不同機器 | Nginx → Go 私有網路 HTTP／WS；Go 綁設定的內網位址，只接受可信 proxy |
| R13 | SSL 已由 Nginx 處理 | 不新增 Go TLS 憑證管理，不要求 `TLS_CERT_FILE` 或 `TLS_KEY_FILE` |

### 1.2 技術方向

新前端使用 **Vue 3 + TypeScript + Vite + Ant Design Vue**；如現有前端已採相容架構，直接擴充。Go 繼續承擔 API、驗證、交易與推播，不新增 Node 後端。Node 僅作前端建置工具。依專案現況鎖定相依版本，不使用未鎖定的 `latest` 作為交付基準。

UI 以英文標籤供面試展示，例如 `Account Balance`、`Quick Trade`、`Review Trade`、`Confirm Trade`；操作／部署／交接文件使用繁體中文。

### 1.3 本版不做

第三家交易所、自動套利／策略、跨所補單、多跳換幣、提款／轉帳／建立子帳戶、槓桿／永續合約 Web 下單、全套交易圖表、訂單簿視覺化、註冊／OAuth／RBAC、多租戶、Go 原生 TLS、重新實作 FIX。

保留舊 CLI 的合約／FIX 功能，但 Web 不暴露任意交易所原生下單代理。指定 Spot 商品不可用時必須 fail closed，不得偷偷跨商品或跨資產繞單。

---

## 2. Phase 0：基線盤點與相容性

建立 `docs/v3/BASELINE_AUDIT.md`，記錄 commit、工作樹、Go／前端版本及下列實況：

| 項目 | 必須核對 |
|---|---|
| 入口 | 真正的 main package、CLI 名稱、既有 web／dashboard 子命令 |
| 設定 | 現有 env 名稱、是否已讀 dotenv、預設 account／category、密鑰路徑語意 |
| Bybit | Spot REST、public spot WS、wallet、order／execution、成交費用與 balance 查詢 |
| Deribit | Spot 商品是否已支援；原 V2 的反向永續單位不可直接沿用 |
| 帳戶 | 實際帳戶 alias／account type；不得假設要轉入 subaccount；記錄 API 讀到的帳戶識別 |
| Web | 現有路由、驗證、前端框架、資產靜態服務、推播與 CORS |
| 儲存 | 訂單意圖、去重／費用、schema、並行寫入、啟動恢復 |
| 測試 | 現有 unit／integration／race／build 結果及已存在失敗 |

基線測試不下單、不取消訂單、不改帳戶設定。建議執行（依實際結構調整）：

```bash
git status --short
go list ./...
go test ./...
go vet ./...
go test -race ./...
```

在既有回歸保護外，V3 必須確保：

- 只有 Bybit 設定時，Bybit 舊命令仍可執行；只有 Deribit 時亦同。
- CLI 非 Web 命令不因缺 `WEB_PASSWORD` 等 Web 設定而失敗。
- 舊資料不能因新 `venue`／`account` 欄位被當成空帳戶；遷移需版本、備份及回復程序。
- 已修正的費用扣除保留：**成交量 ≠ 扣費後入帳量**。
- 不新增另一套 Web 專用訂單 reducer／reconciler，導致 CLI 與 Web 對同張單有不同結果。

---

## 3. 目標架構與責任邊界

```text
Browser (Vue)
    | HTTPS / WSS，單一公開 origin
    v
既有 Nginx：TLS termination（主機 A）
    | HTTP / WS，受控私有網路
    v
Go Console（主機 B，指定 private IP:port）
    |
    +-- Auth / Sessions / CSRF / Trusted Proxy
    +-- Account Query + Account State
    +-- Pricing + Valuation
    +-- Quote + Quick Trade Application Service
    +-- 既有 Intent / Order Tracker / Reconciler / Store
    +-- Browser WS Hub
    |
    +-- Bybit Adapter：REST + public/private WS
    +-- Deribit Adapter：HTTP JSON-RPC + public/private WS
```

### 必須共用與必須隔離

**共用：** domain、帳戶觀測、訂單狀態、成交去重、費用模型、恢復、持久化與風險檢查。  
**隔離：** venue／environment／account／product type／instrument 的 metadata、限流、健康、訂閱、重連及報價快取。

建議能力邊界：`AccountSnapshotProvider`、`AccountEventSource`、`PriceProvider`、`SpotTradeCapacityProvider`、`SpotQuoteService`、`QuickTradeService`、`OrderTracker`。不要為符合名字搬動整個專案。

Web handler 呼叫 application service，**不得透過 shell 執行 CLI 下單**。Browser 不直接連交易所，不接收 API key、secret、登入 token 或 FIX 憑證。

所有交易所連線由後端集中維護；新增一個面試者分頁，不得新增一整組 exchange WS 或每人各自 REST 輪詢。

---

## 4. `.env` 載入與設定契約

### 4.1 優先順序

```text
既有明確 CLI flags（保留既有語意；不新增 secret flags）
    > OS environment
    > 選定的 dotenv 檔
    > 非敏感預設值
```

- 預設僅讀工作目錄的 `.env`。新增 `--env-file /absolute/path/config.env`；旗標位置以現有 CLI 為準並更新文件。
- 支援使用 `--env-file .ENV` 明確選擇大寫檔名；Linux 不應假設 `.env` 與 `.ENV` 相同。
- 顯式指定的檔案不存在／不可讀／格式錯誤：啟動失敗。預設 `.env` 不存在：允許只使用 OS env，但必要設定仍須驗證。
- OS 中「已存在但為空」仍優先於檔案值，再由必要欄位驗證拒絕；不可用空值偷偷觸發舊密碼 fallback。
- 使用 dotenv parser，不用 `sh -c`／`source`／`eval`。不執行命令替換，不遞迴搜尋上層 `.env`，不在 package import 自動載入。
- 可沿用現有 loader；新增時可使用 `godotenv.Load` 或 `Read` 後明確合併，禁止 `Overload` 蓋掉 OS env。[C1]
- 記錄引號、`#`、空白、`$`、反斜線及換行處理，密碼特殊字元使用 parser 支援的字面值引用，寫測試確認。dotenv 不是完整 shell script。
- `.env` 僅後端讀取；不把 repo root 設為前端 public dir，不把 secret 放 `VITE_*`，不把完整 environment／config dump 到日誌。
- 設定於啟動時讀取，V3 不做 hot reload。改密碼／session secret 後重新啟動；未完成交易意圖必須能恢復。

### 4.2 Web 與策略設定

以下為本版建議的正式名稱。現有同義設定若已存在，可保留 alias，但只能有一個有效來源且須記錄映射。

| 設定 | 預設／要求 | 說明 |
|---|---|---|
| `WEB_HOST` | 部署時必填 | Go 主機的 private interface IP；不能填 Nginx IP |
| `WEB_PORT` | `8080` | 不公開給 Internet |
| `WEB_PUBLIC_ORIGIN` | 必填 HTTPS origin | 例如 `https://trade.example.com`；無路徑、userinfo、query |
| `WEB_TRUSTED_PROXY_CIDRS` | 必填 | 實際 Nginx 到 Go 的來源 IP，如 `10.0.0.10/32`；不可信任全網段作方便解法 |
| `WEB_USERNAME` | 必填 | 單一固定登入名稱 |
| `WEB_PASSWORD` | 必填，至少 12 字元 | 範例留空；不允許預設弱密碼 |
| `WEB_SESSION_SECRET` | 必填 | 以 Base64 表示至少 32 個隨機 bytes；驗證解碼後長度 |
| `WEB_SESSION_TTL` | `8h` | 絕對到期，不因 WS 心跳無限展延 |
| `WEB_DEFAULT_VENUE` | `bybit` | 必須是啟用的交易所；不可因離線偷偷跳到另一家送單 |
| `WEB_TRADING_ENABLED` | `false` | 全站唯讀開關；使用者明確設 `true` 才啟用 Web 確認送單 |
| `WEB_PUSH_INTERVAL` | `250ms` | 行情估值合併推播頻率；不是延遲交易狀態的理由 |
| `ACCOUNT_RECONCILE_INTERVAL` | `30s` | 每 venue／account 單一背景快照排程，含 jitter／限流 |
| `QUOTE_TTL` | `5s` | 檢視報價有效期；到期需重新檢視及確認 |
| `TRADE_BOOK_MAX_AGE` | `3s` | 報價與送單的可執行 book 最大年齡；有受控 HTTP 重抓機制 |
| `VALUATION_PRICE_MAX_AGE` | `15s` | 價格逾期停止顯示 live；保留最後值與標記 |
| `QUICK_TRADE_SLIPPAGE_BPS` | `50` | 本版固定上限 50 bps = 0.5%；UI 不提供提高上限 |
| `QUICK_TRADE_MAX_BTC` | `0.01` | 輸入資產 BTC 的單次支出上限，本專案政策 |
| `QUICK_TRADE_MAX_ETH` | `1` | 輸入資產 ETH 的單次支出上限，本專案政策 |
| `QUICK_TRADE_MAX_USDT` | `1000` | 輸入資產 USDT 的單次支出上限，本專案政策 |

上述時間及金額是**專案預設，不是交易所限制、當前市場數據或已測延遲**。送單同時受交易所 metadata、可用餘額、手續費與既有更嚴格限制約束。不以「沒設定」表示無限制。

`WEB_TRADING_ENABLED` 只控制新增 Web 寫入，不悄悄更改舊 CLI 規則。單一共用帳號無法區分擁有人及面試者；開啟後，所有登入者都能做允許的 Testnet 交易。僅展示時設為 `false`。本版不新增第二組帳密或角色系統。

### 4.3 範例

```dotenv
# 以下 IP / 網域只是文件範例，需換成實際部署值。
WEB_HOST=10.0.0.20
WEB_PORT=8080
WEB_PUBLIC_ORIGIN=https://trade.example.com
WEB_TRUSTED_PROXY_CIDRS=10.0.0.10/32
WEB_USERNAME=alex
WEB_PASSWORD=
WEB_SESSION_SECRET=
WEB_SESSION_TTL=8h
WEB_DEFAULT_VENUE=bybit
WEB_TRADING_ENABLED=false

WEB_PUSH_INTERVAL=250ms
ACCOUNT_RECONCILE_INTERVAL=30s
QUOTE_TTL=5s
TRADE_BOOK_MAX_AGE=3s
VALUATION_PRICE_MAX_AGE=15s
QUICK_TRADE_SLIPPAGE_BPS=50
QUICK_TRADE_MAX_BTC=0.01
QUICK_TRADE_MAX_ETH=1
QUICK_TRADE_MAX_USDT=1000
QUICK_TRADE_MAX_USDC=1000

# V3.1：全站共用每小時／並行額度；各交易所來源資產支出上限。
DEMO_MAX_TRADES_PER_SESSION=10
DEMO_MAX_TRADES_PER_HOUR=30
DEMO_MAX_CONCURRENT_TRADES=1
DEMO_MAX_BYBIT_BTC_QTY=0.01
DEMO_MAX_BYBIT_ETH_QTY=1
DEMO_MAX_BYBIT_USDT_AMOUNT=1000
DEMO_MAX_DERIBIT_BTC_AMOUNT=0.01
DEMO_MAX_DERIBIT_USDC_AMOUNT=1000

# 交易所設定沿用既有程式；不要直接覆蓋使用者原檔。
BYBIT_ENV=testnet
BYBIT_API_KEY=
BYBIT_API_SECRET=
BYBIT_WS_SPOT_URL=wss://stream-testnet.bybit.com/v5/public/spot
DERIBIT_ENABLED=true
DERIBIT_ENV=testnet
DERIBIT_API_KEY=
DERIBIT_API_SECRET=
```

`.env.example` 只留空 secret。實際 `.env`、session 檔、state、金鑰與原始私人 response 均不進 Git。部署憑證檔只允許服務帳號讀取。

---

## 5. 登入、Session 與 API 存取

### 5.1 介面

未登入時只顯示登入頁與公開靜態資源，不能取得 balance、交易所帳戶 alias、訂單、行情串流或內部健康細節。

```text
Trading Console
Username   [________________]
Password   [________________]
           [ Sign In ]
```

端點：

- `POST /api/auth/login`：JSON 帳密，成功後 Set-Cookie；回傳使用者顯示名稱、CSRF token、到期時間與唯讀狀態，不回傳密碼。
- `GET /api/auth/me`：已登入狀態；可重新取得 session 綁定的 CSRF token。
- `POST /api/auth/logout`：驗證 CSRF，立即撤銷 session、清 cookie、關閉該 session 的 Browser WS。

### 5.2 Session 設計

使用現有可靠 session middleware，或採標準密碼學元件實作有界的 server-side session store。建議隨機 opaque ID + 簽章，使用 `WEB_SESSION_SECRET` 作簽章鍵；不自行發明加密演算法。

- cookie 名稱：`__Host-trading_session`；`Secure=true`、`HttpOnly=true`、`SameSite=Strict`、`Path=/`，不設 Domain。[S1]
- 固定 8h 絕對到期。伺服器重啟可使 session 失效，但交易意圖／追蹤不能跟著消失。
- 登入後重新產生 session ID，避免 session fixation；登出與到期在後端立即生效。
- 不使用 localStorage／URL query 保存 password、session bearer token 或 exchange token。
- `.env` 中密碼依使用者要求讀取；比較採成熟 password verifier，或對固定長度衍生值做 constant-time comparison；不記錄候選密碼，不把一般 SHA-256 宣稱為可用於持久化密碼儲存的慢雜湊。
- 帳號不存在與密碼錯誤統一回覆，限制 request body 大小，記錄去敏感資訊的登入失敗。

登入防爆破預設：每個可信 client IP 在 5 分鐘內失敗 5 次，冷卻 30 秒，回 `429` + `Retry-After`。增加全域有界 limiter 防止分散流量耗盡資源，但不永久鎖死共用帳號。

### 5.3 CSRF、Origin、授權

- 所有需要登入的 HTTP／WS 端點在後端驗證 session，不只靠 Vue router。
- 所有寫入請求（含 quote 建立、confirm、refresh、logout）檢查**完全相同的** `Origin` 與 CSRF token；不做 suffix／contains 網域比對。[S2]
- login 本身檢查 Origin，限定 JSON 與正確 Content-Type，防止 login CSRF；無 Origin 的 Browser API 寫入預設拒絕。
- CSRF token 與 session 綁定，透過 `X-CSRF-Token` 提交。`SameSite` 只作額外防護，不是唯一機制。
- 不開跨來源 CORS；所有 Browser API 及 WS 走同一公開 origin。
- WS handshake 驗證 session、Origin、Host、trusted proxy；不把 token 放 WS URL。[S3]
- quote／intent 必須屬於登入 identity 且位於允許帳戶範圍；不能靠改 URL 任意操作別家／其他帳戶。
- `WEB_TRADING_ENABLED=false` 的限制在後端執行，偽造前端按鈕也不能送單。
- Nginx ACL 不取代應用驗證；即使來自 trusted proxy，未登入仍不能查資產或交易。

### 5.4 回應與靜態資源

私人 API 使用 `Cache-Control: no-store`。禁止公開 `.env`／`.git`／PEM／state／logs／source map 中的敏感資訊；API 404 不可回傳 SPA index 假裝成功。前端與字型／圖示盡量本機打包，不依賴外部 CDN。

設定可運作的 CSP、`frame-ancestors 'none'`、`X-Content-Type-Options: nosniff` 等防護，並測試不破壞 Ant Design Vue 的樣式。不可用關閉全部 CSP 的方式修 UI。

---

## 6. 首頁：Account Balance 與交易所切換

### 6.1 視覺與配置

參考使用者資產截圖的資訊層級：深色背景、大型總額卡片、幣別表格、數值右對齊。**不複製截圖餘額當真實資料，不顯示虛構 APR，不加入 Deposit／Withdraw。**

頂列包含產品名稱、`Bybit / Deribit` selector、`TESTNET` 標記、帳戶 alias、公開行情／私人事件狀態、重新整理、登出。主要按鈕為 `Quick Trade`。

表格至少包含：

| 欄位 | 語意 |
|---|---|
| Asset | 幣名／代碼；無 icon 時顯示字母圖示 |
| Balance | 交易所回報的資產數量／cash balance |
| Equity | 交易所權益；可能包含損益，不能一律當成可交易數量 |
| Value (USD) | 該列估值、來源與 freshness；未能定價顯示 `—` |
| Available to Trade | 本程式對指定 Spot 操作驗證的可用值；未知顯示 `—` |
| 狀態／操作 | Live／Snapshot／Stale／Unpriced，符合允許方向才提供 Quick Trade |

可提領數量若已有可靠欄位，可作額外資訊顯示，**不可為了仿照截圖把 available-to-trade 改名成 Withdrawable**。

保留未知或不支援的非零資產與負債。預設隱藏零餘額可提供 toggle，但負數、借幣及異常項目不可被「只顯示資產」誤藏。

### 6.2 切換規則

- 每個瀏覽器獨立保存 venue；localStorage 只可保存 UI 偏好，不存帳密或財務快照。
- 切換時取消舊畫面的 HTTP 查詢或使用 request generation；晚到的 Bybit 回應不能覆蓋 Deribit 畫面。
- quote 與 Modal 綁定原 venue／account。EDIT／REVIEW 中切換先關閉／清除 quote；SUBMITTING 之後不允許改變該 Modal 的交易所。
- 後端追蹤不隨 UI 切換停止。允許收起進行中 Modal，但首頁保留交易通知入口，可回看。
- 一家失效只使該家顯示 unavailable／stale；保留最後資料及時間，不能當成零資產，也不拖垮另一家。

### 6.3 資料型別

API 中所有價格、數量、費用與金額使用**十進位字串**，timestamp 使用 UTC RFC3339 或明確命名的毫秒欄位。未知值使用 `null` 加原因，不用 `0` 代替。Browser 僅格式化，不用 JavaScript `Number` 做下單或會計計算。

---

## 7. 帳戶同步與可用資金

### 7.1 初始快照與 WS 銜接

後端啟動該 venue／account 後：

1. 驗證環境、帳戶身分及讀取 scope。
2. 建立私人 WS 並訂閱；先緩衝事件，記錄 connection generation。
3. 抓 account snapshot、必要訂單／成交與可用資金資料。
4. 依交易所時間／revision／語意合併快照與緩衝；不是先 REST 後訂閱而留下空窗。
5. 對無法排序或已衝突的資料重查；完成必要恢復後進入 Ready。

私人訊息不得無聲丟棄。有界 buffer 超限、斷線或缺口時標記 Recovering／Degraded，停止新增 Web 交易，重新核對。UI 仍可呈現最後快照與其年齡。

定期快照、使用者 refresh、成交後刷新與 reconnect reconciliation 應合併排程，**每個 account 同時最多一個 recovery／refresh**，不可每個視窗各打交易所 API。

### 7.2 Bybit

- 初始資產使用 `GET /v5/account/wallet-balance`，account type 依既有帳戶模式；V3 預期 UNIFIED。Funding wallet 不在這個預設總額內，要在 UI 標出 account scope。[B1]
- 訂閱私人 `wallet`；現有 `order`／`execution` 訂閱若已涵蓋 Spot 就沿用，否則加入 Spot 類別，不重複訂閱造成重複計費／資料處理。
- **wallet 訂閱沒有初始 snapshot，而且 unrealised PnL 變動不會觸發 wallet event**。不能只靠該 topic 達到即時 USD 估值。[B2]
- balance／equity／locked／borrow 等原始語意分開；不可使用已棄用的欄位或把空字串 parse 成真實零值。
- 檢視／確認送單時使用現行 Spot capacity 查詢。`/v5/order/spot-borrow-check` 的 `spotMaxTradeQty`／`spotMaxTradeAmount` 排除可借額；不可拿 `maxTradeQty`／`maxTradeAmount` 的含借款額當成現貨持有資金。[B6]
- 送單明確 `category=spot`、`isLeverage=0`。資金在 Funding 而不在交易帳戶時，清楚提示 scope，不自動轉帳。
- 明確區分完整快照與部分 coin 更新：部分訊息缺少 ETH，不表示 ETH 歸零；某個完整非零列表省略已知資產時，按已驗證端點語意處理或針對該幣補查。

### 7.3 Deribit

- 帳戶來源優先 `private/get_account_summaries`，`extended=true` 可用於啟動診斷；解析 `result.summaries`，不是把物件當成陣列，也不是用 `get_positions` 查資產。[D1]
- 回傳帳戶識別與 UI alias 對照；無需建立或轉入 subaccount。只顯示 key 實際連到的帳戶，不能自動用主帳戶權限彙總其他帳戶。
- `user.portfolio.any` 或已確認支援的逐幣 portfolio 訂閱提供私人帳戶更新。動態新增資產需更新訂閱／估值 coverage；沒有事件支援的幣仍保留受控快照。[D2]
- Spot 訂單／成交沿用或擴充可覆蓋 Spot 的 `user.orders`／`user.trades`／`user.changes` 訂閱；以現行文件及實際 subscription ACK 核對，不照抄文檔 placeholder 名稱。[D8]
- `available_funds` 是保證金可用資訊，**不自動等於該幣可支出的現貨餘額**。cross-collateral 下某些聚合值以多種幣別重複表示，不能逐列再加總。[D1][D2]
- `availableToTrade` 使用已驗證的 Spot 規則，納入現有餘額、spot reserve、locked amounts、本程式 reservation 及帳戶限制；沒有可靠公式或 precheck 時阻擋送單並回明確原因，不能猜測可借用全部 collateral。
- V3 不因為畫面只是 Spot 而假設帳戶沒有舊合約部位。需要保留原始 account margin model、負債、margin usage 及不支援的風險資訊供檢查。

### 7.4 餘額不是由交易回報直接永久相加

成交可產生預期資產增減，供核對與結果展示；權威帳戶 store 仍以交易所 wallet／portfolio／snapshot 更新。不可先按 fill 扣一次，再收到 wallet 變化又扣一次。

下單後以「訂單終態」「費用／成交明細完整」「餘額同步」三個維度追蹤，不以 HTTP success 或一個較晚的本機接收時間，就認定餘額已反映成交。

---

## 8. USD 估值：快，但不假裝等於交易所完整權益

### 8.1 兩種數字必須分開

1. **Exchange Equity (USD)**：交易所回報的 account equity，附 `asOf`，不擅自重算為另一個意義。
2. **Estimated Asset Value (USD)**：本地資產數量 × 已驗證價格的即時估值；標示 `Estimated / ≈`，不是清算／保證金引擎。

若交易所（例如 Deribit 多幣別 account summaries）沒有回報單一 aggregate USD total，UI 必須保留 Exchange-reported total 欄位並明示 `Not reported by venue`；不得隱藏資料缺口，也不得以本地估值或逐幣別換算冒充交易所總額。

純 Spot、無負債且 coverage 完整時，可讓主要卡片顯示 `Total Account Value — Estimated` 並隨行情更新，旁邊保留交易所快照。存在合約、options、借款或不完整定價時，主標題改為「已定價資產估值／小計」，不能冒稱全帳戶總額。

**禁止以 `equity(snapshot) × 最新幣價` 宣稱精確重建 derivatives equity；禁止重複加入已包含在 equity 內的 UPL；禁止加總每一幣別重複回報的 Deribit cross-collateral total。** [B1][D1]

### 8.2 價格來源

每個值保存 `asset`、`price`、`quoteCurrency`、`sourceVenue`、`sourceKind`、`sourceInstrument`、`exchangeAsOf`、`receivedAt`、`quality`。

估值順序：

1. 已驗證的同環境官方 USD index／資產價格串流。
2. 同環境可用 Spot book mid，再經有來源與時間的 FX 轉換。
3. 缺乏即時路徑時，保留交易所提供的 USD 快照值，標 `Snapshot`；不偽裝 live。
4. 仍無法定價則顯示 `Unpriced`，保留資產列，總額改成 Partial／priced subtotal。

`BTCUSDT` 是 USDT 報價，**不能無條件等同 BTC/USD**；USDT、USDC、USDe 等也不能硬填 1 USD。多跳只限估值（最多兩段、來源可追溯），不是新增多跳交易。不同 environment／venue 的資料不能無標記混用。

Bybit 公開 Spot 使用對應 endpoint 的 `tickers.{symbol}`／orderbook；Deribit 使用經確認的 ticker／index 訂閱及 metadata。不把 linear 的成交價格當成 Spot 可成交價格。[B7][D3]

### 8.3 即時處理與過期

- qty 改變或 price 改變都觸發重估；沒有 wallet event 時，持幣估值仍隨有效價格更新。
- 估值推播以 250ms 合併最後狀態，避免每個 tick 引發全表重繪；成交終態與健康變化優先處理。
- 每個資產獨立 freshness。過期時保留最後值與時間，不持續播動畫；對 unknown 不填零。
- Browser 的 `Live` 表示後端資料品質符合規則，不能只看瀏覽器 WS 是否連著。
- 比較本地估值與交易所值之前，先確認 scope、估值基準與時間一致；差異不一定是資金遺失。

V3 不要求所有截圖中的代幣都有即時 feed；要求每筆都可見、每個值語意正確、主要交易幣在有有效行情時能即時變動。

---

## 9. Spot 快速交易：固定方向與原生映射

| Route ID | UI 方向 | 原生商品 | 操作 | 下單量單位 |
|---|---|---|---|---|
| `deribit-usdc-btc` | USDC → BTC | `BTC_USDC`，Spot | `private/buy` | BTC（base），由 USDC 支出上限反算 |
| `deribit-btc-usdc` | BTC → USDC | `BTC_USDC`，Spot | `private/sell` | BTC（base） |
| `bybit-usdt-btc` | USDT → BTC | `BTCUSDT`，Spot | Buy | BTC（base），由 USDT 支出上限反算 |
| `bybit-btc-usdt` | BTC → USDT | `BTCUSDT`，Spot | Sell | BTC（base） |
| `bybit-usdt-eth` | USDT → ETH | `ETHUSDT`，Spot | Buy | ETH（base），由 USDT 支出上限反算 |
| `bybit-eth-usdt` | ETH → USDT | `ETHUSDT`，Spot | Sell | ETH（base） |

每個方向都必須映射同一張真實交易所商品的買／賣，不捏造反向 symbol，也不合成缺少的 order-book side。[B3][D3][D4][D7]

### 9.1 Metadata gate

啟動與合理 TTL 後查詢商品：kind/category、base／quote、active state、價格 tick、數量精度／step、最小成交額、最大限價量、費率／規則來源及現行交易限制。[B4][D3]

V3 必須新增 **Deribit Spot capability**，不能沿用 `BTC-PERPETUAL` 的 USD 面額規則。保留 V2 合約支援；允許清單以 `(venue, productType, instrument)` 為 key。

Deribit Spot 的實際匹配場所可能由 metadata 標示 `is_cbe_routed`／`is_csr`；這不等於專案新增 Coinbase adapter。依回傳旗標驗證所需訂單／回報語意，不假定所有 Spot 同步回傳 fills，也不要求使用者新增 Coinbase 憑證。[D5]

Deribit `public/get_instrument` 未在 Spot metadata 另給 pre-trade `fee_currency`；Review 的 fee estimate 必須由該次 API 回傳的 `quote_currency` 與 `taker_commission` 建立，不得沿用其他商品的幣別。成交後以 `private/get_user_trades_by_order` 每筆 execution 的 `fee_currency`／`fee` 為權威值並覆蓋 estimate；缺失或無法映射時 fail closed。

商品不存在、停牌、metadata 不明、IOC 不支援、無有效流動性或缺 scope 時，前端列出 route 但 disabled 並說明原因。不得為了展示成功而改商品／放大金額／換環境／改成 GTC。

### 9.2 輸入語意

使用者輸入的是 **Spend up to X 個 From asset**，包含可能從 From asset 扣除的費用。不是「保證花完」也不是「指定淨買入量」。

表單顯示 From／To、可用數量、Amount、參考匯率、預估收到、費用狀態、價格保護。可提供 25%／50%／Max，但 Max 必須先受單筆上限、fee reserve、精度與現貨能力限制；不得直接把全部 equity 填進去。

只允許正數、有限長度十進位字串，不接受 `NaN`、Infinity、負數、0、科學記號或超長輸入。最終數量若小於 metadata 下限，回可理解錯誤。

---

## 10. 報價、價格保護、數量與費用

### 10.1 Quote 不是 executable guarantee

Quote 完全由後端計算並保存，包含 identity、venue、environment、account、route、原生商品／方向、支出上限、實際 base quantity、限價、TIF、metadata revision、book observation、balance version、fee estimate、有效期。

Browser 僅送 route 與輸入金額。Confirm 僅送 `quoteId` 和 `clientRequestId`；不能接受 Browser 自改 price／side／account／fee／slippage 當成真實交易參數。

報價使用最新有效的**可執行 bid／ask 與深度**；不使用 last trade、index 或估值價格直接當成買賣價格。深度不足可以預告 partial，但不能估出超出受保護範圍的可成交數量。

### 10.2 價格公式

以下 `s=0.005`；`floorTick`／`ceilTick` 是依該價位有效 tick ladder 求合法價格：

```text
Buy:
    limitPrice = floorTick(bestAsk × (1 + s))

Sell:
    limitPrice = ceilTick(bestBid × (1 - s))
```

**買價向下取合法 tick，賣價向上取合法 tick**，避免 rounding 使價格超出使用者的 0.5% 邊界。若合法 tick 下沒有可 marketable 的價格，拒絕此次 quote，不再加一 tick 放寬。

限價與最差價格必須在 Review 顯示且凍結。Confirm 時重驗資料與限制，**不得依新 best ask／bid 重算更差的限價**。quote 過期或必須改變數量、價格／費用上限時，回到 Review，重新明確確認。

0.5% 是相對該方向 quote 時 best ask／bid 的成交價格保護，**不含 bid-ask spread、交易手續費、匯率變動或跨所風控價差，也不保證全額成交**。

### 10.3 支出上限與 quantity

全部以 decimal 計算。實際下單量依 quantity step 向下取整，不把輸入 quote currency 當成 base qty。

```text
Buy（From = quote asset）：
    找最大的合法 baseQty，使
    baseQty × buyLimitPrice + maximumSourceAssetFee(baseQty) <= SpendBudget

Sell（From = base asset）：
    找最大的合法 baseQty，使
    baseQty + maximumSourceAssetFee(baseQty) <= SpendBudget
```

同時驗證無借款的可用資金、本程式已保留資金、交易所容量、原生最大量與最小成交額。費用從 To asset 扣除時，減少預估淨收款；從第三種幣扣除時，要檢查該幣容量，並在 Review 獨立列出。

**假設數值範例，非市場行情／費率承諾：** Ask = 100,000 USDT/BTC、budget = 1,000 USDT、tick = 0.1、qty step = 0.00001、費用從收到的 BTC 扣除。限價 100,500，baseQty = 0.00995，最差 gross spend = 999.975 USDT。全額成交仍可能剩餘預算，不能顯示「已花 1,000」或自動追加補單。

### 10.4 Fee policy

- 費率優先用帳戶／商品費率 API 及已驗證費用模型，不硬填 0%、0.1% 或先前 Testnet 的 1%。[B8]
- 費率、費用幣別與來源記入 quote。若無法確認足以限制支出的費用模型，允許顯示預覽但禁止 confirm，回 `FEE_MODEL_UNAVAILABLE`；不能把未知費用顯示成 0。
- 對可能額外從來源／第三幣扣除的費用，必須有可驗證的預留上限。只顯示估計值不能取代支出保護。
- 交易後以逐筆 execution 的實際 fee／currency 為準；聚合可能含多種幣、折扣、rebate 及 extra fees。不能將不同幣的費用數量直接相加。[B5]
- UI 分別顯示 gross fill、gross paid／received、fees by asset、net received、actual source debit。
- 同一成交從 REST／RPC response、WS、reconciliation 重複抵達，只入帳一次。訂單累計量不能與逐筆 fills 再相加。
- 只有 order Filled、尚缺 fills／fees 時，顯示 `Filled — syncing trade details`，不捏造 net receive。

### 10.5 原生下單設定

Bybit：`category=spot`、`orderType=Limit`、`timeInForce=IOC`、`isLeverage=0`、`orderFilter=Order`，`qty` 為 base units；`marketUnit` 是 Spot market order 相關選項，不靠它把本版限價單變成 quote 金額單。[B3]

Deribit：`type=limit`、`time_in_force=immediate_or_cancel`、`post_only=false`，使用經 Spot metadata 驗證的 base amount、quote/base price；不傳入反向合約的 USD amount。[D4]

交易所風控拒單時顯示去敏感資訊的錯誤碼／原因。**不放寬 0.5%、不改裸市價、不換另一條 route、不拆成兩腿、不自動重送。**

---

## 11. Modal、確認與交易狀態機

### 11.1 視覺流程

```text
EDIT → REVIEW → SUBMITTING → WAITING / RECOVERING → RESULT
```

**EDIT：** venue、direction、source balance、spend amount、估計匯率、Review 按鈕。  
**REVIEW：** 固定 quote 的支出上限、baseQty、預估 gross/net receive、手續費、限價／0.5% 保護、IOC 部分成交提示、quote 到期倒數；Back／Confirm。  
**SUBMITTING：** 禁用二次送單；不能在背景不知情重試。  
**WAITING：** 顯示已保存意圖、已送出／已受理、已知成交量、等待狀態；不用動畫冒充已收到回報。  
**RESULT：** 依真實終態顯示成交、部分成交、未成交取消、拒單或結果仍不明。

按 Enter、雙擊、重整、關閉 Modal 都不得產生第二張新單。關閉 Modal 不表示取消交易；重新打開顯示原 intent 狀態。

### 11.2 業務狀態分開

沿用 V2 的 `ExchangeState` 與 `CommandState`，另加 Web 結果視圖，避免把每個 transient UI 狀態寫成交易所狀態。

| Web 結果 | 判定 | 介面說明 |
|---|---|---|
| `FILLED` | 原生下單量全額成交 | Trade filled；仍可能有 rounding／price improvement 剩餘預算 |
| `PARTIAL_CANCELLED` | 0 < filled < submitted qty，剩餘已取消 | Partially filled；列出實際換到多少與未花費部分 |
| `CANCELLED_NO_FILL` | 終態取消且沒有成交 | No fill within protection limit；不是成功換幣 |
| `REJECTED` | 交易所明確拒單／確定未送出的驗證失敗 | 顯示原因，資料不偽更新 |
| `OUTCOME_UNKNOWN` | 逾時／斷線／crash 後不能證明結果 | Checking order status；不可當失敗釋放再買 |

顯示 UI 等待逾時（建議 15s）只改成「比預期久，正在核對」，不取消／重送。後端有限速查詢及私人事件追蹤繼續；仍無法確定顯示 NeedsReview，保留 reservation 與阻擋。

Deribit RPC 可能直接帶 `order`／`trades`，應即刻合併；Bybit ACK 不代表終態。**不用為了流程動畫強迫已成交單繼續假等，也不把未有 fills 的 ACK 當成交。** [B3][D4][D5]

### 11.3 結果顯示

結果至少列 venue、route、IntentID、原生 OrderID、submitted base qty、actual gross fills、weighted average price、actual source debit、net destination receive、逐幣 fee、未使用預算、時間與資料完整性。

`Again` 必須建立新 quote 並重新確認；只在前一意圖結果與資金已釐清後啟用。不得直接重播前一 POST。

### 11.4 Modal 下與背景的餘額自動更新

- 打開 Modal 保存有時間的 before account snapshot 作比較。
- 成交／終態事件觸發後端 account refresh／reconcile，公開估值繼續推送。
- Modal 的 `Current Balance` 與背景首頁訂閱同一個 venue/account store，不各自維護兩份會分歧的餘額。
- 交易已完成但 wallet 尚未反映時：顯示 `Trade filled — updating balance`；保留最後餘額與 syncing 標記，不造一個 after balance。
- 有可靠 snapshot／wallet 更新且未完成差異已核對，才更新為已同步。僅「收到較晚」不保證它已含該筆交易。
- 多個登入者／CLI／交易所網頁同時操作時，Before/After 是兩次帳戶觀測，不將所有差額歸因於這一筆；本筆 actual asset changes 另外由去重 fills 計算。
- 費用尚未完整時可更新已知帳戶餘額，但 net trade result 保持 pending；兩者互不冒充。

---

## 12. 確認送單、冪等與持久化

### 12.1 單一確認交易

Confirm 在 account writer／store transaction 中執行：

1. 驗證登入、CSRF、allowlist、`WEB_TRADING_ENABLED`、quote ownership／expiry。
2. 先查 `clientRequestId` 及 quote 是否已有 intent。相同內容回原 intent；不同內容重用 request ID 回 `409`。
3. 重驗 venue healthy、私人恢復完成、book 品質、metadata、price protection、無借款可用資金、fees 與支出限制。
4. 原子地消耗 quote、保存 immutable intent、建立 reservation 與 request→intent 對應，再允許送出。
5. 在 network send 前持久化「可能已送出」狀態；從此 timeout／crash 不當成確定未送出。
6. 固定 transport 送一次，結果交給既有 tracker／reducer。Browser HTTP request 中斷不取消後端追蹤生命週期。

同一 quote 即使換一個 clientRequestId，也不能再次送出。冪等以登入 identity + request ID，以及 quote 的唯一 consumption 為準，不能只用記憶體 mutex／按鈕 disable／短暫 session ID 達成。

### 12.2 Timeout 與斷線

前端 confirm response 掉失時，保留原 clientRequestId 並查結果；可重送同一 request 取原結果，**不能生成新 ID 重下**。先前 HTTP 202、WS 最終事件及 GET 查詢任意順序到達都要合併到同一 intent。

Bybit `orderLinkId`／Deribit `label` 作 correlation，不作跨所 exactly-once 保證；Deribit label 可以對應多張單。[D6]

結果不明：查原生 OrderID；必要時 label／client ID + 參數、order history、executions 多來源核對。一次查不到不是沒有送成功。不得跨 REST／WS／FIX 或跨交易所 fallback 重送。

### 12.3 Reservation 與多客戶端

每個 account 可用資金先扣本程式 reservations，避免兩個瀏覽器同時花同一份餘額。V3 可保守採「每個 account 一次一個新增 Quick Trade intent」，忙碌時回 `ACCOUNT_BUSY`；不能影響另一交易所。

私人訊息／RPC response／HTTP snapshot 各自不能重複扣 reservation。只有已確定未送出，或終態且資金已核對，才釋放。未知 outcome 不能因 UI timeout 自動釋放。

Web 與 CLI 共用資料時採既有單寫者／跨程序鎖。若現有 store 不能安全多 writer，明確禁止 CLI 同時寫入並提供清楚錯誤，不能用 atomic rename 冒充跨程序交易。

### 12.4 必須保存

quote 的確認副本、IntentID、clientRequestId、quote consumption、venue／env／account／route、native params、send attempt 狀態、NativeOrderID、fills／fees 去重鍵、結果、reservations、recovery cursor、audit references。

未確認 quote 可在記憶體過期；**已確認 intent 必須耐受重啟**。server 重啟使登入失效不影響訂單恢復；重新登入能查回尚未完成／近期意圖。

---

## 13. HTTP API 契約

建議以 `/api` 為同 origin API prefix，若已有版本路徑可沿用並提供映射。下表除 login 外均須登入；所有 POST 同時需要 §5 的防護。

| Method | 路徑 | 用途 |
|---|---|---|
| POST | `/api/auth/login` | 固定帳密登入 |
| GET | `/api/auth/me` | session、CSRF、設定摘要（非秘密） |
| POST | `/api/auth/logout` | 撤銷 session |
| GET | `/api/venues` | 支援的 venue、account alias、健康、唯讀／交易能力 |
| GET | `/api/venues/{venue}/account` | cached normalized account snapshot、估值與品質 |
| POST | `/api/venues/{venue}/account/refresh` | 觸發合併後的受控刷新，不立即重複打交易所 |
| GET | `/api/venues/{venue}/quick-trades` | 固定 route、limits、availability 與原因 |
| POST | `/api/venues/{venue}/quotes` | 以 routeId／amount 建立 Review quote，不下單 |
| POST | `/api/venues/{venue}/trades` | 以 quoteId／clientRequestId 確認一個 intent |
| GET | `/api/venues/{venue}/trades/{intentId}` | 狀態、fills／fees、同步狀態 |
| GET | `/api/venues/{venue}/trades` | 有界近期列表／pending；可用 clientRequestId 查回結果 |
| GET | `/api/ws` | 驗證後升級，接收後端 account／valuation／order updates |

建立 quote 範例（字串金額）：

```json
{
  "routeId": "bybit-usdt-btc",
  "amount": "1000"
}
```

Confirm 範例：

```json
{
  "quoteId": "q_<opaque-id>",
  "clientRequestId": "req_<browser-generated-random-id>"
}
```

新 intent 成功持久化後回 `202` 與 `intentId`／目前已知狀態，不把 `202` 當成交證明。重複 confirm 回原資源，不產生第二次副作用。

錯誤 envelope：

```json
{
  "error": {
    "code": "QUOTE_EXPIRED",
    "message": "Quote expired. Review a fresh quote before confirming.",
    "retryAction": "REQUOTE",
    "requestId": "r_<opaque-id>"
  }
}
```

必需錯誤碼：`AUTH_REQUIRED`、`FORBIDDEN`、`CSRF_INVALID`、`UNTRUSTED_PROXY`、`READ_ONLY`、`INVALID_AMOUNT`、`UNSUPPORTED_ROUTE`、`MARKET_UNAVAILABLE`、`STALE_MARKET_DATA`、`INSUFFICIENT_SPOT_BALANCE`、`TRADE_LIMIT_EXCEEDED`、`FEE_MODEL_UNAVAILABLE`、`QUOTE_EXPIRED`、`QUOTE_CHANGED`、`IDEMPOTENCY_CONFLICT`、`ACCOUNT_BUSY`、`VENUE_RECOVERING`、`OUTCOME_UNKNOWN`。

未知 outcome 應是已存在 intent 的狀態；不得回一個暗示「安全重下」的泛用 500。業務拒單需保留已知訂單結果，HTTP 狀態碼與 exchange 業務碼分開。

---

## 14. Browser WebSocket 與前端 store

### 14.1 資料流

Browser 先以 HTTP 取得 session／venues。登入後建立同 origin WSS，訂閱目前 venue 的帳戶與該 identity 有權看的意圖。為避免初始空窗，WS server 在訂閱處理時，從同一有序狀態來源先推完整 UI snapshot，再推後續更新；HTTP 取得的 cached account 僅作初始顯示，不越過較新的 WS revision。

事件：

```text
snapshot
account.updated
valuation.updated
trade.updated
venue.health.updated
resync.required
session.expiring
```

每則 envelope 包含 `schemaVersion`、`instanceId`、`seq`、`type`、`venue`、`accountAlias`、`stateRevision`、`sentAt`、`payload`。`seq` 依單一 Browser stream 連續遞增；估值 coalescing 在配置 seq 前完成，不能故意造成假的缺號。

斷線／instanceId 改變／sequence 缺口：停止把畫面當成 Live，重新訂閱取得 snapshot 與 pending intents。舊 generation 晚到事件不得覆蓋新資料。

### 14.2 控制與背壓

- Browser WS 僅允許有界 subscribe／unsubscribe／resync 控制；**不接受下單訊息**，下單走有 CSRF 防護的 HTTP。
- 30s 內有應用或 control heartbeat，Nginx timeout 應更長。登入到期／登出會關閉連線，不靠重新整理才生效。[S3][N1]
- 限制每 session 的連線數（預設 5）及全域有界 clients、訊息大小、queue。
- 慢客戶端的行情估值可只保留最新 snapshot；order／account 關鍵事件不能無聲丟棄，無法送出時斷線並要求 resync，不阻塞 exchange event loop。
- 推播只含正規化資料，不能直接轉送 raw auth／private response／敏感帳戶欄位。

### 14.3 前端實作

使用單一 normalized store，key 至少包含 venue／environment／account。首頁、Modal 下方 balance 與近期交易共用它。decimal 原值留在 store，顯示時再格式化，USD 適度取兩位、幣量依 precision 顯示；複製值保留必要精度。

Modal 必須支援鍵盤操作、可辨識 focus、窄螢幕、loading／empty／error／stale；不能只以紅綠色區別結果。提交時可收起但不取消，頂部交易通知可重新打開。長 OrderID 不擠爆版面。

---

## 15. Nginx 與 Go 跨機器部署

### 15.1 實際拓撲

```text
使用者／面試者 Browser
        |
 HTTPS + WSS，公開網域
        |
Nginx 主機 A：例如 10.0.0.10（現有 SSL）
        |
 受控 private LAN / VLAN 或加密私有隧道
        | HTTP + WS
Go 主機 B：例如 10.0.0.20:8080
```

IP／網域皆為範例，實際值由部署設定提供。**Go 不是綁 `127.0.0.1`；Nginx upstream 也不是 `127.0.0.1:8080`。** 不使用 Go 原生 TLS，不搬動現有憑證流程。

跨機器 HTTP 在該段沒有 TLS 加密；僅適用受控可信私網。若穿越不可信網路，必須讓這段走 VPN／加密私有隧道，不能因外面有 SSL 就公開 HTTP port。

### 15.2 網路與 proxy trust

- Go 綁定 `WEB_HOST` 的指定 private IP。只有確有需要時才由部署人員顯式改 `0.0.0.0`，仍須防火牆限制。
- Go port 的 inbound ACL 只允許**實際可觀察到的 Nginx 來源 IP**；同時檢查 IPv4／IPv6、容器／NAT 來源，不直接套範例位址。
- ACL 是 network boundary，不是登入替代品。即使可觸達 Go，所有私有 API 仍驗證 session。
- 後端只在 `RemoteAddr` 屬於 `WEB_TRUSTED_PROXY_CIDRS` 時使用 forwarding headers；該部署模式下未信任來源直接拒絕 Web 請求，不信任自行宣告的 XFF。[N2]
- 本版是**單一 edge Nginx**：Nginx 覆寫 XFF 成一個已驗證的 client IP，不保留 Internet 任意輸入的長鏈。Go 檢查格式，不盲取第一個 header 值。
- Origin／Host 的預期值來自 `WEB_PUBLIC_ORIGIN`，不是從使用者任意 Host 推導。
- 若前面日後再加 CDN／第二層 proxy，另做明確 trusted-chain 設定；不為本版默認信任所有 `X-Forwarded-*`。

### 15.3 Nginx 範例的交付方式

使用者已有 SSL，本包提供的是 `http` scope 的 map 與**加入既有 HTTPS server**的 location 範例；不要建立另一份互相衝突的 `server_name`／憑證設定。[N1][N3]

`http` context：

```nginx
map $http_upgrade $console_connection_upgrade {
    default upgrade;
    ''      close;
}
```

既有 HTTPS server 內，示意：

```nginx
# 網域與 upstream IP 請換成實際值。
# API 禁止 cache；不要用 CDN cache 私人回應。
location / {
    proxy_pass http://10.0.0.20:8080;
    proxy_http_version 1.1;

    proxy_set_header Host trade.example.com;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-Host trade.example.com;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header Forwarded "";

    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $console_connection_upgrade;

    proxy_connect_timeout 5s;
    proxy_read_timeout 90s;
    proxy_send_timeout 90s;
    proxy_buffering off;
    proxy_cache off;
    proxy_next_upstream off;
    client_max_body_size 64k;
}
```

另外加入 `/api/ws` 的適當長連線 timeout／proxy buffering 與敏感 dotfile 拒絕，詳見隨附範例。所有 websocket header、Host、forwarding header 必須在實際匹配 location 生效，不因 Nginx `proxy_set_header` 的繼承規則漏設。[N3]

`proxy_next_upstream off` 避免 reverse proxy 在交易請求上自行嘗試另一個 backend；仍不能取代 application idempotency。外部 HTTP→HTTPS redirect、default vhost 拒絕未知 Host 與有效憑證沿用原有 Nginx 設定。

### 15.4 Cookie 與外部 URL

Browser 看到的是 HTTPS，因此 Go 即使上游收到 HTTP，仍設定 `Secure` cookie；由 `WEB_PUBLIC_ORIGIN` 和可信 proxy policy 決定，不用 `r.TLS != nil` 作唯一判斷。

Browser API 使用相對路徑，WS 使用公開 origin 的 `wss://.../api/ws`，不得把 `10.0.0.20` 發給 Browser 或硬寫 `ws://` 導致 mixed content。[S1][S3]

### 15.5 部署驗收

1. Nginx 主機可連到 Go 指定 port；其他不被允許的主機無法直連。
2. 外部經 HTTPS 登入成功；cookie 含 Secure／HttpOnly／SameSite，無 Domain。
3. 經反向代理 `/api/ws` 回 `101`，連線維持且 logout／expiry 能關閉。
4. 修改 Origin／Host／forwarded IP 不能繞過驗證或限流。
5. `/.env`、`/.ENV`、`/.git/config`、state／key 檔案回 403／404，不被 SPA fallback 暴露。
6. 登入後 API 不被 proxy cache；未登入查資產仍 401。
7. 實際執行 `nginx -t` 後才由部署人員 reload。Codex 沒有實際機器／憑證時標 NOT_RUN，不宣稱部署成功。

---

## 16. 建置、啟動與交付

### 16.1 建置相容性

提供一個 `make build` 或等效入口：安裝鎖定的前端相依 → typecheck／build → 放入指定 embed 目錄 → 明確產生 Go binary。

既有 CLI 的 `go test ./...` 與純 CLI build 不能因未產生 `dist/` 就失敗。可用 `webui` build tag 將真實 embedded assets 與無 UI stub 分開；**full Web binary 缺 assets 必須報明確錯誤，不能悄悄用假頁面。**

下列為候選命令；只有儲存庫存在這些路徑才採用，否則以實際名稱更新：

```bash
# 開發階段
npm --prefix web ci
npm --prefix web run typecheck
npm --prefix web run build

# make build 須包含把 dist 放入實際 go:embed 位置的步驟
make build

# 交付必須產生明確檔案，而不是只 go build ./...
# 例如：go build -tags webui -o ./bin/venuewire ./cmd/venuewire

# 程式自行讀檔，不需要 source / export
./bin/venuewire --env-file /etc/trading-console/console.env web
```

Go 提供靜態前端；不把 Vite dev server 當部署 server。交付可執行檔與 SHA／build info，沿用專案現有名稱 `venuewire`。

開發測試可透過 httptest／Browser test harness 注入 config；不要為了本機 HTTP 測試在正式遠端部署留一個可關閉全部驗證的旗標。

### 16.2 運行生命週期

收到 SIGTERM 時停止新 quote confirm，讓已保存意圖保持可恢復，持久化狀態、關閉 Browser／exchange 連線，限定時間退出；不因服務停止自動清倉或取消非本程式訂單。

啟動後先恢復 pending intents／reservations，再開放 Web 交易；read-only 首頁可提早顯示 recovering 快照。登入者離線不影響後端恢復。

### 16.3 Codex 必須交付

- 增量程式、Go／前端測試、實際建置與啟動指令。
- `.env.example`，僅空 secret，保留舊設定相容性。
- `docs/v3/BASELINE_AUDIT.md`、`PROTOCOL_NOTES.md`、`API.md`、`DEPLOYMENT.md`、`DEMO.md`。
- 對照實作的 Nginx map／location include 與部署 ACL 說明。
- `IMPLEMENTATION_STATUS.md`、`TEST_REPORT_V3.md`、繁體中文 `CODEX_HANDOFF_V3.md`。
- UI 截圖：login、Bybit balance、Deribit balance、review、waiting、filled／partial、balance syncing／synced、degraded。
- 測試證據去敏感化；不保留測試使用者帳密、cookie、API secret 或真實 account ID。

---

## 17. 分階段實作與退出條件

| Phase | 工作 | 退出條件 |
|---|---|---|
| 0 | 基線盤點、回歸測試、差異矩陣 | 有真實程式現況、已知失敗與保留清單 |
| 1 | dotenv、設定驗證、登入／session／CSRF、proxy trust | 缺必要設定安全失敗；所有私人路由不可未登入讀取 |
| 2 | Account normalization、私人 WS、恢復、API | 兩家都能呈現 fixture 與實際可用帳戶；不是 positions 充 balance |
| 3 | Vue 主頁、venue store、Browser WS、估值 | 切換不串資料；價格變但數量不變時估值會更新；stale 可見 |
| 4 | Spot metadata、capacity、quote／decimal／fee | 四個方向映射與 spend 上限測試通過；不曾送出實單 |
| 5 | Confirm／intent／reservation／tracker、Modal 結果 | 雙擊／timeout／重啟不重單；filled／partial／unknown 正確 |
| 6 | 成交後 balance sync、UX、Nginx 建置部署範例 | Modal 與首頁同時更新；cookie／WSS／proxy 測試通過 |
| 7 | Testnet opt-in、回歸與交接 | 已完成本機驗收；外部結果逐項列 PASS／BLOCKED／NOT_RUN |

外部驗證受阻不是停止本機工作的理由，也不是降低驗收要求或製造假成功的理由。

---

## 18. 測試與正式驗收矩陣

### 18.1 設定／驗證／代理

| ID | 案例 | 必須結果 |
|---|---|---|
| T01 | OS env 與 dotenv 同名、OS 空值 | OS 優先；必要值空時報錯，不取舊 secret |
| T02 | 引號、空白、`#`／`$`／反斜線、`.ENV` 指定 | parser 行為已測且記錄；不執行 shell |
| T03 | 檔案缺失／malformed、缺密碼／session secret | 符合 §4，Web fail closed，舊 CLI 不被 Web 必填阻擋 |
| T04 | 未登入 HTTP／WS／已存在舊 dashboard endpoint | 所有私人資料與寫入被拒絕，不只新路由加驗證 |
| T05 | 正確登入、錯誤帳密、爆破限流 | Secure cookie 正確，統一錯誤，429 有效且不永久鎖定 |
| T06 | logout／TTL／重啟 | session 失效；舊 WS 關閉；訂單仍可恢復 |
| T07 | 惡意 Origin／Host／CSRF、偽 XFF | 全部按 policy 拒絕；不能偽造 trusted client IP |
| T08 | Nginx private IP、WSS、direct port、dotfile | 部署條件可驗證，無 mixed content，不能抓 secret |
| T09 | 全站唯讀模式 | 修改前端／直接 POST 仍不能交易 |

### 18.2 帳戶／行情／畫面

| ID | 案例 | 必須結果 |
|---|---|---|
| T10 | Bybit／Deribit 正常非零、多幣、零值、負債 | 每幣／欄位語意正確，未知不變零 |
| T11 | Deribit `summaries` 與 `positions=[]` | 有餘額仍顯示，空 derivatives positions 不等於沒資產 |
| T12 | wallet 無 initial snapshot；只價格變 | 初始數量正確，估值持續更新，不等待 wallet 事件 |
| T13 | 私人連線靜默但 heartbeat 正常 | 不以「沒有成交」誤判 offline |
| T14 | delta 只含 BTC、完整快照省略零值 | 不誤清 ETH；按已驗證快照語意處理歸零 |
| T15 | snapshot 與私人事件交錯／重連 buffer overflow | 正確合併；不能確認時顯示 Recovering 並阻擋新單 |
| T16 | USDT 非 1 USD、無匯率、未知代幣 | 不硬填 USD 1:1，不假裝完整總額 |
| T17 | 帳戶有 derivatives／cross-collateral | 不重算錯誤完整 equity，不重複相加 aggregate |
| T18 | venue 切換、舊 HTTP／WS 晚到 | 不串帳戶、不共用 global selected venue |
| T19 | 多 Browser 同時看、慢客戶端 | exchange API／WS 連線不線性增加；慢 client 不阻塞交易 |
| T20 | WS gap／instance 改變／reload | resnapshot、pending intents 恢復；不誤顯 live |

### 18.3 交易／費用／恢復

| ID | 案例 | 必須結果 |
|---|---|---|
| T21 | 四個方向的 amount、base／quote | mapping 正確；Deribit Spot 不沿用 USD contract amount |
| T22 | tick／step／最低金額／最大量 | 買價不超保護上限、賣價不低於下限；quantity 不向上取整 |
| T23 | stale／缺口／crossed book、空深度、venue risk limit | quote／confirm 阻擋或正常拒單，不改價格策略／路由 |
| T24 | Quote 過期／帳戶容量變／惡改 payload | 原 quote 不可默默重算；重新 review／confirm |
| T25 | fee 在 From／To／第三幣、1% 合成 fee、rebate | spend cap、gross／net、fee currencies 全部正確 |
| T26 | 1.00000 ETH gross、0.01000 ETH fee 合成 fixture | 顯示成交 1 ETH、淨入帳 0.99 ETH，不判部分成交 |
| T27 | ACK 但無 fill、RPC 立即含 fills、WS 先到 | 真實狀態與去重正確，不假成功／假等待 |
| T28 | IOC partial 後取消、零成交取消 | PARTIAL_CANCELLED／CANCELLED_NO_FILL，不丟已成交量 |
| T29 | 確認雙擊、並發、重整、換 request ID 重用 quote | at-most-one 本地 intent dispatch，回同資源或 conflict |
| T30 | HTTP timeout 可能已送出／Browser 斷線 | OutcomeUnknown、查詢、原 ID 恢復，不建立第二張 |
| T31 | crash 在 persist／send／ACK 各點 | 保存的 intent 可恢復；不因重啟自動重送未知單 |
| T32 | REST／WS／reconcile 同筆 execution | qty／fees／reservation 不重複 |
| T33 | 兩個 session 同時花餘額、CLI 並行寫入 | reservation／account writer 生效，無 lost update |
| T34 | trade 終態先到、wallet 後到、費用後到 | syncing 可見，兩處 balance 自動更新，不捏造 after |
| T35 | Deribit routed Spot 異步 fills（若 metadata 啟用） | 使用正確事件／查詢來源，不只等 HTTP response |
| T36 | 原 Bybit／Deribit／FIX／CLI 回歸 | 未為 Web 擴充破壞既有可用功能與資料 |

### 18.4 測試層級

1. **Unit：** dotenv、safe config、decimal／tick、quote cap、fee model、intent idempotency、account reducer、估值品質、session／proxy。
2. **本機整合：** mock exchange + Go API + WS + 持久化重啟；含掉包、事件亂序、body errors、業務拒單與時鐘注入。
3. **Browser E2E：** login → 切換兩家 → balance live → modal review/confirm → filled／partial → modal 與背景更新；登出／重登入／重整恢復。
4. **部署整合：** 有條件的本機 Nginx fixture 可驗 proxy／cookie／WSS；真實兩台部署另列。
5. **真實 Testnet：** 另設 `RUN_BYBIT_INTEGRATION`／`RUN_DERIBIT_INTEGRATION` 及**獨立寫入允許開關** `ALLOW_TESTNET_ORDERS=1`。只有 integration flag 不足以授權下單。金額受嚴格 test budget 限制，與自動 CI 隔離。

真實 Testnet 驗證需要兩家各自的 account 查詢、行情、私人事件與至少授權的 order flow 證據；不能以 fixture 成功代替。四方向中因市場／帳戶受阻的項目列 BLOCKED，不為了綠燈反覆掃單。

### 18.5 性能與資料呈現目標

以下只作受控測試驗收目標，不可寫成已量測成果：

- 正常時 account／valuation event 被後端處理後，Browser 約 1s 內可見；拆開量測交易所延遲與本程式推送延遲。
- 主要幣有有效價格變化時，quantity 不變也能看見估值更新；不靠人工亂數動畫。
- order terminal／balance updated 事件到達後，Modal 與首頁使用一致 revision。
- 前景 5 個 Browser session 的 demo 不增加 exchange 私人連線數；長時間運行及 reconnect 測試無無界 queue／goroutine 增長。

---

## 19. Definition of Done 與交接格式

### 本機功能完成

- [ ] 所有 R01–R13 有對應實作與測試。
- [ ] 舊兩家連線／CLI／FIX 回歸結果清楚且無未解釋退步。
- [ ] 不需要 shell source；fixed env login、CSRF／proxy／WSS 正常。
- [ ] 首頁呈現真實 account snapshot，估值標示資料來源與品質。
- [ ] 四方向 Spot quote、單位、0.5% 保護與費用規則可驗證。
- [ ] Confirm 具持久化冪等與 reservation，未知狀態不自動重送。
- [ ] Filled／Partial／Cancelled／Rejected／Unknown 全有 UI 與測試。
- [ ] 成交後 Modal／背景帳戶同步，費用與餘額未完整時有 pending 標示。
- [ ] full Web build 產生明確 binary；舊 CLI build 不需隱藏前端產物。
- [ ] Nginx 與 Go 跨機器設定範例、ACL、secret 防護與 rollback 文件齊備。

### 外部驗證完成

- [ ] Bybit Testnet：讀取／stream／允許的交易驗證及證據。
- [ ] Deribit Testnet：讀取／stream／允許的交易驗證及證據。
- [ ] 實際 Nginx HTTPS → 另一台 Go 的 login／WSS／trade／balance flow。

外部清單未完成時，交接名稱應為「本機實作完成，外部驗證待辦」，不可籠統稱全部完成。

`CODEX_HANDOFF_V3.md` 必須包含：

- 修改檔案與 baseline→V3 差異、資料遷移／回復方式。
- 實際 dotenv 路徑、設定名稱映射（不含值）、build／start 指令。
- 官方協定核對日期、Spot／fee／valuation 特別差異。
- 每類測試的命令、PASS／FAIL／BLOCKED／NOT_RUN、證據位置。
- Nginx／Go 真實配置仍需填寫的 placeholder。
- 已知限制、未解決意圖、下一步需使用者處理的外部條件。

---

## 20. 官方來源與核對紀錄

本節是協定核對依據，不是要求照抄範例回應。查閱日期：**2026-09-10**。Codex 在實作涉及的 method 再核對一次；預設值、UI、價格預算與驗收政策由本文件定義，不應描述成交易所規則。

### Bybit

- **[B1] Wallet balance／account scope／欄位語意：**  
  `https://bybit-exchange.github.io/docs/v5/account/wallet-balance`
- **[B2] Private wallet stream／snapshot 與 PnL 限制：**  
  `https://bybit-exchange.github.io/docs/v5/websocket/private/wallet`
- **[B3] Place order／Spot 參數／ACK：**  
  `https://bybit-exchange.github.io/docs/v5/order/create-order`
- **[B4] 商品 precision／tick／限制：**  
  `https://bybit-exchange.github.io/docs/v5/market/instrument`
- **[B5] Executions／逐筆成交與費用幣別：**  
  `https://bybit-exchange.github.io/docs/v5/websocket/private/execution`
- **[B6] Spot 真正可交易容量，不含借款的欄位：**  
  `https://bybit-exchange.github.io/docs/v5/order/spot-borrow-quota`
- **[B7] Public ticker／Spot 更新：**  
  `https://bybit-exchange.github.io/docs/v5/websocket/public/ticker`
- **[B8] 帳戶費率：**  
  `https://bybit-exchange.github.io/docs/v5/account/fee-rate`

### Deribit

- **[D1] 多幣帳戶摘要與 cross-collateral 資訊：**  
  `https://docs.deribit.com/api-reference/account-management/private-get_account_summaries`
- **[D2] 私人 portfolio 更新與欄位語意：**  
  `https://docs.deribit.com/subscriptions/user/userportfoliocurrency`
- **[D3] 商品 metadata／Spot units／routed flag：**  
  `https://docs.deribit.com/api-reference/market-data/public-get_instrument`
- **[D4] Spot 下單／IOC／回傳 order 與 trades：**  
  `https://docs.deribit.com/api-reference/trading/private-buy`
- **[D5] Deribit／Coinbase-routed Spot 的差異：**  
  `https://docs.deribit.com/articles/spot-trading-venues`
- **[D6] 依 label 查詢多筆訂單：**  
  `https://docs.deribit.com/api-reference/trading/private-get_order_state_by_label`
- **[D7] 官方 Spot 商品說明：**  
  `https://support.deribit.com/hc/en-us/articles/31424969480093-Spot-Instruments`
- **[D8] Spot 等私人成交事件：**  
  `https://docs.deribit.com/subscriptions/user/usertradeskindcurrencyinterval`

### 配置／Web 安全／Nginx

- **[C1] godotenv 的載入及 env 優先順序：**  
  `https://github.com/joho/godotenv`
- **[S1] OWASP Session Management：**  
  `https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html`
- **[S2] OWASP CSRF Prevention：**  
  `https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html`
- **[S3] OWASP WebSocket Security：**  
  `https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html`
- **[N1] Nginx WebSocket proxying／heartbeat timeout：**  
  `https://nginx.org/en/docs/http/websocket.html`
- **[N2] Nginx trusted real IP：**  
  `https://nginx.org/en/docs/http/ngx_http_realip_module.html`
- **[N3] Nginx proxy headers／timeouts／retry／inheritance：**  
  `https://nginx.org/en/docs/http/ngx_http_proxy_module.html`

---

## 附錄 A：可直接貼給 Codex 的指令

```text
請閱讀 TRADING_CONSOLE_V3_CHANGE_SPEC.md，以及儲存庫的 AGENTS.md、
README、現有 V2 規格與交接文件，先做 Phase 0 基線盤點，再增量實作 V3。

不要重建專案，不要破壞已完成的 Bybit／Deribit、CLI、FIX、資料與測試。
本版加入：程式自行讀 dotenv、固定 env 帳密登入、Vue Web 切換交易所、
即時資產／估值頁、Deribit BTC↔USDC 與 Bybit BTC／ETH↔USDT 的 Spot Quick Trade Modal。

交易必須 Review→Confirm，使用 0.5% 保護的 Limit IOC；處理部分成交、
實際費用、未知結果、持久化冪等與恢復。結果與餘額同步分開，
Modal 下方及背景首頁使用同一帳戶 store 自動更新。

Nginx 已處理 SSL，且與 Go 在不同主機。
Go 綁可設定的 private IP，只信任明確 Nginx 來源，支援 HTTPS/WSS proxy、
Secure session cookie、Origin/CSRF；不要加入 Go TLS／公開 Go port。

先完成本機測試與建置；沒有憑證或部署存取時將外部驗證標 BLOCKED/NOT_RUN。
不得自行下 Testnet 訂單、修改 Nginx／防火牆或使用舊洩漏憑證。
每階段更新 IMPLEMENTATION_STATUS.md，最後提供實際 build／start 指令、
測試報告及繁體中文 CODEX_HANDOFF_V3.md。
```

---

## 附錄 B：前後端資料契約最低欄位

這些是正規化 DTO，不是要求交易所回傳同名欄位。可沿用既有命名，但應在 `docs/v3/API.md` 提供一對一映射；所有 decimal 均維持字串，未知均可為 `null` 並帶原因。

### B.1 AccountView

| 欄位 | 要求 |
|---|---|
| `venue`、`environment`、`accountAlias`、`accountType` | 明確 scope；environment 必須是 testnet |
| `accountRevision`、`observedAt` | 該帳戶 store 的 revision 與觀測時間，不冒充交易所全域序號 |
| `syncStatus` | Ready／Recovering／Degraded／Unavailable |
| `exchangeEquityUsd`、`exchangeEquityAsOf` | 交易所回報值及時間；不支援則 null |
| `valuation.totalUsd` | 只有定價完整且 quantity basis 清楚時有值 |
| `valuation.pricedSubtotalUsd` | 已定價範圍小計，不能代替 full total |
| `valuation.basis`、`valuation.completeness`、`valuation.unpricedAssets` | 說明 holdings／equity、完整性與缺少的資產 |
| `liabilityStatus`、`hasDerivativePositions` | none／present／unknown 等可辨識狀態，控制標題與風險提示 |
| `assets[]` | 下面的 AssetView，不把 zero／unknown 當同一件事 |

AssetView 至少包括 `asset`、`balance`、`equity`、`locked`、`liability`、`availableToTrade`、`availableToTradeAsOf`、`availableStatus`、`valuationQuantity`、`quantityBasis`、`usdValue`、`priceSource`、`priceAsOf`、`quality`。

本地 holdings 估值預設取已核對語意的 cash／wallet balance，不將可用保證金或不完整 derivatives equity 當持幣量。有負債時原始 cash 的估值是 gross holdings，不能命名 net worth；沒有可靠淨量模型時保留負債資訊並降低總額完整性。只有純 Spot／無負債／完整估值時，才可使用主要卡片的 Total Account Value 標題。

`availableToTrade` 若依 route 不同，回 `capacityByRoute`；沒有支援 route 的資產顯示可觀測 balance，但可交易值為 null。絕不能把 withdrawable 或 aggregate margin 直接換個名字填入。

### B.2 QuoteView

至少包含：

```text
quoteId, venue, environment, accountAlias, routeId
fromAsset, toAsset, spendBudget
instrument, side, baseQty, limitPrice, timeInForce
referenceBid, referenceAsk, bookObservedAt, metadataRevision
priceProtectionBps, grossReceiveEstimate, netReceiveEstimate
fees[] { asset, estimatedAmount, source, calculationBasis }
sourceDebitUpperBound, thirdAssetReserves[]
accountRevision, createdAt, expiresAt
warnings[], executable, blockedReason
```

fee estimate 不是支出保護上限；模型須能把 `sourceDebitUpperBound` 約束在使用者預算內。`executable=false` 的 quote 不能因前端送了 Confirm 就被接受。

### B.3 TradeView

至少包含：

```text
intentId, clientRequestId, quoteId
venue, environment, accountAlias, routeId, nativeOrderId
submittedBaseQty, submittedLimitPrice, submittedTimeInForce
exchangeState, commandState, resultStatus, stateRevision
filledBaseQty, grossSourceSpent, grossDestinationReceived, averagePrice
fees[] { asset, amount }, netDestinationReceived, actualSourceDebit
remainingBudget, fillDetailsStatus, feeDetailsStatus, balanceSyncStatus
createdAt, submittedAt, terminalAt, lastCheckedAt
beforeAccountRevision, afterAccountRevision, discrepancyReasons[]
```

`fillDetailsStatus`／`feeDetailsStatus`／`balanceSyncStatus` 分開使用 `pending / complete / needs_review` 等明確狀態。尚未完整時不填虛構的 net result；afterAccountRevision 未核對前為 null。

若交易所 fee 總額已包含某項 extra fee，不得再加一次；欄位包含關係須在 adapter 與 fixture 中驗證。未知欄位先保存去敏感資訊的診斷，不任意將金額聚合。
