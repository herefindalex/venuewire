package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bybit/internal/config"
	"bybit/internal/domain"
	"bybit/internal/observability"
	"bybit/internal/orderstate"
)

func TestAuthenticatedCommandClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		rest bool
		fix  bool
	}{
		{args: []string{"time"}},
		{args: []string{"market", "trades"}},
		{args: []string{"order", "place"}, rest: true},
		{args: []string{"private-stream"}, rest: true},
		{args: []string{"reconcile"}, rest: true},
		{args: []string{"fix", "mock-demo"}},
		{args: []string{"fix", "connect-testnet"}, fix: true},
		{args: []string{"venue", "deribit", "fix", "mock-demo"}},
		{args: []string{"venue", "deribit", "fix", "connect-testnet"}},
	}
	for _, tt := range tests {
		if got := isRESTAuthenticatedCommand(tt.args); got != tt.rest {
			t.Errorf("isRESTAuthenticatedCommand(%q) = %v, want %v", tt.args, got, tt.rest)
		}
		if got := isFIXAuthenticatedCommand(tt.args); got != tt.fix {
			t.Errorf("isFIXAuthenticatedCommand(%q) = %v, want %v", tt.args, got, tt.fix)
		}
	}
}

func TestUsageListsEveryImplementedCommand(t *testing.T) {
	commands := []string{
		"time",
		"instrument",
		"account info",
		"account balances",
		"market trades",
		"market orderbook",
		"order place",
		"order cancel",
		"order amend",
		"order status",
		"executions",
		"positions",
		"private-stream",
		"reconcile",
		"fix mock-demo",
		"fix mock-server",
		"fix connect-testnet",
	}
	for _, command := range commands {
		if !strings.Contains(usage, "bybitctl "+command) {
			t.Errorf("help does not list %q", command)
		}
	}
	for _, command := range []string{
		"bybitctl --venue deribit fix mock-demo",
		"bybitctl --venue deribit fix connect-testnet",
		"--enable-connection-cod --confirm",
	} {
		if !strings.Contains(usage, command) {
			t.Errorf("help does not list %q", command)
		}
	}
	if strings.Contains(usage, "added phase-by-phase") {
		t.Error("help still contains the Phase 0 placeholder")
	}
}

func TestRunRefusesAuthenticatedCommandWithoutCredentials(t *testing.T) {
	setCleanTestnetEnvironment(t)
	if code := run([]string{"order", "status"}); code != 2 {
		t.Fatalf("run returned %d, want configuration error exit code 2", code)
	}
}

func TestRunAllowsDeribitFIXMockWhenVenueIsDisabled(t *testing.T) {
	setCleanTestnetEnvironment(t)
	t.Setenv("DERIBIT_ENABLED", "false")

	if code := runContext(context.Background(), []string{"--venue", "deribit", "fix", "mock-demo"}); code != 0 {
		t.Fatalf("runContext returned %d, want 0 for credential-free local mock", code)
	}
}

func TestDeribitPrivateStreamCODRequiresExplicitConfirmation(t *testing.T) {
	setCleanTestnetEnvironment(t)
	cfg := config.Load()
	handled, err := executeDeribitCommand(context.Background(), cfg, []string{"venue", "deribit", "private-stream", "--enable-connection-cod"}, io.Discard)
	if !handled || err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("handled=%v error=%v, want explicit confirmation error", handled, err)
	}
}

func TestRunRejectsMainnetBeforeAuthenticatedCommand(t *testing.T) {
	setCleanTestnetEnvironment(t)
	t.Setenv("BYBIT_API_KEY", "fixture-key")
	t.Setenv("BYBIT_API_SECRET", "fixture-secret")
	t.Setenv("BYBIT_REST_BASE_URL", "https://api.bybit.com")
	if code := run([]string{"order", "status"}); code != 2 {
		t.Fatalf("run returned %d, want configuration error exit code 2", code)
	}
}

func setCleanTestnetEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"BYBIT_ENV", "BYBIT_API_KEY", "BYBIT_API_SECRET", "BYBIT_FIX_API_KEY",
		"BYBIT_FIX_PRIVATE_KEY_PATH", "BYBIT_REST_BASE_URL", "BYBIT_WS_PUBLIC_URL",
		"BYBIT_WS_PRIVATE_URL", "BYBIT_WS_TRADE_URL", "BYBIT_FIX_ADDRESS",
	} {
		t.Setenv(key, "")
	}
}

func TestRESTPlaceCommandPersistsAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/order/create" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"orderLinkId":"client-fixture"`) {
			t.Errorf("missing orderLinkId in %s", body)
		}
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"orderId":"exchange-fixture","orderLinkId":"client-fixture"}}`)
	}))
	defer server.Close()

	statePath := filepath.Join(t.TempDir(), "orders.json")
	cfg := config.Config{RESTBaseURL: server.URL, APIKey: "fixture-key", APISecret: "fixture-secret", StateFile: statePath}
	var logs, output bytes.Buffer
	logger := observability.NewJSON(&logs, cfg.APISecret)
	handled, err := executeRESTCommand(context.Background(), cfg, logger, []string{
		"order", "place", "--symbol", "BTCUSDT", "--side", "Buy", "--qty", "0.001", "--price", "10000", "--order-link-id", "client-fixture",
	}, &output)
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	snapshot, err := (orderstate.FileStore{Path: statePath}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	order := snapshot.Orders["client-fixture"]
	if order.OrderID != "exchange-fixture" || order.RawStatus != "REST_ACK" {
		t.Fatalf("unexpected persisted order: %+v", order)
	}
	if !strings.Contains(logs.String(), "exchange-fixture") || !strings.Contains(logs.String(), "client-fixture") {
		t.Fatalf("correlation identifiers missing from logs: %s", logs.String())
	}
	if strings.Contains(logs.String(), cfg.APISecret) {
		t.Fatalf("API secret leaked: %s", logs.String())
	}
}

func TestRESTPlaceCommandPersistsBusinessRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/order/create" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"retCode":10001,"retMsg":"fixture rejection","result":{}}`)
	}))
	defer server.Close()

	statePath := filepath.Join(t.TempDir(), "orders.json")
	cfg := config.Config{
		RESTBaseURL: server.URL,
		APIKey:      "fixture-key",
		APISecret:   "fixture-secret",
		StateFile:   statePath,
	}
	logger := observability.NewJSON(io.Discard, cfg.APISecret)
	handled, err := executeRESTCommand(context.Background(), cfg, logger, []string{
		"order", "place", "--symbol", "BTCUSDT", "--side", "Buy", "--qty", "0.001", "--price", "10000",
		"--order-link-id", "client-rejected",
	}, io.Discard)
	if !handled {
		t.Fatal("REST order command was not handled")
	}
	if err == nil {
		t.Fatal("expected fixture business rejection")
	}

	snapshot, loadErr := (orderstate.FileStore{Path: statePath}).Load(context.Background())
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	order, ok := snapshot.Orders["client-rejected"]
	if !ok {
		t.Fatal("rejected order was not persisted")
	}
	if order.Status != domain.OrderStatusRejected {
		t.Fatalf("status = %q, want %q", order.Status, domain.OrderStatusRejected)
	}
	if order.RawStatus != "REST_REJECTED_10001" {
		t.Fatalf("raw status = %q, want REST_REJECTED_10001", order.RawStatus)
	}
}

func TestRESTAccountCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v5/account/wallet-balance":
			if got := r.URL.Query().Get("accountType"); got != "UNIFIED" {
				t.Errorf("accountType = %q, want UNIFIED", got)
			}
			if got := r.URL.Query().Get("coin"); got != "" {
				t.Errorf("coin = %q, want omitted", got)
			}
			_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"list":[{"accountType":"UNIFIED","totalEquity":"12500","totalWalletBalance":"12400","totalAvailableBalance":"10000","coin":[{"coin":"BTC","equity":"1","walletBalance":"1","usdValue":"78000"},{"coin":"USDT","equity":"10000","walletBalance":"10000","usdValue":"10000"}]}]}}`)
		case "/v5/account/info":
			_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"unifiedMarginStatus":5,"marginMode":"REGULAR_MARGIN","spotHedgingStatus":"OFF","updatedTime":"1700000000123"}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Config{RESTBaseURL: server.URL, APIKey: "fixture-key", APISecret: "fixture-secret"}
	logger := observability.NewJSON(io.Discard, cfg.APISecret)

	var balancesOutput bytes.Buffer
	handled, err := executeRESTCommand(context.Background(), cfg, logger, []string{"account", "balances"}, &balancesOutput)
	if !handled || err != nil {
		t.Fatalf("balances handled=%v err=%v", handled, err)
	}
	var balances []struct {
		AccountType string `json:"accountType"`
		Coin        []struct {
			Coin string `json:"coin"`
		} `json:"coin"`
	}
	if err := json.Unmarshal(balancesOutput.Bytes(), &balances); err != nil {
		t.Fatal(err)
	}
	if len(balances) != 1 || balances[0].AccountType != "UNIFIED" || len(balances[0].Coin) != 2 {
		t.Fatalf("unexpected balances: %+v", balances)
	}

	var infoOutput bytes.Buffer
	handled, err = executeRESTCommand(context.Background(), cfg, logger, []string{"account", "info"}, &infoOutput)
	if !handled || err != nil {
		t.Fatalf("info handled=%v err=%v", handled, err)
	}
	var info struct {
		UnifiedMarginStatus int    `json:"unifiedMarginStatus"`
		MarginMode          string `json:"marginMode"`
	}
	if err := json.Unmarshal(infoOutput.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.UnifiedMarginStatus != 5 || info.MarginMode != "REGULAR_MARGIN" {
		t.Fatalf("unexpected account info: %+v", info)
	}
}

func TestRESTAccountBalancesRejectMalformedCoinFilter(t *testing.T) {
	cfg := config.Config{RESTBaseURL: "https://api-testnet.bybit.com", APIKey: "fixture-key", APISecret: "fixture-secret"}
	logger := observability.NewJSON(io.Discard, cfg.APISecret)
	handled, err := executeRESTCommand(context.Background(), cfg, logger, []string{"account", "balances", "--coin", "BTC,,ETH"}, io.Discard)
	if !handled {
		t.Fatal("account command was not handled")
	}
	if err == nil || !strings.Contains(err.Error(), "coin") {
		t.Fatalf("error = %v, want coin validation error", err)
	}
}
