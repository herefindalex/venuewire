package webconsole

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"venuewire/internal/config"
	"venuewire/internal/domain"
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

	logout := performRequest(server, http.MethodPost, "/api/auth/logout", "", cookie, loginBody.CSRFToken)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logout.Code)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("WebSocket remained open after logout")
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
