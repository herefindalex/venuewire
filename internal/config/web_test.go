package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestLoadWebConfigValidatesSafeDefaults(t *testing.T) {
	setValidWebEnvironment(t)
	base := validWebBaseConfig()
	base.APIKey, base.APISecret = "fixture-key", "fixture-secret"

	cfg, err := LoadWebConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddress() != "10.0.0.20:8080" || cfg.QuoteTTL != 5*time.Second {
		t.Fatalf("unexpected web config: %+v", cfg)
	}
	if cfg.MaxTradesPerSession != 10 || cfg.MaxTradesPerHour != 30 || cfg.MaxConcurrentTrades != 1 {
		t.Fatalf("unexpected demo limits: %+v", cfg)
	}
}

func TestLoadWebConfigRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"public bind", "WEB_HOST", "0.0.0.0", "WEB_HOST"},
		{"HTTP origin", "WEB_PUBLIC_ORIGIN", "http://trade.example.com", "WEB_PUBLIC_ORIGIN"},
		{"origin path", "WEB_PUBLIC_ORIGIN", "https://trade.example.com/app", "WEB_PUBLIC_ORIGIN"},
		{"proxy subnet", "WEB_TRUSTED_PROXY_CIDRS", "10.0.0.0/24", "one proxy IP"},
		{"weak password", "WEB_PASSWORD", "too-short", "WEB_PASSWORD"},
		{"empty duration overrides default", "QUOTE_TTL", "", "QUOTE_TTL"},
		{"excess slippage", "QUICK_TRADE_SLIPPAGE_BPS", "51", "QUICK_TRADE_SLIPPAGE_BPS"},
		{"zero quota", "DEMO_MAX_TRADES_PER_HOUR", "0", "DEMO_MAX_TRADES_PER_HOUR"},
		{"invalid amount", "DEMO_MAX_BYBIT_BTC_QTY", "NaN", "positive decimal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setValidWebEnvironment(t)
			t.Setenv(tc.key, tc.value)
			base := validWebBaseConfig()
			base.APIKey, base.APISecret = "fixture-key", "fixture-secret"
			_, err := LoadWebConfig(base)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadWebConfigRequiresEnabledVenueCredentials(t *testing.T) {
	setValidWebEnvironment(t)
	base := validWebBaseConfig()
	_, err := LoadWebConfig(base)
	if err == nil || !strings.Contains(err.Error(), "BYBIT_API_KEY") || !strings.Contains(err.Error(), "BYBIT_API_SECRET") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadWebConfigRejectsDisabledDefaultVenue(t *testing.T) {
	setValidWebEnvironment(t)
	t.Setenv("BYBIT_ENABLED", "false")
	t.Setenv("DERIBIT_ENABLED", "true")
	base := validWebBaseConfig()
	base.Deribit.Enabled = true
	base.Deribit.APIKey, base.Deribit.APISecret = "fixture-key", "fixture-secret"
	_, err := LoadWebConfig(base)
	if err == nil || !strings.Contains(err.Error(), "WEB_DEFAULT_VENUE must be enabled") {
		t.Fatalf("error = %v", err)
	}
}

func validWebBaseConfig() Config {
	cfg := validConfig()
	cfg.Deribit = DeribitConfig{
		Environment:  "testnet",
		AccountAlias: "deribit-test",
		HTTPBaseURL:  DeribitTestnetHTTPBaseURL,
		WSURL:        DeribitTestnetWSURL,
		FIXAddress:   DeribitTestnetFIXAddress,
		Risk: RiskLimits{
			MaxOrderUSD:          "100",
			MaxAggregateOpenUSD:  "500",
			MaxPriceDeviationPct: "2",
			MaxOpenOrders:        5,
		},
		PlanTTL: 30 * time.Second,
	}
	return cfg
}

func setValidWebEnvironment(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"WEB_HOST":                "10.0.0.20",
		"WEB_PUBLIC_ORIGIN":       "https://trade.example.com",
		"WEB_TRUSTED_PROXY_CIDRS": "10.0.0.10/32",
		"WEB_USERNAME":            "alex",
		"WEB_PASSWORD":            "fixture-password",
		"WEB_SESSION_SECRET":      base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"BYBIT_ENABLED":           "true",
		"DERIBIT_ENABLED":         "false",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	for _, key := range []string{
		"WEB_PORT", "WEB_SESSION_TTL", "WEB_DEFAULT_VENUE", "WEB_TRADING_ENABLED",
		"WEB_PUSH_INTERVAL", "ACCOUNT_RECONCILE_INTERVAL", "QUOTE_TTL", "TRADE_BOOK_MAX_AGE",
		"VALUATION_PRICE_MAX_AGE", "QUICK_TRADE_SLIPPAGE_BPS", "QUICK_TRADE_MAX_BTC",
		"QUICK_TRADE_MAX_ETH", "QUICK_TRADE_MAX_USDT", "DEMO_MAX_TRADES_PER_SESSION",
		"DEMO_MAX_TRADES_PER_HOUR", "DEMO_MAX_CONCURRENT_TRADES", "DEMO_MAX_BYBIT_BTC_QTY",
		"DEMO_MAX_BYBIT_USDT_AMOUNT", "DEMO_MAX_DERIBIT_BTC_AMOUNT", "DEMO_MAX_DERIBIT_ETH_AMOUNT",
	} {
		unsetAfterTest(t, key)
	}
}
