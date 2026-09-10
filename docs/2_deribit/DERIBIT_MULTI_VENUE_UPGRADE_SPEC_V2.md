# Bybit → Bybit + Deribit：多交易所增量改版規格

**版本：2.0 · 日期：2026-09-09 · 交付對象：Codex**  
**適用專案：使用者已完成、可運行的 Bybit Go 專案**  
**目標：在原專案加入 Deribit，而不是重新建立兩套互不相干的示範程式。**

> 本文件是開發要求，不是已實作或已通過測試的證明。
> 已知使用者的 Bybit 程式已完成；本文件撰寫時未取得該程式原始碼。
> Codex 必須先檢查實際儲存庫，確認 CLI、dashboard、儲存方式、REST／WebSocket／FIX 與測試現況，再做最小必要修改。
> 不得把先前 Bybit 規格中的每項要求，直接當成程式已實作的功能。

---

## 0. Codex 執行指令

先讀取儲存庫的 `AGENTS.md`、`README.md`、原始規格、交接文件與本文件。接著執行 Phase 0 基線盤點，再依序實作。除需要使用者提供新憑證、外部授權或不可逆資料操作以外，不要只交付計畫就停止。

必須遵守：

1. **增量擴充、不重寫 Bybit。** 保留現有指令、設定、畫面、資料與已修正行為。
2. **只使用 Testnet。** 不使用對話中曾貼出的憑證、不接 Mainnet、不入金、不提款、不修改帳戶槓桿或保證金模式。
3. **先實作 Deribit HTTP + WebSocket，再處理 Deribit FIX。** 核心版本不因 FIX 外部存取問題停擺。
4. **區分實作、模擬測試與外部驗證。** 沒有憑證時繼續做本機測試，但外部測試必須標為 `BLOCKED` 或 `NOT_RUN`，不能標成通過。
5. **不自動跨交易所補單或避險。** 多交易所功能先提供明確路由、獨立恢復與唯讀彙總。
6. 每一階段執行相關測試及 Bybit 回歸測試，更新 `IMPLEMENTATION_STATUS.md`。
7. 文件列出的新路徑及指令是建議介面；以現有儲存庫結構為優先，記錄實際對應，不為了符合目錄範例搬動整個專案。

### 規格優先順序

安全與資料完整性要求 > 已驗證的官方協定／實際回應 > 本文件的改版要求 > 舊規格中的理想化架構。

官方文件與 Testnet 行為不一致時：保留去識別化證據、記錄差異、縮小受影響功能的支援範圍；不得猜參數、猜單位或默默使用正式環境。

---

## 1. 版本目標與範圍

### 1.1 R1：核心多交易所版本，必須完成

- 原有 Bybit 功能持續可用。
- Deribit Testnet：HTTP JSON-RPC 查詢與下單／改單／取消。
- Deribit Testnet：WebSocket JSON-RPC 查詢、下單及私人訂閱。
- 公開行情、訂單簿、私人訂單、成交、部位與帳戶摘要。
- 共用訂單意圖、事件、成交去重、持久化及恢復流程，但保留交易所差異。
- 兩家交易所同時運行；其中一家失效不拖垮另一家。
- 唯讀的跨交易所訂單、部位、帳戶與健康狀態檢視。
- 現有 dashboard 若存在，增量加入交易所選擇與資料隔離；不存在則不另建完整前端。
- 兩家各自的外部驗證結果與證據清單。

### 1.2 R2：Deribit FIX，接續完成

- 重用適合共用的 FIX framing／codec／TLS／測試工具。
- 獨立 Deribit FIX dialect：Logon、心跳、訂單、回報、序號與恢復。
- Deribit 專屬 mock／fixture／失敗情境測試。
- 帳戶及網路允許時，執行真正的 Testnet Logon、下單、取消與回報驗證。
- 若外部條件阻擋，保留已完成實作與本機測試，明列阻擋原因；不得宣稱完成外部 FIX 整合。

### 1.3 第一版的商品範圍

**Deribit 可交易支援先限縮到 metadata 驗證通過的 `BTC-PERPETUAL`、`ETH-PERPETUAL` 反向永續合約。** 未列入允許清單的商品只能查詢，不可下單。

到期期貨：支援發現及保存到期日等 metadata，暫不要求交易。選擇權、線性商品、組合單、現貨轉送交易、股票／商品衍生品，不在第一版寫入範圍。不要因名稱看起來相近就套用反向永續合約的數量規則。[R4][R5]

Bybit 保留現有已支援商品，不把現有 Spot 功能改成 Linear，也不要求為本次改版補齊所有 Bybit 商品。

### 1.4 明確排除

不做策略、自動套利、自動跨所重送、資金轉帳、智慧路由、投資組合保證金引擎、選擇權定價、完整通用 FIX 引擎或 HFT 效能宣稱。

---

## 2. Phase 0：先盤點現有程式

建立 `docs/upgrade/BASELINE_AUDIT.md`，至少記錄：

| 項目 | 要確認的內容 |
|---|---|
| 工作樹 | 目前 commit／分支、未提交修改；不可覆蓋使用者工作 |
| Go | module 名稱、Go 版本、相依套件、實際 main packages |
| CLI | `venuewire` 或其他入口、旗標、預設交易所及輸出格式 |
| Bybit | 哪些 REST／WS／FIX 功能有程式、哪些有真實 Testnet 證據 |
| 商品 | Spot／Linear／Inverse 等實際範圍、數量單位、手續費處理 |
| 訂單 | ID、狀態更新、成交去重、取消／成交競爭的處理方式 |
| 儲存 | JSON／SQLite／其他；格式版本、寫入者、鎖、啟動恢復 |
| UI | dashboard、API、推播、下單表單是否存在 |
| 運維 | 設定來源、遮罩、限流、重連、關閉流程 |
| 測試 | 單元／整合／race／建置結果及已存在的失敗 |

**基線工作不得下單。** 先執行不需要憑證及不會修改帳戶的測試。

建議命令，依實際專案調整：

```bash
git status --short
go list ./...
go test ./...
go vet ./...
# 僅在支援的平台與工具鏈執行；不支援時明列原因。
go test -race ./...
```

建置必須提供明確的二進位檔路徑，不能只寫 `go build ./...`：

```bash
mkdir -p bin
go build -o ./bin/venuewire ./cmd/venuewire
```

以上 main package 只在儲存庫確實有該入口時使用；否則改為實際路徑並更新文件。

### 基線保護

- 對現有 Bybit 簽章、訂單查詢、成交與費用計算新增／保留回歸測試。
- 若現有 dashboard 已能顯示成交量與扣費後餘額，保留該差別，不得把淨入帳量當成成交量。
- 不硬編碼測試環境費率；實際費用及幣別以交易回報為準。[R28]
- Deribit 未啟用或未設定憑證時，原 Bybit 命令仍能獨立工作。
- 原資料的 category／account 無法可靠判斷時，停止該筆資料遷移並要求明確對應，不能全部猜成 `linear`。

---

## 3. 架構：共用業務語意，不共用錯誤假設

### 3.1 元件關係（文字示意）

```text
現有 CLI / 現有 dashboard
            |
    應用服務與明確 venue 路由
            |
   訂單意圖 / 事件處理 / 儲存
        /                 \
Bybit Adapter         Deribit Adapter
既有 REST / WS        HTTP JSON-RPC / WS JSON-RPC
既有 FIX dialect      Deribit FIX dialect
        \                 /
     共用健康監控、測試工具、唯讀彙總
```

不要建立一個要求每種 transport 都有完全相同方法的大型介面。以能力拆分：

| 邊界 | 職責 |
|---|---|
| `InstrumentCatalog` | 查商品、metadata、允許的單位與下單能力 |
| `OrderCommands` | 建立、修改、取消訂單；回傳已知狀態及可能附帶的成交 |
| `OrderQueries` | 個別訂單、開放訂單、歷史、成交分頁 |
| `AccountQueries` | 部位與餘額，保持幣別與語意 |
| `MarketEventSource` | 公開行情及訂單簿 |
| `PrivateEventSource` | 訂單、成交、部位更新 |
| `VenueRecovery` | 該交易所／帳戶的恢復協調 |
| `Capabilities` | 商品 × transport × 操作的支援矩陣 |

交易所識別不得靠 symbol 推導。所有送單服務必須拿到明確的 venue、environment、account、instrument 與 transport。

### 3.2 建議新增區域

```text
internal/venue/                  # 共用邊界；若既有 domain 已適用則沿用
internal/deribit/
    config.go
    rpc.go                       # JSON-RPC envelope、錯誤、request ID
    http.go
    auth.go
    ws.go
    instruments.go
    orders.go
    accounts.go
    subscriptions.go
    normalize.go
    recovery.go
    fix/                         # Deribit 專屬 dialect
internal/deribitmock/
docs/upgrade/
testdata/deribit/                 # 只放去識別化或合成資料
```

Bybit 原目錄可以完全不搬動。只有兩家實際需要共用的部分才抽出；不要先為未來十家交易所建框架。

---

## 4. 設定、憑證與 Testnet 防護

### 4.1 新設定

沿用現有 Bybit 變數；新增獨立的 Deribit 設定，不共用 key／secret：

```dotenv
# 範例檔只放空值，不能放真實憑證。
DERIBIT_ENABLED=false
DERIBIT_ENV=testnet
DERIBIT_ACCOUNT_ALIAS=deribit-test
DERIBIT_CLIENT_ID=
DERIBIT_CLIENT_SECRET=
DERIBIT_FIX_ENABLED=false
DERIBIT_FIX_SENDER_COMP_ID=venuewire
```

本機建議預設（本專案政策，不是交易所規定）：

- 新增的多交易所下單入口預設僅預覽，執行須明確確認。
- 只使用 `BTC-PERPETUAL`、`ETH-PERPETUAL` 允許清單。
- 送單前必須設定／通過非零風險上限，不以「未設定」代表無上限。
- 不自動修改帳戶層級 Cancel-on-Disconnect 設定。
- `DERIBIT_ENABLED=false` 時不初始化 Deribit 私人連線。

不要因新增這些設定，要求現有 Bybit 使用者無條件重建 `.env`。

### 4.2 允許的遠端位置

| 用途 | Testnet 位置 |
|---|---|
| Deribit HTTP | `https://test.deribit.com/api/v2/{method}` |
| Deribit WS | `wss://test.deribit.com/ws/api/v2` |
| Deribit FIX | `fix-test.deribit.com:9883`，TLS |

帳號與 key 必須屬於獨立 Testnet 環境。[R1][R2][R19]

只接受完整主機名稱白名單，不用字串 `contains("test")` 判斷。驗證 scheme、hostname、port；禁止跟隨 redirect 把憑證送到其他主機。正式環境、IP 替代位址、明文 FIX 連線均不作為失敗後的 fallback。

測試可注入 `httptest`／本機 TLS server；這是測試用 dependency injection，不是在正式 CLI 加任意 host 的安全繞過旗標。

### 4.3 權限與機密

核心查詢／交易使用最小必要權限：讀帳戶、讀寫交易；涉及修改 connection-scope COD 時才需要對應帳戶寫入權限。不得要求提款權限。[R3][R13]

以下內容不得出現在 log、fixture、錯誤字串、dashboard、URL access log 或報告：client secret、access token、refresh token、Authorization、FIX Password、完整 Logon／auth frame。

FIX Logon 回應可能回送敏感欄位，因此**接收方向也必須遮罩**。機密不得放進命令列旗標。既有對話中貼過的 Bybit 憑證不能複製到程式或新文件。

---

## 5. 識別碼、商品及數量單位

### 5.1 複合鍵

禁止用單獨的 `order_id`、`symbol` 或 `trade_id` 作為全域鍵。

至少具備：

```text
MarketKey     = venue + environment + market_kind + native_instrument
AccountKey    = venue + environment + stable_account_alias
OrderKey      = AccountKey + native_order_namespace + native_order_id
ExecutionKey  = AccountKey + native_trade_namespace + native_trade_id
PositionKey   = AccountKey + MarketKey + native_position_side_or_index
```

`native_*_namespace` 必須覆蓋該交易所 ID 的實際作用範圍，例如 currency／category／instrument。帳戶別名需穩定且對應單一實際帳戶；改 key 但未改帳戶，不應生成另一套持倉。

### 5.2 商品 metadata

每個商品保存原始 metadata 以及正規化欄位：

```text
venue / native_instrument / kind / contract_style
base_currency / quote_currency / settlement_currency
native_order_amount_unit / position_size_unit
contract_size / quantity_increment / minimum_order_amount
price_tick / tick_size_steps / expiration_timestamp
is_active / native_state / fetched_at / source
```

從 `public/get_instruments`／`public/get_instrument` 發現商品，不在原始碼硬寫數量步進或 tick。metadata 只列出商品，不自動賦予交易能力；unsupported contract style 必須拒絕送單。[R5]

**`contract_size`、最小委託量及數量步進是不同概念。** 不因某一個商品數值剛好相同，就用同一欄位代表三件事。保留官方欄位語意，對目標商品建立明確測試。

### 5.3 Deribit 數量不可直接套用 Bybit

本版目標 `BTC-PERPETUAL`／`ETH-PERPETUAL` 的反向合約，HTTP／WS `amount` 以 USD 面額表示；不是買幾顆 BTC／ETH。其他商品類型不保證同樣單位。[R4]

因此禁止：

```text
Bybit qty=1 → 原封不動轉成 Deribit amount=1
```

內部請求必須帶單位，例如：

```text
instrument = BTC-PERPETUAL
native_amount = 100
native_amount_unit = USD
```

這只是資料語意範例，不是固定下單值。實際大小與價格必須經過最新 metadata、餘額及本機限制檢查。

第一版不要同時傳 `amount` 與 `contracts`；先選擇一種清楚驗證的表示法。其他表示法只有完成換算與協定測試後才啟用。

### 5.4 精度

沿用專案既有可靠 decimal 型別；沒有時選擇固定精度或成熟 decimal 實作。不得用 `float64` 做數量合法性、金額、費用或部位累加。

Deribit JSON 數值須以 `json.Number`／等價 decimal decoder 保留精度，送出時產生合法 JSON number，不能因內部儲存用字串，就把所有參數發成 JSON string。

數量不合法時預設拒絕並提供合法建議，不默默四捨五入放大訂單。價格採用分段 tick 時，在邊界上下各加測試。

### 5.5 費用與成交

保存 `gross_fill_amount`、`fee_amount`、`fee_currency`、`liquidity_role`；費用可以包含回扣，不能任意取絕對值。

現貨淨餘額變化不等於成交量；衍生品成交量也不等於買入該數量的現貨。不同幣別的手續費分開呈現，沒有有時效的匯率就不輸出一個看似精確的 USD 總數。[R4][R28]

---

## 6. Deribit JSON-RPC 與驗證

### 6.1 共用 RPC 層

Deribit 提供 JSON-RPC over HTTP／WebSocket。HTTP 支援 GET 與 POST；本專案私人操作優先採 POST JSON body，避免把憑證或交易參數放在 URL。[R1][R2]

使用官方 method 路徑 `/api/v2/{method}`，fixture 及外部測試確認實際 envelope，不直接複製文件產生器中的 GET-body 範例。

RPC 層要求：

- 解讀 `jsonrpc`、`id`、`result`、`error.code`、`error.message`、`error.data`。
- HTTP 200 不代表業務成功。
- 限制 response／frame 最大尺寸，超過限制視為協定錯誤。
- WS request ID 在連線世代內唯一；pending map 有上限及 timeout 清理。
- 同一 WS 上回應可不依送出順序抵達，以 ID 對應，不能 FIFO 配對。
- `method=subscription`、`method=heartbeat` 與一般 RPC 回應分流。
- 重連增加 connection generation；舊連線的遲到回應不能完成新連線的 request。
- 定義 typed error：`Auth`、`Permission`、`Validation`、`RateLimited`、`Unavailable`、`OutcomeUnknown`、`Unsupported`。

### 6.2 Token 管理

第一版使用 `public/auth` 的 `client_credentials`，支援 `refresh_token` 續期。HTTP 用 Bearer header；WS 使用已驗證的連線／token scope，並依官方方式攜帶 access token。不可假設 HTTP token 能無條件跨所有連線重用。[R3][R6]

要求：

1. 依回應 `expires_in` 計算到期，不能硬寫一年。
2. 同一 token scope 的續期需 single-flight，避免每個 goroutine 同時 refresh。
3. 原子更新新 access／refresh token；機密只存在受控記憶體。
4. refresh 失敗可重新驗證，但不得因此自動重送結果不明的下單。
5. 讀取 granted scope，缺權限立即顯示；不以不斷重連掩蓋權限錯誤。
6. 明確區分 connection scope、session scope 及所用 token 的生命週期。

### 6.3 限流

Deribit 有 credit-based 限流；`10028 / too_many_requests` 可能伴隨斷線。不能直接套用 Bybit 的 rate-limit headers 或每秒額度。[R7]

每個 venue／account 使用獨立 limiter，涵蓋 HTTP、WS RPC 及會共同消耗帳戶額度的活動；保留訂閱／心跳／恢復的控制容量。限流後 cooldown + backoff + jitter，不立即登入風暴。metadata 要快取，不能每個 tick 重新抓商品清單。

---

## 7. Deribit HTTP 功能

以下是本版需要的功能對應；每個方法的參數、scope、分頁與支援商品須以官方頁面確認，並記錄在 `docs/upgrade/PROTOCOL_NOTES.md`。[R4][R5][R14–R18][R29–R33]

| 功能 | 方法 |
|---|---|
| 連線／時間診斷 | `public/test`、`public/get_time` |
| 商品資訊 | `public/get_instruments`、`public/get_instrument` |
| 行情快照 | `public/ticker`、`public/get_order_book` |
| 驗證／續期 | `public/auth` |
| 買／賣 | `private/buy`、`private/sell` |
| 取消／改單 | `private/cancel`、`private/edit` |
| 訂單 | `private/get_order_state`、`private/get_open_orders_by_instrument` |
| 依 label 搜尋 | `private/get_order_state_by_label` |
| 訂單歷史 | `private/get_order_history_by_instrument` |
| 成交查詢 | `private/get_user_trades_by_instrument`、`private/get_user_trades_by_order` |
| 部位 | `private/get_positions` |
| 帳戶 | `private/get_account_summary` |

核心示範：啟動私人 WS → 以 HTTP 下小額限價單 → 處理 HTTP 的訂單／成交資料 → 收私人事件 → 改單或取消 → 查詢確認。

Deribit 的下單結果可以已包含 `order` 與 `trades`；不能把所有交易所回應都降級成 Bybit 式的純 ACK。回應附帶的成交與後續推播必須走同一去重管道。[R4][R27]

---

## 8. WebSocket 行情、交易與私人資料

### 8.1 連線分離

預設兩條 Deribit WS：公開行情連線，以及私人交易／事件連線。這是本專案的隔離設計，避免大量 orderbook 訊息阻塞私人回報。帳戶限流仍要共用，不把多開連線當成增加額度。

### 8.2 公開訂閱

以 `public/subscribe` 訂閱第一批商品：

```text
ticker.BTC-PERPETUAL.100ms
trades.BTC-PERPETUAL.100ms
book.BTC-PERPETUAL.100ms
```

ETH 可依允許清單加入。先用 `100ms`，不把需要額外授權的 `raw` 當成預設。[R1][R8]

### 8.3 訂單簿正確性

`book.{instrument}.{interval}` 提供初始 snapshot 與增量變更，增量有 `change_id`／`prev_change_id`；更新含 `new`、`change`、`delete`。[R8]

必須實作：

- snapshot 原子取代該 instrument 的完整 book。
- delta 只在 `prev_change_id == last_change_id` 時正常套用；不要求 ID 每次加一。
- 重複訊息可丟棄並計數；失序／缺口必須把 book 標為不可信。
- 發現缺口後停止使用該 book 做送單預覽／風險檢查，重訂閱並等新 snapshot。
- 重連後清除舊連線 generation 的 delta。
- 不把缺口後的 delta 硬接到任意時間抓到的 HTTP snapshot；除非實作並驗證明確的序號銜接演算法。
- 每個商品 book 設大小與記憶體限制；完整 book 超限時停止／降級顯示，不能偷偷截斷後仍自稱完整。

### 8.4 心跳

使用 `public/set_heartbeat` 啟用應用層心跳；收到 `test_request` 必須呼叫 `public/test`。WS control ping／pong 不代替這個流程。[R9]

心跳、RPC 回應及私人控制訊息使用保留容量，不因行情 queue 塞滿而卡死。沒有成交不代表私人連線過期；健康檢測結合心跳、讀寫狀態及恢復結果，不能只看最後一筆 trade 的時間。

### 8.5 私人訂閱

第一版使用 `private/subscribe` 訂閱：

```text
user.changes.BTC-PERPETUAL.100ms
user.changes.ETH-PERPETUAL.100ms
```

只訂閱實際啟用的商品。解析同一訊息中的 `orders`、`trades`、`positions` 陣列，不假設各只有一筆。[R10]

帳戶餘額以 `private/get_account_summary` 做啟動及有節制的定期快照；需要即時餘額時再接 `user.portfolio.{currency}`。不把 derivatives position 當成現貨 balance。[R18][R26]

### 8.6 WS 下單

WS 下單／改單／取消是 R1 必需，不只接收行情。共用 §7 的 method 與商業驗證，但用 request ID 做 RPC 配對。[R2][R4]

私人事件可能先於 RPC 回應抵達；同一訂單的回報也可能被 HTTP 查詢、WS notification 和 FIX 重複觀察。所有來源都要能重入同一事件 reducer。

### 8.7 斷線即取消（COD）

Deribit COD 作用於特定連線所建立的訂單；不是帳戶所有 transport 的總開關。HTTP 訂單不因另一條 WS 斷線而獲得相同保護。`private/logout` 的正常登出也不能當成一定會觸發 COD。[R13]

本版規則：

- 啟動時查詢並展示有效 COD 設定。
- 只在使用者選擇時啟用 connection-scope COD；不自動改 account scope。
- 自動化 WS 交易測試可要求 COD 開啟；若無權限，該測試標明阻擋，不偷偷修改帳戶權限。
- 記錄每張訂單來自哪個 transport／connection generation。
- COD 開啟也不是同步取消保證；斷線後仍需查詢訂單及成交。
- 關閉程式前如要取消，明確取消本程式擁有的訂單並確認；不依賴 Logout 的副作用。

---

## 9. 訂單意圖、冪等與狀態機

### 9.1 三種識別分開

```text
IntentID      = 本程式持久化的唯一業務意圖
RequestID     = 一次 HTTP／WS／FIX 嘗試的 correlation
NativeOrderID = 交易所回傳的訂單 ID
```

新 IntentID 使用不含個資的短識別，例如 `cx-` + 26 字元隨機 ID；映射至 Bybit `orderLinkId` 或 Deribit `label`，但保留各自原值。

**Deribit `label` 可以對應多張訂單，不是交易所保證的唯一鍵，也不是 exactly-once 保證。** 依 label 查詢回傳多筆時必須列為異常，不能隨便選第一筆。[R11][R12]

### 9.2 Write-ahead intent

送單前先持久化：venue、account、instrument、side、原生數量與單位、價格、選項、IntentID、attempt 與建立時間。

同一 IntentID 不能被不同 goroutine／CLI 程序並行送出。依現有架構選擇單一寫入程序、檔案鎖或既有資料庫交易；不以記憶體 mutex 冒充跨程序保護。

### 9.3 結果不明時

```text
持久化意圖 → 已嘗試送出 → 逾時／斷線 → OutcomeUnknown
                                        |
                                  查訂單／成交／歷史
```

收到 timeout 不能假設拒單。禁止把 WS 訂單逾時自動轉送 HTTP，也禁止改到另一家交易所送同一經濟意圖。

恢復依據：NativeOrderID；未知時依 label 加完整參數比對；配合私人事件及近期／歷史查詢。暫時查不到不證明沒下成功。經有限次查詢仍無法確定，保持 `OutcomeUnknown / NeedsReview`，暫停該意圖後續寫入。

本版不對未知結果自動建立第二張訂單。使用者需先完成核對，才能明確建立新意圖。

### 9.4 ExchangeState 與 CommandState 分開

```text
ExchangeState:
    Unknown / Open / PartiallyFilled / Filled / Cancelled / Rejected

CommandState:
    Idle / Submitting / Amending / Cancelling / OutcomeUnknown
```

Deribit `open` 且 `0 < filled_amount < amount` 可正規化為 `PartiallyFilled`；保留 `raw_order_state`。不支援的 untriggered／其他類型只保存並標示不支援，不錯誤當成 Open。[R4][R29]

取消被拒不代表原訂單 Rejected。取消途中仍可能成交；取消成功也可能保留部分成交。`Cancelled` 不可把已成交數量歸零。

### 9.5 事件合併

- 成交以穩定 ExecutionKey 去重，持久化去重紀錄與游標。
- 訂單累計成交量與逐筆成交加總是兩種觀察，不可再彼此相加。
- HTTP 回應已含成交時先入帳；同筆 WS／FIX 回報不再次增加成交量或費用。
- 原生 revision／時間戳／事件種類共同判斷新舊；不要只靠狀態名稱排序。
- 改單會改變總量；`max(cum_qty)` 不能單獨構成完整訂單 reducer。
- 衝突且無可靠先後時，標記 discrepancy 並查詢，不默默覆蓋。
- 帳戶部位 snapshot 與本地成交推導分開，避免把同筆成交算到部位兩次。

---

## 10. 恢復與分頁

### 10.1 每個 venue／account 獨立恢復

啟動、私人 WS 重連、FIX session 改變、人工命令均可觸發。相同帳戶只允許一個 recovery run；新觸發可合併但不能無限排隊。

狀態：

```text
Disconnected → Authenticating → Subscribing → Recovering → Ready
                                    └────────→ Degraded / NeedsReview
```

恢復中，停止該帳戶增加曝險的新單；允許有明確 ID、能力及最新資料支持的取消操作。健康的另一交易所不受此帳戶的 circuit breaker 連帶封鎖。

### 10.2 不可只抓 open orders

訂單不在 open list，可能已成交、取消、過期、查詢漏頁或尚未可見；不是自動 Cancelled。

恢復至少使用：本地未完成意圖、open orders、個別 state、近期／歷史 orders、trade history、positions、account summary。[R14–R18]

### 10.3 Snapshot 與即時事件銜接

1. 登入及訂閱成功後開始把私人事件放入有界恢復緩衝區，記錄 generation。
2. 取得查詢快照及從最後持久化游標開始的成交資料。
3. 先寫入去重後成交，再合併訂單與部位觀察，重播緩衝事件。
4. 對仍衝突的活動訂單重查，不假設 HTTP 多次查詢構成原子快照。
5. 全部必要查詢成功、緩衝已追上且沒有影響送單的未知狀態，才進入 Ready。
6. 緩衝超限／再次斷線時維持 Degraded，重跑恢復；不能丟掉私人事件後顯示 Ready。

本版不宣稱跨交易所原子快照。彙總輸出必須帶每家 `as_of`、完整性及資料年齡。

### 10.4 分頁與歷史資料

Deribit 有近期與 `historical=true` 查詢路徑；歷史索引可能延後出現。長時間停機不能只查近期預設範圍。[R16]

- 實作各 method 專屬分頁，不設一個適用所有 API 的假 cursor。
- 成交優先使用 instrument scope 的 `trade_seq`／官方支援游標；保存 scope，不當成全交易所連續編號。[R15]
- 時間範圍查詢要處理同毫秒多筆成交，不使用單純 `last_timestamp + 1` 跳頁。
- 邊界重疊查詢並依 TradeID 去重，直到 `has_more`／continuation 完成。
- 訂單歷史查詢要包含尚未成交即取消的訂單，依方法設定相關選項。[R14]
- 無法取齊全部資料時回傳 `Partial` 及缺口；不得把部分資料標記為完整。
- 只有資料成功落盤後才推進 recovery cursor。
- 恢復不建立新單、不自動平倉、不取消不屬於本程式的訂單。

---

## 11. 儲存與相容性遷移

沿用現有儲存技術，不為第二家交易所強制引入 Kafka、資料庫或新服務。

如果現有 JSON 結構無版本，加入 `schema_version`。遷移必須：

1. 備份原資料，不覆蓋唯一副本。
2. dry-run 顯示將補上的 venue／environment／account／category。
3. 依可驗證基線設定補上 Bybit 身分；不明欄位停止遷移。
4. 原子寫入新格式，保留回復方式。
5. 重跑遷移結果相同；不能重複生成訂單／成交。
6. 未支援的未來版本明確拒讀，不當成空資料啟動。

下列內容必須持久化：意圖、原生 order ID 對應、成交、費用、恢復游標、必要 order state、帳戶映射與 schema 版本。公開行情可不持久化。

多個 CLI／dashboard 同時讀寫時不得 lost update。單檔 atomic rename 不等於跨程序交易鎖；需要明確單寫者或鎖協定。

---

## 12. 多交易所檢視與風險界線

### 12.1 R1 必需的唯讀檢視

顯示 venue、environment、account、原生 instrument、contract style、原生數量、單位、方向、mark／index、幣別、最後更新、連線狀態與恢復狀態。

`--venue all` 只適用查詢、唯讀彙總與恢復；對 place／amend／cancel 等寫入操作必須拒絕。取消 ID 屬於別家時回傳明確錯誤，不尝試別家同號訂單。

### 12.2 不可錯誤相加

禁止直接把 Bybit 的 BTC 數量加上 Deribit 的 USD 面額；也不能把 BTC 餘額、USDT 餘額與 USD 損益放進同一 `balance`。

第一版預設顯示原生值及分組小計。若加上估計名目曝險：

- 每個商品必須有已驗證的換算規則。
- 明列估值價格、幣別、時間、FX 來源與完整性。
- 沒有有效 USDT／USD 等匯率時，不暗設 1:1 後輸出正式總額。
- 反向合約 `USD notional / price` 最多作為明確標示的基礎幣等值估算，不命名為完整 delta 或可跨所抵銷的保證金。
- 交易所保證金彼此獨立；名目多空抵銷不代表單邊不會被清算。
- 未支援的部位保留可見，不能排除後仍說總曝險完整。

這是唯讀觀測功能，不是用來自動調整避險的風險引擎。

### 12.3 故障隔離

任一家 auth／限流／queue／重連失敗，只改變該 venue 的狀態。`all` 查詢可回傳成功部分，但整體必須標記 Partial，不能把失效 venue 的部位顯示為零。

---

## 13. CLI 與既有 dashboard

### 13.1 相容性

沿用既有 binary 名稱。可新增全域 `--venue bybit|deribit`；舊指令沒有該參數時維持原來 Bybit 行為。新增 generic alias 可列為非必要，不能強制改名破壞腳本。

下列為目標能力範例，不代表目前已有這些子命令：

```bash
./bin/venuewire --venue deribit doctor
./bin/venuewire --venue deribit instruments --currency BTC --kind future
./bin/venuewire --venue deribit market trades --instrument BTC-PERPETUAL
./bin/venuewire --venue deribit market orderbook --instrument BTC-PERPETUAL
./bin/venuewire --venue deribit private-stream --instrument BTC-PERPETUAL
./bin/venuewire --venue deribit orders list --instrument BTC-PERPETUAL
./bin/venuewire --venue deribit positions --currency BTC
./bin/venuewire --venue deribit reconcile
./bin/venuewire --venue all status
./bin/venuewire --venue all portfolio
```

`doctor` 可做讀取／連線驗證，但不可下單、改設定、改 COD、取消或平倉。

### 13.2 新送單流程

新多交易所入口採兩階段：`plan` → 明確 `execute`。能沿用既有安全確認介面時，不需再做重複 UI。

plan 至少輸出：venue／Testnet／帳戶、商品、數量及單位、價格、有效 tick、預估面額、上限、transport、COD 是否涵蓋、metadata／價格時間及到期時間。

可提供 `min-valid` 數量策略及 passive 價格策略，但計算須依 metadata 與最新行情；不得硬寫當天 BTC 價格或保證非成交。限價不得跑到交易所限制之外。

execute 要重新檢查 plan 的時效、商品狀態、資料健康與風險限制。過期或數量換算不明就拒絕。明確選擇 HTTP 或 WS，不因失敗換 transport 重送。

### 13.3 Dashboard 存在時

- 預設仍顯示原本 Bybit；新增 venue／account 選擇。
- 每一行訂單、成交、持倉都帶 venue，前端 row key 也用複合鍵。
- 表單依商品顯示 `amount (USD)` 或實際單位，不統一寫成「數量 BTC」。
- all 模式唯讀；任何寫入要求重新確認單一交易所。
- 顯示 `Recovering / Stale / Partial / NeedsReview`，不只 Connected 綠點。
- 沿用現有推播架構，勿為每個瀏覽器頁籤建立一整組交易所私人連線。
- log 遮罩、內部錯誤、token 都不能經由 UI API 外洩。

若目前沒有 dashboard，完成 CLI 即可，不額外擴大範圍。

---

## 14. Deribit FIX：獨立 dialect，不能只換主機

### 14.1 版本與文件

本版鎖定官方目前標示的 **production/classic FIX 4.4 subset** 文件，連線仍只用 Testnet。`production` 是文件的現行協定分支，不代表允許使用真錢環境。[R19]

不要把 `/upcoming/` 或 Starbase 另一套規格混進現行 codec。若 Testnet 已切換版本，先用非交易診斷確認、記錄版本及對應文件，再實作相應 dialect；版本不明禁止下單。

### 14.2 可共用與不可共用

可共用：SOH framing、BodyLength、CheckSum、TLS transport、時鐘／隨機數注入、session 測試工具。

不可直接共用：Bybit RSA 驗證、Bybit 自訂 tags、OrderQty 單位、ClOrdID 回報對應、是否重傳、sequence reset 規則及 cancel-on-disconnect 行為。[R19–R25][R34]

### 14.3 Logon

使用 TLS `fix-test.deribit.com:9883`；`TargetCompID=DERIBITSERVER`。驗證最小邏輯：[R19][R20]

```text
nonce_bytes = cryptographically_secure_random(32 bytes)
nonce_b64   = Base64(nonce_bytes)
timestamp   = strictly_increasing_unix_ms
RawData(96) = decimal(timestamp) + "." + nonce_b64
Username(553) = client_id
Password(554) = Base64(SHA256(bytes(RawData) || bytes(client_secret)))
```

這不是 Bybit 的 RSA，也不是把 SHA256 改成 HMAC。不要把 Base64 的十六進位字串誤當原始 digest。

採測試可注入的時鐘與隨機來源，驗證同毫秒重試、時鐘倒退、換 key、拒絕登入及敏感回應遮罩。安全 timestamp 水位可持久化但不能包含 secret。

明確設定 `HeartBtInt` 及 COD 政策。不要假設未填 tag 的預設值在所有帳戶一致。

### 14.4 Session 與恢復

Deribit 現行文件列出 `ResendRequest(2)`、`SequenceReset(4)`，與 Bybit 的無標準重送路線不同。Deribit 的 reset 回覆及序號規則也有場域特有語意。[R21][R22]

要求：

- 獨立 `DeribitSessionPolicy`，不能把 Bybit「發現 gap 就重連、全部歸 1」套入同一 session。
- 支援辨識 Heartbeat、TestRequest、Logout、Reject、ResendRequest、SequenceReset。
- 驗證 outbound sequence、server resend 要求及 reset 只能前進等實際行為。
- inbound sequence 不能假定與 outbound 有相同模型；記錄使用的 logon 選項及排序保證。
- 不假設 Deribit 必然提供跨 socket 的永久歷史重播；重新登入後仍做 HTTP 業務狀態核對。
- 本版對序號異常採 fail-closed：暫停新單、記錄缺口、執行已驗證的 session 恢復或關閉／重新登入，然後核對訂單。
- 不對結果不明的 `NewOrderSingle` 自動 replay。若未完成安全的重傳處理，明確拒絕該恢復分支並重新登入／核對，不能宣稱支援完整 FIX replay。
- 這個有限範圍必須在能力矩陣及 README 顯示，不得靠 generic FIX engine 的預設行為猜測。

### 14.5 訂單與回報

至少處理：[R23–R25]

```text
NewOrderSingle       35=D
OrderCancelRequest   35=F
OrderCancelReplace   35=G
ExecutionReport      35=8
OrderCancelReject    35=9
```

重要差異：

- Deribit 現行 ExecutionReport 的 `ClOrdID(11)` 不可一律當成客戶原始 ID；對照 `OrderID(37)`、`OrigClOrdID(41)` 等欄位建立 mapping。[R25]
- JSON `amount` 與 FIX `OrderQty(38)` 不可直接假定同單位。先從 FIX SecurityList／相應商品規格取得 multiplier，證明輸入、回報及 JSON 核對的換算一致，才允許 FIX 下單。[R23][R25]
- 回報可能有訂單狀態與成交通知兩種用途。完整處理選定 fill 格式；可以明確使用官方支持的 `ReportFillsAsExecReports(9015)` 模式，但需要對照 fixture 及外部回應驗證，不能因 parser 不支援 groups 而漏掉成交。[R20]
- codec 不可用一個 `map[tag]value` 就丟掉重複群組。未知 tags 保留，未知群組明確拒絕／降級，不靜默誤解析。
- FIX 與 JSON 沒有已驗證的一對一 trade ID 對應時，不用價格＋數量＋毫秒猜同一筆成交。把 FIX 視為 order-state evidence，帳務去重以可核對的 canonical JSON trade ID 為準。

### 14.6 驗證層級

```text
IMPLEMENTED           有程式
LOCAL_TESTED          fixture/mock 通過
TESTNET_LOGON         真正登入成功
TESTNET_ORDER_FLOW    真正下單/取消/回報可對照
```

每一層分開記錄。登入成功不能代表下單成功；mock 通過不能代表 Deribit 接受該 dialect。

---

## 15. 觀測、失敗隔離與關閉

所有結構化紀錄含 venue、environment、account alias、transport、connection generation、method/topic、IntentID／order ID、耗時、錯誤類別。敏感欄位一律遮罩。

至少有：

- 各 venue RPC 成功／失敗／延遲、限流及 token refresh。
- WS 重連、pending RPC、各 queue 深度、book gaps。
- 私人資料的 Recovering／Ready／Degraded。
- 重複成交、未知送單結果、reconcile 差異及最後成功時間。
- FIX session、sequence anomaly、reject、Logon 與 order-flow 驗證狀態。

queue、goroutine、pending map、recovery buffer 都有上限。私人資料不可靜默丟棄；發生 overload 時標記該 venue 不可靠並恢復。

RPC round-trip 用本機 monotonic clock。exchange timestamp 到本機的差只能標記為估計事件年齡，不能在未校時情況稱為精確網路延遲。

SIGINT／SIGTERM：停止新意圖 → 對不明的在途寫入持久化狀態 → 依明確設定取消本程式訂單 → 寫入狀態／游標 → 正常關閉連線，整體有 timeout。取消或 flush 失敗不可回報「已清空所有訂單」。

---

## 16. 開發階段與閘門

| Phase | 工作 | 通過條件 |
|---|---|---|
| 0 | 盤點、基線、回歸測試 | audit 完成，已知失敗與本次新增失敗可區分 |
| 1 | venue routing、複合鍵、設定、必要資料遷移 | Bybit 不回歸；Deribit 未啟用不影響 Bybit |
| 2 | Deribit metadata、HTTP RPC、auth、帳戶查詢 | 精度／權限／token／限流測試通過；讀取 smoke test |
| 3 | 公開 WS、book、心跳、私人 WS | 缺口、重連、事件分流及多筆訊息通過 |
| 4 | HTTP + WS 訂單、意圖、去重 | 限價／改單／取消與未知結果測試；受控外部驗證 |
| 5 | 持久化、重啟、歷史分頁、恢復 | 斷線／索引延遲／分頁重複／同毫秒多筆可恢復 |
| 6 | 多所同跑、CLI／既有 UI | 隔離與唯讀彙總正確，輸出單位及 freshness |
| 7 | R1 驗收 | 核心測試綠燈、外部證據或明列阻擋項 |
| 8 | Deribit FIX dialect／mock | 驗證、session、訊息與單位測試通過 |
| 9 | FIX Testnet 驗證 | 能連則完成；不能則記錄具體阻擋，禁止偽造成功 |
| 10 | 最終文件與交接 | build、demo、能力矩陣、已知限制與完整結果 |

每個 Phase 結束更新 status：改動檔案、測試命令、結果、失敗、偏離規格原因、下一步。沒有外部權限不應阻止離線階段繼續，也不應使外部驗收自動算通過。

---

## 17. 必需測試矩陣

CI 不使用外部憑證；外部 Testnet 測試獨立 opt-in。

| ID | 情境 | 必須得到的結果 |
|---|---|---|
| B01 | Deribit 不啟用／缺 key | Bybit 舊命令與測試仍可用 |
| B02 | 原 Spot／費用案例回歸 | 成交量、淨餘額及費用幣別不混淆 |
| B03 | 舊 JSON／DB 資料升級 | 備份、可重跑、無重複、可回復 |
| N01 | 同號 order ID 來自兩家 | 不覆蓋、不跨所取消 |
| N02 | 同商品但不同帳戶／category | 資料與部位完整隔離 |
| A01 | token 到期、同時多筆請求 | 一次續期、原子更新、沒有 auth storm |
| A02 | auth／refresh／FIX 回應進 log | secret、token、password 不出現 |
| A03 | 錯環境、redirect、惡意 hostname | 在送出憑證前拒絕 |
| R01 | HTTP 200 + RPC error | 業務失敗，不是假成功 |
| R02 | WS 回應倒序、夾雜 notification | 正確對應 pending request |
| R03 | 舊 connection 遲到回應 | 不能完成新 connection request |
| R04 | private order event 先於 ACK | 只建立一張本地訂單，之後合併 |
| M01 | amount USD 與 BTC 混用 | 本地拒絕，不送交易所 |
| M02 | 分段 tick／非法步進 | 正確判定，禁止默默放大數量 |
| M03 | 欄位是高精度 JSON number | 無 float64 捨入造成的金額差異 |
| M04 | 未支援合約／expired metadata | 可查詢但不能交易 |
| W01 | heartbeat test_request | 及時呼叫 public/test |
| W02 | snapshot + delta + duplicate | book 正確且不重複套用 |
| W03 | prev_change_id 不連續 | book stale，等新 snapshot 才恢復 |
| W04 | 私人 queue／recovery buffer 滿 | 該 venue degraded，不靜默丟資料 |
| O01 | 同一 IntentID 兩程序送出 | 至多一個獲准進入寫入流程 |
| O02 | 交易所接受後 socket 斷線 | OutcomeUnknown，不自動重送 |
| O03 | 同 label 查到多張單 | NeedsReview，不選第一張 |
| O04 | RPC 回應與 WS 含同一成交 | 成交與費用只入帳一次 |
| O05 | Partial fill 後取消 | Cancelled + 保留已成交量 |
| O06 | Fill 與 cancel／amend 競爭 | 保留交易所最後可靠狀態，無歸零 |
| O07 | 改單失敗 | 原訂單不被改成 Rejected |
| C01 | 已下單但未持久化 ACK 即 crash | 重啟由持久化意圖核對，不再建第二單 |
| C02 | 分頁有重複、同毫秒多筆 | 不漏成交、不重複計算 |
| C03 | 近期已移除、歷史尚未索引 | 保持未知／部分，重試有界 |
| C04 | 停機期間訂單由外部取消 | 重啟查詢修復本地狀態 |
| C05 | recovery 途中又有成交／重連 | 不以舊 snapshot 覆蓋新可靠資料 |
| C06 | 重複執行 reconcile | 相同結果，沒有額外交易 |
| D01 | WS COD 開啟、HTTP 訂單存在 | 不假設 HTTP 訂單也會被取消 |
| D02 | graceful logout／真正斷線 | 區分政策與實際取消結果 |
| V01 | Bybit 網路／驗證失敗 | Deribit 不崩潰且狀態獨立 |
| V02 | Deribit 限流重連 | 不連帶重連 Bybit、不形成登入風暴 |
| V03 | 任一家資料不完整 | portfolio 顯示 Partial，不當成零 |
| V04 | BTC、USD、USDT 原生值 | 不直接相加；換算來源不足就不總計 |
| F01 | Deribit FIX Logon fixture | digest、timestamp、nonce、tags 正確 |
| F02 | Bybit／Deribit FIX 同時測試 | dialect 不互相污染 |
| F03 | 拆包／黏包／checksum／groups | 可解析或明確拒絕，不 panic／漏填單 |
| F04 | ResendRequest／SequenceReset | 依 Deribit 政策處理，不誤用 Bybit |
| F05 | ExecReport tag11 被替換 | 仍能正確關聯本地意圖與 tag37 |
| F06 | JSON／FIX 數量換算不明 | 禁止 live FIX order，標記阻擋 |
| F07 | FIX 成交與 JSON 核對 | ID 已證實才去重，不以價格時間猜 |
| S01 | SIGTERM 有在途訂單 | 落盤未知狀態、無無限等待 |
| S02 | Mainnet／提款／all venue 寫入 | 明確拒絕 |

對有支援的環境執行 `go test -race ./...`。對 FIX framing／RPC decode 增加 fuzz seed 與有界 fuzz 測試；不得以測試過程開無限網路連線。

---

## 18. 外部 Testnet 驗證與證據

### 18.1 開關分離

建議沿用既有整合測試慣例，新增等價的分離開關：

```dotenv
RUN_DERIBIT_READ_TESTS=0
RUN_DERIBIT_TRADING_TESTS=0
RUN_DERIBIT_FIX_TESTS=0
```

read 開關不能下單；FIX 開關不自動代表允許交易，寫入仍需 trading 開關及明確確認。新憑證由使用者自行配置到本機環境，本文件不提供任何值。

### 18.2 R1 外部驗證

1. 商品 metadata、時間、token、帳戶摘要與 positions 讀取成功。
2. 公開 WS 與私人 WS 同時運作；證明心跳及重訂閱。
3. HTTP 建立一張 metadata 驗證的小額限價單，取得私人事件，改單／取消並查詢。
4. WS 另外建立一張新意圖，完成相同生命週期。
5. 在設定的測試風險與價格上限內觀察至少一筆真實 Testnet execution，與 order／trade history／本地帳務比對。
6. 停機期間由另一個明確操作改變訂單，重啟並修復。
7. Bybit 與 Deribit 同時工作，注入單所故障，另一所持續可用。

測試環境流動性不足時，第 5 項可保持 `BLOCKED_LIQUIDITY`；mock fill 測試仍必需，但不能拿 mock 代替真實 execution 證據。不為了得到成交去取消風控、繞過自成交保護、放大面額、改 Mainnet 或無上限追價。

測試清理只碰本次明確擁有的訂單。若意外形成部位，先報告；有事先授權的受限 reduce-only 清理才可執行，不能全帳戶平倉。

### 18.3 證據格式

建立 `docs/upgrade/VALIDATION_REPORT.md`，每一項記錄：

```text
Test ID / UTC time / commit / build
Venue / Testnet / account alias / transport
Instrument / amount + unit / metadata timestamp
Command / expected result / actual result
Native order ID / trade ID（分享前可遮罩）
Result: PASS | FAIL | BLOCKED | NOT_RUN
Evidence: sanitized log / fixture / screenshot（若已有 UI）
```

不得在報告附完整 auth frame、token 或帳戶個資。

### 18.4 完成定義

**R1 程式完成：** 必需離線測試與 Bybit 回歸通過；HTTP、WS、意圖／恢復／多所隔離皆有實作，文件與 build 可用。

**R1 外部驗證完成：** 上述核心 Testnet 驗證有真實證據。某項 BLOCKED 時，寫「程式完成、外部驗證未完成」，不能簡稱全部完成。

**R2 FIX 外部整合完成：** 不只 TCP／TLS 成功；必須有真實 Logon 與訂單／回報流程，並可用 JSON 查詢核對。

---

## 19. 交付物

Codex 最後交付：

```text
現有專案中的增量程式與測試
可執行的 build 指令及 bin 路徑
更新後的 README.md
更新後的 .env.example / .gitignore
更新後的 IMPLEMENTATION_STATUS.md

docs/upgrade/BASELINE_AUDIT.md
docs/upgrade/PROTOCOL_NOTES.md
docs/upgrade/MIGRATION.md
docs/upgrade/DEMO.md
docs/upgrade/VALIDATION_REPORT.md
docs/upgrade/CAPABILITY_MATRIX.md
docs/upgrade/CODEX_HANDOFF.md
```

`CODEX_HANDOFF.md` 用繁體中文說明：改了什麼、怎麼 build／啟動、Bybit 是否保留、Deribit 哪些真測過、FIX 到哪一層、阻擋項與下一步。不得把模板內所有核取方塊自動勾滿。

**期待最終專案能證明的是：兩個真實 Testnet 的連線與訂單生命週期，以及可解釋、可測試的故障恢復。不是接了兩個 URL 就稱為多交易所交易平台。**

---

## 20. 官方資料來源與核對規則

以下來源於 2026-09-09 查閱。實作時再次核對對應 method，保存核對日期、現行／upcoming 分支與去識別化回應證據。只用官方文件確認協定；範例數值不是永久商品規格。

- **[R1]** Deribit Quickstart：介面、獨立 Testnet、端點及範例。  
  https://docs.deribit.com/articles/deribit-quickstart
- **[R2]** Deribit JSON-RPC 協定：HTTP／WS、envelope、transport 限制。  
  https://docs.deribit.com/articles/json-rpc-overview
- **[R3]** Authentication／public auth：token、scope 與續期。  
  https://docs.deribit.com/articles/authentication  
  https://docs.deribit.com/api-reference/authentication/public-auth
- **[R4]** Buy：amount 語意、回應 order／trades、訂單選項。  
  https://docs.deribit.com/api-reference/trading/private-buy
- **[R5]** 商品 metadata。  
  https://docs.deribit.com/api-reference/market-data/public-get_instruments  
  https://docs.deribit.com/api-reference/market-data/public-get_instrument
- **[R6]** Connection management：scope、心跳與生命週期。  
  https://docs.deribit.com/articles/connection-management-best-practices
- **[R7]** Rate limits：credit model、10028 與帳戶額度。  
  https://docs.deribit.com/articles/rate-limits
- **[R8]** Orderbook snapshot／delta／change ID。  
  https://docs.deribit.com/subscriptions/orderbook/bookinstrument_nameinterval
- **[R9]** WebSocket heartbeat／test_request。  
  https://docs.deribit.com/api-reference/session-management/public-set_heartbeat
- **[R10]** Private changes：orders／trades／positions。  
  https://docs.deribit.com/subscriptions/user/userchangesinstrument_nameinterval
- **[R11]** 依 label 查多筆近期訂單。  
  https://docs.deribit.com/api-reference/trading/private-get_order_state_by_label
- **[R12]** Edit by label：只在剛好一張開放訂單時適用。  
  https://docs.deribit.com/api-reference/trading/private-edit_by_label
- **[R13]** Cancel on Disconnect 設定與作用範圍。  
  https://docs.deribit.com/api-reference/session-management/private-enable_cancel_on_disconnect  
  https://docs.deribit.com/api-reference/session-management/private-get_cancel_on_disconnect
- **[R14]** 訂單歷史、未成交取消單與分頁。  
  https://docs.deribit.com/api-reference/trading/private-get_order_history_by_currency
- **[R15]** 成交依商品查詢及分頁。  
  https://docs.deribit.com/api-reference/trading/private-get_user_trades_by_instrument
- **[R16]** 近期／歷史查詢、索引延遲。  
  https://docs.deribit.com/articles/accessing-historical-trades-orders
- **[R17]** Derivatives positions。  
  https://docs.deribit.com/api-reference/account-management/private-get_positions
- **[R18]** Account summary。  
  https://docs.deribit.com/api-reference/account-management/private-get_account_summary
- **[R19]** 現行 FIX overview、Testnet TLS 與標頭。  
  https://docs.deribit.com/fix-api/production/overview
- **[R20]** Deribit FIX Logon、認證與選項。  
  https://docs.deribit.com/fix-api/production/logon
- **[R21]** Deribit FIX Resend Request。  
  https://docs.deribit.com/fix-api/production/resend-request
- **[R22]** Deribit FIX Sequence Reset。  
  https://docs.deribit.com/fix-api/production/sequence-reset
- **[R23]** Deribit FIX New Order Single。  
  https://docs.deribit.com/fix-api/production/new-order-single
- **[R24]** Deribit FIX Cancel／Replace。  
  https://docs.deribit.com/fix-api/production/order-cancel-request  
  https://docs.deribit.com/fix-api/production/order-cancel-replace
- **[R25]** Deribit FIX Execution Reports、ID 及數量欄位。  
  https://docs.deribit.com/fix-api/production/execution-reports
- **[R26]** Currency 參數與 spot／derivatives 的帳戶差異。  
  https://docs.deribit.com/articles/currency-parameter
- **[R27]** Bybit 下單 ACK、orderLinkId 及商品參數。  
  https://bybit-exchange.github.io/docs/v5/order/create-order
- **[R28]** Bybit execution／fees。  
  https://bybit-exchange.github.io/docs/v5/websocket/private/execution
- **[R29]** Deribit cancel。  
  https://docs.deribit.com/api-reference/trading/private-cancel
- **[R30]** Deribit edit。  
  https://docs.deribit.com/api-reference/trading/private-edit
- **[R31]** Deribit open orders by instrument。  
  https://docs.deribit.com/api-reference/trading/private-get_open_orders_by_instrument
- **[R32]** Deribit trades by order。  
  https://docs.deribit.com/api-reference/trading/private-get_user_trades_by_order
- **[R33]** Deribit ticker。  
  https://docs.deribit.com/api-reference/market-data/public-ticker
- **[R34]** Bybit FIX，僅作差異核對，不套用到 Deribit。  
  https://bybit-exchange.github.io/docs/fix-api/guide

---

## 附錄：交給 Codex 的起始提示

```text
請先閱讀本儲存庫的 AGENTS.md、README、現有規格、交接文件，以及
DERIBIT_MULTI_VENUE_UPGRADE_SPEC_V2.md。

這是已有 Bybit 程式的增量改版，請勿重新開一個不相干的專案。
先完成 Phase 0 基線盤點，確認真實功能與測試狀況，再依序加入 Deribit。
保留 Bybit 指令、設定、儲存資料和既有 dashboard（若有）。

先完成 Deribit HTTP JSON-RPC + WebSocket、訂單意圖／去重／恢復、
多交易所隔離及唯讀彙總，再接續 Deribit FIX dialect 與測試。
不要把 Deribit label 當成交易所冪等鍵，不要混用 USD 面額與 BTC 數量，
不要直接套用 Bybit 的 FIX 認證與恢復規則。

所有外部連線只用 Testnet；不使用曾貼在對話中的舊 key；
沒有新憑證時繼續本機實作和測試，將外部驗證標記 BLOCKED。
沒有明確授權及測試開關不得下單，不可自動跨 transport 或跨交易所重送。

每個階段執行測試並更新 IMPLEMENTATION_STATUS.md。
最後交付可執行的 build／啟動方式、增量程式、測試、驗證報告、能力矩陣，
並用繁體中文提供 CODEX_HANDOFF.md，清楚區分 mock 通過與真實 Testnet 通過。
```
