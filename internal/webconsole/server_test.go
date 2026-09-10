package webconsole

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"venuewire/internal/config"
	"venuewire/internal/domain"
	"venuewire/internal/intent"
	"venuewire/internal/quicktrade"
	"venuewire/internal/runtimeevent"
)

func TestAuthenticationSessionCSRFAndLogout(t *testing.T) {
	server := newTestServer(t)

	unauthorized := performRequest(server, http.MethodGet, "/api/auth/me", "", nil, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	var publicErr publicError
	decodeBody(t, unauthorized, &publicErr)
	if publicErr.RequestID == "" || strings.Contains(unauthorized.Body.String(), "internal/") {
		t.Fatalf("unsafe or uncorrelated error: %s", unauthorized.Body.String())
	}

	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	var loginBody struct {
		CSRFToken string `json:"csrfToken"`
		ReadOnly  bool   `json:"readOnly"`
	}
	decodeBody(t, login, &loginBody)
	if loginBody.CSRFToken == "" || !loginBody.ReadOnly {
		t.Fatalf("unexpected login body: %+v", loginBody)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" || cookies[0].Domain != "" {
		t.Fatalf("unsafe session cookie: %+v", cookies)
	}

	me := performRequest(server, http.MethodGet, "/api/auth/me", "", cookies[0], "")
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", me.Code, me.Body.String())
	}

	badLogout := performRequest(server, http.MethodPost, "/api/auth/logout", "", cookies[0], "wrong")
	if badLogout.Code != http.StatusForbidden {
		t.Fatalf("bad logout status = %d", badLogout.Code)
	}
	logout := performRequest(server, http.MethodPost, "/api/auth/logout", "", cookies[0], loginBody.CSRFToken)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d body=%s", logout.Code, logout.Body.String())
	}
	if got := performRequest(server, http.MethodGet, "/api/auth/me", "", cookies[0], ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d", got.Code)
	}
}

func TestLoginUsesGenericFailureAndRateLimit(t *testing.T) {
	server := newTestServer(t)
	var firstError publicError
	for i := 0; i < 5; i++ {
		body := `{"username":"alex","password":"wrong-password"}`
		if i%2 == 0 {
			body = `{"username":"unknown","password":"fixture-password"}`
		}
		response := performRequest(server, http.MethodPost, "/api/auth/login", body, nil, "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status/body = %d %s", i, response.Code, response.Body.String())
		}
		var failure publicError
		decodeBody(t, response, &failure)
		if i == 0 {
			firstError = failure
		} else if failure.Error != firstError.Error || failure.Message != firstError.Message {
			t.Fatalf("credential failures differ: first=%+v current=%+v", firstError, failure)
		}
	}
	limited := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit status/header = %d %q", limited.Code, limited.Header().Get("Retry-After"))
	}
}

func TestProxyHostOriginAndContentTypeAreEnforced(t *testing.T) {
	server := newTestServer(t)
	tests := []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"untrusted peer", func(r *http.Request) { r.RemoteAddr = "10.0.0.99:4000" }, http.StatusForbidden},
		{"wrong host", func(r *http.Request) { r.Host = "attacker.example" }, http.StatusForbidden},
		{"spoofed chain", func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1") }, http.StatusForbidden},
		{"wrong origin", func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") }, http.StatusForbidden},
		{"wrong content type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, http.StatusUnsupportedMediaType},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest(http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`)
			tc.mutate(r)
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d body=%s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func TestSessionAbsoluteExpiration(t *testing.T) {
	server := newTestServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	server.now = func() time.Time { return now }
	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login failed: %s", login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	now = now.Add(server.config.SessionTTL)
	if response := performRequest(server, http.MethodGet, "/api/auth/me", "", cookie, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d", response.Code)
	}
}

func TestVenuesExposeNoCredentials(t *testing.T) {
	server := newTestServer(t)
	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	cookie := login.Result().Cookies()[0]
	response := performRequest(server, http.MethodGet, "/api/venues", "", cookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, secret := range []string{"fixture-api-key", "fixture-api-secret", "fixture-password"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response exposes secret: %s", body)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("missing security headers: %v", response.Header())
	}
}

func TestBrowserWebSocketRequiresSessionAndClosesOnLogout(t *testing.T) {
	server := newTestServer(t)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/ws"

	baseHeaders := http.Header{
		"Host":              []string{"trade.example.com"},
		"Origin":            []string{"https://trade.example.com"},
		"X-Forwarded-Proto": []string{"https"},
		"X-Forwarded-For":   []string{"203.0.113.20"},
		"X-Real-IP":         []string{"203.0.113.20"},
	}
	if conn, response, err := websocket.DefaultDialer.Dial(wsURL, baseHeaders); err == nil {
		_ = conn.Close()
		t.Fatal("unauthenticated WebSocket connected")
	} else if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated WebSocket response = %+v err=%v", response, err)
	}

	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	cookie := login.Result().Cookies()[0]
	var loginBody struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeBody(t, login, &loginBody)
	headers := baseHeaders.Clone()
	headers.Set("Cookie", cookie.String())
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("authenticated WebSocket failed: response=%+v err=%v", response, err)
	}
	defer conn.Close()
	var ready map[string]any
	if err := conn.ReadJSON(&ready); err != nil || ready["type"] != "session.ready" {
		t.Fatalf("ready event = %#v err=%v", ready, err)
	}
	var snapshot map[string]any
	if err := conn.ReadJSON(&snapshot); err != nil || snapshot["type"] != "snapshot" || snapshot["seq"] != float64(1) {
		t.Fatalf("snapshot event = %#v err=%v", snapshot, err)
	}
	if snapshot["instanceId"] == "" || snapshot["instanceId"] != ready["instanceId"] {
		t.Fatalf("snapshot instance = %#v, ready instance = %#v", snapshot["instanceId"], ready["instanceId"])
	}

	server.events.Publish(runtimeevent.Event{Type: "venue.health.updated", Venue: domain.VenueBybit, At: time.Now()})
	var update map[string]any
	if err := conn.ReadJSON(&update); err != nil || update["type"] != "venue.health.updated" || update["seq"] != float64(2) {
		t.Fatalf("runtime event = %#v err=%v", update, err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "resync"}); err != nil {
		t.Fatal(err)
	}
	var resnapshot map[string]any
	if err := conn.ReadJSON(&resnapshot); err != nil || resnapshot["type"] != "snapshot" || resnapshot["seq"] != float64(3) {
		t.Fatalf("resync snapshot = %#v err=%v", resnapshot, err)
	}

	logout := performRequest(server, http.MethodPost, "/api/auth/logout", "", cookie, loginBody.CSRFToken)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logout.Code)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("WebSocket remained open after logout")
	}
}

func TestBrowserWebSocketGlobalConnectionLimit(t *testing.T) {
	store := &sessionStore{
		sessions:      map[string]*session{"session": {ID: "session", conns: make(map[*websocket.Conn]struct{})}},
		maxTotalConns: 2,
	}
	first, second, third := &websocket.Conn{}, &websocket.Conn{}, &websocket.Conn{}
	if !store.addConn("session", first, 5) || !store.addConn("session", second, 5) {
		t.Fatal("connections within the global limit were rejected")
	}
	if store.addConn("session", third, 5) {
		t.Fatal("connection over the global limit was accepted")
	}
	store.removeConn("session", first)
	if !store.addConn("session", third, 5) {
		t.Fatal("connection was not accepted after capacity was released")
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	_, proxy, err := net.ParseCIDR("127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	webCfg := config.WebConfig{
		Host:                "127.0.0.1",
		Port:                8080,
		PublicOrigin:        "https://trade.example.com",
		TrustedProxyCIDRs:   []*net.IPNet{proxy},
		Username:            "alex",
		Password:            "fixture-password",
		SessionSecret:       bytes.Repeat([]byte{7}, 32),
		SessionTTL:          8 * time.Hour,
		DefaultVenue:        domain.VenueBybit,
		BybitEnabled:        true,
		TradingEnabled:      false,
		MaxTradesPerSession: 10,
		MaxTradesPerHour:    30,
		MaxConcurrentTrades: 1,
	}
	base := config.Config{AccountAlias: "bybit-test", APIKey: "fixture-api-key", APISecret: "fixture-api-secret"}
	return New(webCfg, base, nil)
}

func performRequest(server *Server, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := validRequest(method, path, body)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	return w
}

func validRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "https://trade.example.com"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:4000"
	r.Host = "trade.example.com"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-For", "203.0.113.20")
	r.Header.Set("X-Real-IP", "203.0.113.20")
	r.Header.Set("Origin", "https://trade.example.com")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func decodeBody(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func TestSessionCookieSignatureCannotBeForged(t *testing.T) {
	server := newTestServer(t)
	forged := &http.Cookie{Name: sessionCookieName, Value: base64.RawURLEncoding.EncodeToString([]byte("unknown")) + ".invalid"}
	response := performRequest(server, http.MethodGet, "/api/auth/me", "", forged, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("forged cookie status = %d", response.Code)
	}
}

func TestFrontendServesEmbeddedIndexAndDoesNotMaskAPINotFound(t *testing.T) {
	server := newTestServer(t)
	server.assets = fstest.MapFS{
		"index.html":    {Data: []byte("<html>VenueWire</html>")},
		"assets/app.js": {Data: []byte("console.log('VenueWire')")},
	}
	index := performRequest(server, http.MethodGet, "/", "", nil, "")
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "VenueWire") || index.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index status/body/cache = %d %s %q", index.Code, index.Body.String(), index.Header().Get("Cache-Control"))
	}
	spa := performRequest(server, http.MethodGet, "/trades/trade-1", "", nil, "")
	if spa.Code != http.StatusOK || !strings.Contains(spa.Body.String(), "VenueWire") {
		t.Fatalf("SPA fallback = %d %s", spa.Code, spa.Body.String())
	}
	api := performRequest(server, http.MethodGet, "/api/not-implemented", "", nil, "")
	if api.Code != http.StatusNotFound || strings.Contains(api.Body.String(), "<html>") {
		t.Fatalf("API fallback = %d %s", api.Code, api.Body.String())
	}
}

func TestRecentTradesAreSharedWithoutSessionIdentifiers(t *testing.T) {
	server := newTestServer(t)
	server.trades = intent.Store{Path: t.TempDir() + "/intents.json"}
	now := time.Now().UTC()
	confirmation := webTradeConfirmation(now)
	if _, _, err := server.trades.ConfirmQuickTrade(t.Context(), confirmation, intent.DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 30, MaxConcurrentTrades: 1}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := server.trades.MarkQuickTradeDispatching(t.Context(), confirmation.IntentID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := server.trades.UpdateQuickTrade(t.Context(), confirmation.IntentID, intent.QuickTradeUpdate{Status: intent.TradeUnknown}, now); err != nil {
		t.Fatal(err)
	}
	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	response := performRequest(server, http.MethodGet, "/api/trades", "", login.Result().Cookies()[0], "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), confirmation.SessionID) || strings.Contains(response.Body.String(), confirmation.Identity) {
		t.Fatalf("public trade response exposes internal identity: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), confirmation.IntentID) || !strings.Contains(response.Body.String(), "bybit-usdt-btc") {
		t.Fatalf("public trade missing lifecycle identity: %s", response.Body.String())
	}
}

func TestTradeRecheckRequiresCSRFAndIsRateLimited(t *testing.T) {
	server := newTestServer(t)
	server.trades = intent.Store{Path: t.TempDir() + "/intents.json"}
	now := time.Now().UTC()
	server.now = func() time.Time { return now }
	confirmation := webTradeConfirmation(now)
	if _, _, err := server.trades.ConfirmQuickTrade(t.Context(), confirmation, intent.DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 30, MaxConcurrentTrades: 1}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := server.trades.MarkQuickTradeDispatching(t.Context(), confirmation.IntentID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := server.trades.UpdateQuickTrade(t.Context(), confirmation.IntentID, intent.QuickTradeUpdate{Status: intent.TradeUnknown}, now); err != nil {
		t.Fatal(err)
	}
	server.rechecker = fakeRechecker{store: server.trades}
	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	cookie := login.Result().Cookies()[0]
	var sessionView struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeBody(t, login, &sessionView)
	path := "/api/trades/" + confirmation.IntentID + "/recheck"
	if response := performRequest(server, http.MethodPost, path, "", cookie, "wrong"); response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", response.Code)
	}
	response := performRequest(server, http.MethodPost, path, "", cookie, sessionView.CSRFToken)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"Filled"`) {
		t.Fatalf("recheck status/body = %d %s", response.Code, response.Body.String())
	}
	limited := performRequest(server, http.MethodPost, path, "", cookie, sessionView.CSRFToken)
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("second recheck = %d retry=%q", limited.Code, limited.Header().Get("Retry-After"))
	}
}

func TestQuickTradeAPIUsesServerIdentityAndHidesPrivateSession(t *testing.T) {
	server := newTestServer(t)
	server.config.TradingEnabled = true
	application := &stubTradeApplication{}
	server.tradeApp = application
	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	cookie := login.Result().Cookies()[0]
	var sessionView struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeBody(t, login, &sessionView)

	quote := performRequest(server, http.MethodPost, "/api/venues/bybit/quotes", `{"routeId":"bybit-usdt-btc","amount":"100"}`, cookie, sessionView.CSRFToken)
	if quote.Code != http.StatusCreated || strings.Contains(quote.Body.String(), "alex") || strings.Contains(quote.Body.String(), "private-session") {
		t.Fatalf("quote status/body = %d %s", quote.Code, quote.Body.String())
	}
	if application.createRequest.Identity != "alex" || application.createRequest.AccountAlias != "bybit-test" {
		t.Fatalf("server scope not applied: %+v", application.createRequest)
	}

	confirm := performRequest(server, http.MethodPost, "/api/trades/confirm", `{"quoteId":"quote-1","clientRequestId":"browser-request-1"}`, cookie, sessionView.CSRFToken)
	if confirm.Code != http.StatusAccepted || strings.Contains(confirm.Body.String(), application.confirmRequest.SessionID) {
		t.Fatalf("confirm status/body = %d %s", confirm.Code, confirm.Body.String())
	}
	if application.confirmRequest.Identity != "alex" || application.confirmRequest.SessionID == "" || application.confirmRequest.ClientRequestID != "browser-request-1" {
		t.Fatalf("confirmation scope = %+v", application.confirmRequest)
	}
}

func TestTradingDisabledBlocksConfirmBeforeApplication(t *testing.T) {
	server := newTestServer(t)
	application := &stubTradeApplication{}
	server.tradeApp = application
	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	cookie := login.Result().Cookies()[0]
	var sessionView struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeBody(t, login, &sessionView)
	response := performRequest(server, http.MethodPost, "/api/trades/confirm", `{"quoteId":"quote-1","clientRequestId":"browser-request-1"}`, cookie, sessionView.CSRFToken)
	if response.Code != http.StatusForbidden || application.confirmRequest.QuoteID != "" {
		t.Fatalf("disabled confirm status/call = %d %+v", response.Code, application.confirmRequest)
	}
}

type fakeRechecker struct{ store intent.Store }

func (f fakeRechecker) RecheckTrade(ctx context.Context, intentID string, now time.Time) (intent.QuickTrade, error) {
	return f.store.UpdateQuickTrade(ctx, intentID, intent.QuickTradeUpdate{Status: intent.TradeFilled, ResultStatus: "FILLED", Checked: true}, now)
}

type stubTradeApplication struct {
	createRequest  quicktrade.CreateRequest
	confirmRequest quicktrade.ConfirmRequest
}

func (s *stubTradeApplication) CreateQuote(_ context.Context, request quicktrade.CreateRequest) (intent.QuickTradeQuote, error) {
	s.createRequest = request
	now := time.Now().UTC()
	return intent.QuickTradeQuote{ID: "quote-1", Identity: request.Identity, Venue: string(request.Venue), Environment: "testnet", AccountAlias: request.AccountAlias, RouteID: request.RouteID, FromAsset: "USDT", ToAsset: "BTC", SpendBudget: request.SpendBudget, Instrument: "BTCUSDT", Side: "Buy", BaseQty: "0.001", LimitPrice: "100500", TimeInForce: "IOC", CreatedAt: now, ExpiresAt: now.Add(5 * time.Second), Executable: true}, nil
}

func (s *stubTradeApplication) Confirm(_ context.Context, request quicktrade.ConfirmRequest) (quicktrade.ConfirmResult, error) {
	s.confirmRequest = request
	now := time.Now().UTC()
	return quicktrade.ConfirmResult{Created: true, Trade: intent.QuickTrade{ID: "trade-1", Identity: request.Identity, SessionID: request.SessionID, ClientRequestID: request.ClientRequestID, Quote: intent.QuickTradeQuote{ID: request.QuoteID, Venue: "bybit", RouteID: "bybit-usdt-btc", FromAsset: "USDT", ToAsset: "BTC", SpendBudget: "100", Instrument: "BTCUSDT", Side: "Buy", BaseQty: "0.001", LimitPrice: "100500", TimeInForce: "IOC"}, ClientOrderID: "vw-trade-1", Status: intent.TradeAccepted, CreatedAt: now, UpdatedAt: now}}, nil
}

func webTradeConfirmation(now time.Time) intent.QuickTradeConfirmation {
	return intent.QuickTradeConfirmation{
		IntentID: "trade-1", Identity: "shared-demo-user", SessionID: "private-session-id",
		ClientRequestID: "request-1", ClientOrderID: "vw-trade-1",
		Quote: intent.QuickTradeQuote{
			ID: "quote-1", Identity: "shared-demo-user", Venue: "bybit", Environment: "testnet",
			AccountAlias: "bybit-test", RouteID: "bybit-usdt-btc", FromAsset: "USDT", ToAsset: "BTC",
			SpendBudget: "100", Instrument: "BTCUSDT", Side: "Buy", BaseQty: "0.001",
			LimitPrice: "100500", TimeInForce: "IOC", SourceDebitUpperBound: "100",
			CreatedAt: now, ExpiresAt: now.Add(time.Minute), Executable: true,
		},
	}
}
