package webconsole

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/accountstate"
	"github.com/herefindalex/venuewire/internal/domain"
)

type fakeAccountService struct {
	mu           sync.Mutex
	snapshots    map[domain.Venue]accountstate.Snapshot
	health       []accountstate.VenueHealth
	refreshErr   error
	refreshCalls []domain.Venue
}

func (f *fakeAccountService) Snapshot(venue domain.Venue) (accountstate.Snapshot, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot, ok := f.snapshots[venue]
	return snapshot, ok
}

func (f *fakeAccountService) Refresh(_ context.Context, venue domain.Venue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls = append(f.refreshCalls, venue)
	return f.refreshErr
}

func (f *fakeAccountService) Health() []accountstate.VenueHealth {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]accountstate.VenueHealth(nil), f.health...)
}

func TestAccountEndpointsUseAuthenticatedCachedSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 9, 17, 0, 0, 0, time.UTC)
	service := &fakeAccountService{snapshots: map[domain.Venue]accountstate.Snapshot{
		domain.VenueBybit: {
			Venue: domain.VenueBybit, Environment: "testnet", AccountAlias: "bybit-test", AccountType: "UNIFIED", Revision: 7,
			SnapshotAsOf: now, ExchangeReportedAsOf: now, Completeness: "exchange-reported", LiabilityStatus: "unknown",
			DerivativePositionEvidence: "wallet snapshot does not prove derivative-position absence",
			Assets:                     []accountstate.Asset{{Asset: "BTC", Balance: "0.1", AvailableToTrade: "0.1", AvailableStatus: "verified", Quality: "snapshot"}},
		},
	}}
	server := newTestServer(t)
	server.accounts = service

	if response := performRequest(server, http.MethodGet, "/api/venues/bybit/account", "", nil, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated account status = %d", response.Code)
	}
	cookie, csrf := loginSession(t, server)
	response := performRequest(server, http.MethodGet, "/api/venues/bybit/account", "", cookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("account status = %d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Account accountstate.Snapshot `json:"account"`
	}
	decodeBody(t, response, &body)
	if body.Account.Revision != 7 || body.Account.AccountType != "UNIFIED" || body.Account.LiabilityStatus != "unknown" || body.Account.HasDerivativePositions != nil || body.Account.DerivativePositionEvidence == "" || body.Account.Assets[0].AvailableToTrade != "0.1" {
		t.Fatalf("account response = %+v", body.Account)
	}
	if response := performRequest(server, http.MethodGet, "/api/venues/deribit/account", "", cookie, ""); response.Code != http.StatusNotFound {
		t.Fatalf("disabled venue status = %d", response.Code)
	}

	refresh := performRequest(server, http.MethodPost, "/api/venues/bybit/account/refresh", "", cookie, csrf)
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", refresh.Code, refresh.Body.String())
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.refreshCalls) != 1 || service.refreshCalls[0] != domain.VenueBybit {
		t.Fatalf("refresh calls = %v", service.refreshCalls)
	}
}

func TestAccountRefreshRequiresCSRFAndReturnsSafeFailure(t *testing.T) {
	service := &fakeAccountService{
		snapshots:  make(map[domain.Venue]accountstate.Snapshot),
		refreshErr: errors.New("credential-like private failure detail"),
	}
	server := newTestServer(t)
	server.accounts = service
	cookie, csrf := loginSession(t, server)

	if response := performRequest(server, http.MethodPost, "/api/venues/bybit/account/refresh", "", cookie, "wrong"); response.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF status = %d", response.Code)
	}
	response := performRequest(server, http.MethodPost, "/api/venues/bybit/account/refresh", "", cookie, csrf)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("refresh failure status = %d body=%s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); containsAny(body, "credential-like", "private failure") {
		t.Fatalf("refresh response exposed private failure: %s", body)
	}
}

func TestAccountUnavailableAndSyncingAreExplicit(t *testing.T) {
	server := newTestServer(t)
	cookie, _ := loginSession(t, server)
	response := performRequest(server, http.MethodGet, "/api/venues/bybit/account", "", cookie, "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing service status = %d", response.Code)
	}
	server.accounts = &fakeAccountService{snapshots: make(map[domain.Venue]accountstate.Snapshot)}
	response = performRequest(server, http.MethodGet, "/api/venues/bybit/account", "", cookie, "")
	if response.Code != http.StatusServiceUnavailable || !containsAny(response.Body.String(), "account_syncing") {
		t.Fatalf("syncing response = %d %s", response.Code, response.Body.String())
	}
}

func TestSystemStatusUsesRuntimeHealthAndSafeBuildMetadata(t *testing.T) {
	now := time.Date(2026, 9, 9, 18, 0, 0, 0, time.UTC)
	service := &fakeAccountService{
		snapshots: map[domain.Venue]accountstate.Snapshot{
			domain.VenueBybit: {Venue: domain.VenueBybit, SnapshotAsOf: now.Add(-3 * time.Second)},
		},
		health: []accountstate.VenueHealth{{
			Venue: domain.VenueBybit, REST: "LIVE", PublicWS: "STALE", PrivateWS: "LIVE", AccountSync: "SYNCED",
			MarketEventAt: now.Add(-1500 * time.Millisecond), Reconnects: 2, LastReconcileAt: now.Add(-4 * time.Second),
			RequestErrors: 3, Discrepancies: 1, LastRequestRTT: 125 * time.Millisecond,
			LastPublicReceiveAt: now.Add(-100 * time.Millisecond), LastPrivateReceiveAt: now.Add(-200 * time.Millisecond),
			LastPublicEventAt: now.Add(-300 * time.Millisecond), LastPrivateEventAt: now.Add(-400 * time.Millisecond),
			PublicReconnects: 1, PrivateReconnects: 1, OrderRequestRTT: 82 * time.Millisecond,
			FirstOrderEventLatency: 104 * time.Millisecond, HasFirstOrderEvent: true,
			FirstExecutionEventLatency: 116 * time.Millisecond, HasFirstExecutionEvent: true,
			OrderRequestErrors: 2, RateLimitState: "OK", ReconciliationStatus: "SYNCED",
		}},
	}
	server := newTestServer(t)
	server.accounts = service
	server.now = func() time.Time { return now }
	server.startedAt = now.Add(-65 * time.Second)
	server.build = BuildMetadata{Timestamp: "2026-09-09T17:55:00Z", Commit: "abc123"}
	cookie, _ := loginSession(t, server)

	response := performRequest(server, http.MethodGet, "/api/system/status", "", cookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status endpoint = %d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Venues []publicVenueStatus `json:"venues"`
		Build  map[string]string   `json:"build"`
	}
	decodeBody(t, response, &body)
	if len(body.Venues) != 1 {
		t.Fatalf("venue statuses = %+v", body.Venues)
	}
	status := body.Venues[0]
	if status.PublicReceiveAgeMS == nil || *status.PublicReceiveAgeMS != 100 || status.PrivateReceiveAgeMS == nil || *status.PrivateReceiveAgeMS != 200 || status.PublicEventAgeMS == nil || *status.PublicEventAgeMS != 300 || status.PrivateEventAgeMS == nil || *status.PrivateEventAgeMS != 400 {
		t.Fatalf("stream ages = %+v", status)
	}
	if status.OrderRequestRTTMS == nil || *status.OrderRequestRTTMS != 82 || status.FirstOrderEventLatencyMS == nil || *status.FirstOrderEventLatencyMS != 104 || status.FirstExecEventLatencyMS == nil || *status.FirstExecEventLatencyMS != 116 {
		t.Fatalf("order timings = %+v", status)
	}
	if status.PublicReconnects != 1 || status.PrivateReconnects != 1 || status.OrderRequestErrors != 2 || status.RateLimitState != "OK" || status.ReconciliationStatus != "SYNCED" {
		t.Fatalf("runtime counters = %+v", status)
	}
	if status.REST != "LIVE" || status.PublicWS != "STALE" || status.MarketAgeMS == nil || *status.MarketAgeMS != 1500 || status.AccountAgeMS == nil || *status.AccountAgeMS != 3000 || status.LastRequestRTT == nil || *status.LastRequestRTT != 125 {
		t.Fatalf("runtime status = %+v", status)
	}
	if body.Build["environment"] != "Testnet" || body.Build["gitCommit"] != "abc123" || body.Build["uptime"] != "1m5s" {
		t.Fatalf("build metadata = %+v", body.Build)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if containsAny(string(encoded), "fixture-api-key", "fixture-api-secret", "fixture-password") {
		t.Fatalf("system status exposed a secret: %s", encoded)
	}
}

func TestSystemStatusKeepsHeartbeatLiveWhileSurfacingStaleAccountAndMarket(t *testing.T) {
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	service := &fakeAccountService{health: []accountstate.VenueHealth{{
		Venue:                domain.VenueBybit,
		REST:                 "LIVE",
		PublicWS:             "STALE",
		PrivateWS:            "LIVE",
		AccountSync:          "STALE",
		LastPublicReceiveAt:  now.Add(-500 * time.Millisecond),
		LastPrivateReceiveAt: now.Add(-250 * time.Millisecond),
		LastPublicEventAt:    now.Add(-8 * time.Second),
		PublicReconnects:     2,
		PrivateReconnects:    1,
	}}}
	server := newTestServer(t)
	server.now = func() time.Time { return now }
	server.accounts = service
	cookie, _ := loginSession(t, server)
	response := performRequest(server, http.MethodGet, "/api/system/status", "", cookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status response = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Venues []publicVenueStatus `json:"venues"`
	}
	decodeBody(t, response, &body)
	if len(body.Venues) != 1 {
		t.Fatalf("status venues = %+v", body.Venues)
	}
	status := body.Venues[0]
	if status.PublicWS != "STALE" || status.PrivateWS != "LIVE" || status.AccountSync != "STALE" {
		t.Fatalf("stale/live states = %+v", status)
	}
	if status.PublicReceiveAgeMS == nil || *status.PublicReceiveAgeMS != 500 || status.PrivateReceiveAgeMS == nil || *status.PrivateReceiveAgeMS != 250 || status.PublicEventAgeMS == nil || *status.PublicEventAgeMS != 8000 {
		t.Fatalf("stream ages = %+v", status)
	}
	if status.PublicReconnects != 2 || status.PrivateReconnects != 1 {
		t.Fatalf("reconnect counters = %+v", status)
	}
}

func loginSession(t *testing.T, server *Server) (*http.Cookie, string) {
	t.Helper()
	response := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeBody(t, response, &body)
	return response.Result().Cookies()[0], body.CSRFToken
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
