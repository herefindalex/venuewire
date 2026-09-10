# V3 部署附註：既有 Nginx SSL → 另一台 Go

本文件是 V3.1 split-host 部署參考。2026-09-10 已由操作者完成 Nginx 調整／reload，並由公開 HTTPS/WSS Browser flow 驗證；本程序未直接檢查 privileged Nginx／防火牆內容。正式驗收以 `../TRADING_CONSOLE_V3_CHANGE_SPEC.md` 為準。

V3.1 同時適用 `../VENUEWIRE_V3_1_PUBLIC_DEMO_SUPPLEMENT.md` §26：共用交易紀錄、全站滾動每小時與並行限額、`Unknown` 保留名額及重啟恢復。設定範例以根目錄 `.env.example` 為 canonical；`../.env.example` 保持完全相同。沿用 `WEB_TRUSTED_PROXY_CIDRS`、`QUICK_TRADE_SLIPPAGE_BPS`，並使用 `QUOTE_TTL=5s`。

## 1. 需要填入的值

| 項目 | 範例 | 實際意義 |
|---|---|---|
| 公開 origin | `https://trade.example.com` | Browser 登入、API、WSS 都使用它 |
| Nginx 來源位址 | `10.0.0.10` | Go 實際看到的 proxy 來源；有 NAT 時需核對 |
| Go 綁定位址 | `10.0.0.20` | Go 主機自己的 private interface，不是 loopback |
| Go port | `8080` | ACL 僅允許 Nginx，不做 Internet port-forward |

兩台之間只在可信受控 LAN／VLAN 使用 HTTP。經不可信網路時先建立加密私有隧道，外部 SSL 不會加密 Nginx→Go 這段。

## 2. 後端 `.env`

將隨附 `.env.example` 的新設定合併到原設定，不覆寫原本 Bybit／Deribit keys、account aliases、state 或 FIX 設定。所有秘密由使用者在 Go 主機填入。

`WEB_SESSION_SECRET` 可在自己的主機產生：

```bash
openssl rand -base64 32
```

將結果直接保存於受限權限的 dotenv 檔，不提交 Git、不貼對話。建議服務帳號專用、檔案 mode 600，並核對 owner。`WEB_PASSWORD` 至少 12 字元，特殊字元依程式選用的 dotenv parser 正確引用。

啟動指令由 Codex 配合實際 CLI 提供，例如：

```bash
./bin/venuewire --env-file /etc/trading-console/console.env web
```

程式自行讀檔，不需 `source`。若 systemd 的 WorkingDirectory 不同，使用絕對 `--env-file` 路徑。沒有重複的 EnvironmentFile 注入，就較不容易出現「改了 `.env` 卻被既有 OS env 蓋掉」的混淆。

## 3. Nginx 三個範例檔

`nginx-http-map.conf` 放在 `http {}` scope；例如環境原本已在 http 中 include `conf.d/*.conf`，可由部署人員放進該目錄。不能 include 在 server 中。

`nginx-proxy-common.conf` 建議保存為 `/etc/nginx/snippets/trading-console-proxy-common.conf`，先修改公開 Host。

`nginx-https-locations.conf` 的內容合併到既有 HTTPS server；修改 Go IP 及 common include 路徑。不得直接複製出第二份衝突的 `location /` 或 `server_name`。原有 HTTP→HTTPS redirect、憑證續期／ACME 與 default-vhost 防護照舊保留。

此範例假設單一 edge Nginx。若現有設定啟用了 real_ip，要核對它只信任真正的前置代理，不能讓任意使用者用 header 改寫 `$remote_addr`。

部署人員確認變更後執行：

```bash
sudo nginx -t
# 只有上面通過且已核對變更，才 reload。
sudo systemctl reload nginx
```

Codex 不得自行操作使用者 Nginx 或防火牆。

## 4. Go 主機 ACL

規則語意：允許「實際 Nginx 來源 → Go private IP:8080/TCP」，拒絕其他來源。先檢查現有防火牆規則順序、IPv4／IPv6、容器映射及管理通道；不在未知主機上自動執行會影響 SSH 的規則。

只綁 private IP 不等於只有 Nginx 能連。ACL、Go trusted proxy check、應用 session／CSRF 三層都要有；任何一層不能取代另一層。

## 5. 展示模式與交易模式

```dotenv
# 所有登入者只能看資產／報價，不能送單。
WEB_TRADING_ENABLED=false
```

```dotenv
# 所有使用同一帳密登入者皆能做被允許的 Testnet Spot 交易。
WEB_TRADING_ENABLED=true
```

本版只有一個固定登入帳號，沒有「Alex 可交易、訪客只能看」的角色分離。改設定後重啟；後端先恢復 pending intents 才重新開放交易。

## 6. 部署檢查

- 外部只看到 HTTPS origin，API 使用相對路徑，WebSocket 是 WSS。
- 登入 cookie 帶 `Secure / HttpOnly / SameSite=Strict / Path=/`，不帶 Domain。
- 未登入 `/api/venues/.../account` 回 401；未登入 WS 無法升級。
- 成功登入後 `/api/ws` 回 101，超過一般 idle timeout 仍可依心跳維持。
- logout／session 到期後舊 WS 被關閉；重新登入可查看舊的交易意圖。
- 偽造 XFF／Origin／Host 不能繞過 limiter 或寫入防護。
- `.env`／`.ENV`／`.git`／state／logs 不可下載。
- Go port 不接受非允許主機；Nginx request 不被 cache 或 upstream 重送。
- 一筆授權的 Testnet 交易完成後，Modal 與背景 balance 一致更新；不能只驗證 HTTP 200。

## 7. 回復

回復 UI／binary 前先停止接受新的 Web intent，保存並核對未完成交易。依 schema migration 的備份／相容性程序回復，不把舊 binary 指向不認識的資料格式。任何 outcome unknown 先保持可追蹤，不能因回版把 state 清空。

目前部署已驗證 HTTPS/WSS、服務及小額 Testnet 交易回報；實際 IP、ACL、憑證或帳戶變更後仍須在部署環境重新驗證。
