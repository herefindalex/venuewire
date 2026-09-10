# V3 Codex 入口

正式規格：`TRADING_CONSOLE_V3_CHANGE_SPEC.md`。

這是既有 Bybit + Deribit 程式的增量變更，不是新專案。先閱讀儲存庫的 `AGENTS.md`、README、V2 規格及交接文件，再做 Phase 0。

```text
請依 TRADING_CONSOLE_V3_CHANGE_SPEC.md 與 VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md 開始實作。
先讀 CODEX_START_V3_1.md；衝突處以補充文件 §26 已確認決策為準。

保留既有 adapters、CLI、FIX、訂單追蹤、成交去重、資料與測試。
加入 dotenv、env 固定帳密登入、Vue Web、即時帳戶／估值、
四個方向的 Spot Quick Trade Modal、Review/Confirm、Limit IOC 0.5% 保護。

Nginx 已處理 SSL，並與 Go 位於不同機器；Go 綁 private IP，
不提供自己的 TLS，只信任指定 Nginx，實作 Secure session/CSRF/WSS。

依 Phase 0–7 執行，逐階段測試並更新 IMPLEMENTATION_STATUS.md。
沒有憑證或部署環境時完成本機工作，把外部驗證標 BLOCKED/NOT_RUN。
未經另外明確授權不得執行 Testnet 下單、改 Nginx／防火牆或改帳戶設定。

最後交付實際 build/start 指令、測試／部署文件、
TEST_REPORT_V3.md 與繁體中文 CODEX_HANDOFF_V3.md。
```

## 套件內容

| 檔案 | 用途 |
|---|---|
| `TRADING_CONSOLE_V3_CHANGE_SPEC.md` | 唯一正式 V3 增量規格，包含流程、契約、限制與驗收 |
| `.env.example` | 新設定範例；秘密為空，不覆蓋原檔 |
| `deploy/nginx-http-map.conf` | http scope 的 WebSocket Connection map |
| `deploy/nginx-proxy-common.conf` | 各 location 共用 headers／timeout／no-retry 設定 |
| `deploy/nginx-https-locations.conf` | 合併到現有 SSL server 的 location 範例 |
| `deploy/DEPLOYMENT_NOTES.md` | 跨機器位址、ACL、帳密與展示模式說明 |
| `reference/account-balance-reference.png` | 使用者提供的資產頁參考；不能當真實 API 資料或宣稱已實作 |

尚未檢視實際儲存庫，本套件沒有修改／執行使用者程式，也沒有登入或交易。
