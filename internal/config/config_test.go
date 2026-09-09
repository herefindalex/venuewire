package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsToTestnet(t *testing.T) {
	for _, key := range []string{"BYBIT_ENV", "BYBIT_REST_BASE_URL", "BYBIT_WS_PUBLIC_URL", "BYBIT_WS_PRIVATE_URL", "BYBIT_WS_TRADE_URL", "BYBIT_FIX_ADDRESS"} {
		t.Setenv(key, "")
	}
	cfg := Load()
	if cfg.Environment != "testnet" || cfg.RESTBaseURL != TestnetRESTBaseURL || cfg.WSPublicURL != TestnetWSPublicURL || cfg.WSPrivateURL != TestnetWSPrivateURL || cfg.FIXAddress != TestnetFIXAddress {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if err := cfg.ValidateTestnet(); err != nil {
		t.Fatalf("default configuration rejected: %v", err)
	}
}

func TestDeribitDisabledDefaultsAreSafeAndNonZero(t *testing.T) {
	for _, key := range []string{
		"DERIBIT_ENABLED", "DERIBIT_ENV", "DERIBIT_ACCOUNT_ALIAS", "DERIBIT_HTTP_BASE_URL",
		"DERIBIT_WS_URL", "DERIBIT_FIX_ADDRESS", "DERIBIT_MAX_ORDER_USD",
		"DERIBIT_MAX_OPEN_USD", "DERIBIT_MAX_PRICE_DEVIATION_PCT", "DERIBIT_MAX_OPEN_ORDERS",
		"DERIBIT_PLAN_TTL",
	} {
		t.Setenv(key, "")
	}
	cfg := Load()
	if cfg.Deribit.Enabled {
		t.Fatal("Deribit must be disabled by default")
	}
	if err := cfg.Deribit.ValidateTestnet(); err != nil {
		t.Fatalf("safe defaults rejected: %v", err)
	}
}

func TestDeribitRejectsHostConfusionMainnetAndZeroRisk(t *testing.T) {
	base := loadDeribit()
	tests := []struct {
		name   string
		mutate func(*DeribitConfig)
	}{
		{"environment", func(c *DeribitConfig) { c.Environment = "production" }},
		{"HTTP mainnet", func(c *DeribitConfig) { c.HTTPBaseURL = "https://www.deribit.com/api/v2" }},
		{"HTTP suffix attack", func(c *DeribitConfig) { c.HTTPBaseURL = "https://test.deribit.com.attacker.invalid/api/v2" }},
		{"WS mainnet", func(c *DeribitConfig) { c.WSURL = "wss://www.deribit.com/ws/api/v2" }},
		{"FIX mainnet", func(c *DeribitConfig) { c.FIXAddress = "fix.deribit.com:9883" }},
		{"zero order risk", func(c *DeribitConfig) { c.Risk.MaxOrderUSD = "0" }},
		{"negative aggregate risk", func(c *DeribitConfig) { c.Risk.MaxAggregateOpenUSD = "-1" }},
		{"zero orders", func(c *DeribitConfig) { c.Risk.MaxOpenOrders = 0 }},
		{"zero TTL", func(c *DeribitConfig) { c.PlanTTL = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)
			if err := cfg.ValidateTestnet(); err == nil {
				t.Fatal("unsafe Deribit configuration accepted")
			}
		})
	}
}

func TestDeribitRequiresBothCredentials(t *testing.T) {
	cfg := loadDeribit()
	cfg.APIKey, cfg.APISecret = "", ""
	err := cfg.RequireCredentials()
	if err == nil || !strings.Contains(err.Error(), "DERIBIT_API_KEY") || !strings.Contains(err.Error(), "DERIBIT_API_SECRET") {
		t.Fatalf("missing credentials not reported together: %v", err)
	}
}

func TestDeribitInvalidEnvironmentValuesFailClosed(t *testing.T) {
	t.Setenv("DERIBIT_ENABLED", "sometimes")
	t.Setenv("DERIBIT_MAX_OPEN_ORDERS", "none")
	cfg := Load()
	if err := cfg.Deribit.ValidateTestnet(); err == nil {
		t.Fatal("invalid environment values accepted")
	}
}

func TestRequireRESTCredentialsReportsBothMissing(t *testing.T) {
	cfg := validConfig()
	err := cfg.RequireRESTCredentials()
	if err == nil || !strings.Contains(err.Error(), "BYBIT_API_KEY") || !strings.Contains(err.Error(), "BYBIT_API_SECRET") {
		t.Fatalf("expected both missing variables, got %v", err)
	}
}

func TestRequireCredentialsAcceptsConfiguredTestnet(t *testing.T) {
	cfg := validConfig()
	cfg.APIKey = "fixture-key"
	cfg.APISecret = "fixture-secret"
	if err := cfg.RequireRESTCredentials(); err != nil {
		t.Fatalf("valid credentials rejected: %v", err)
	}
}

func TestMainnetAndHostConfusionAreRejected(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"environment", func(c *Config) { c.Environment = "mainnet" }},
		{"REST mainnet", func(c *Config) { c.RESTBaseURL = "https://api.bybit.com" }},
		{"REST suffix attack", func(c *Config) { c.RESTBaseURL = "https://api-testnet.bybit.com.attacker.invalid" }},
		{"public WS mainnet", func(c *Config) { c.WSPublicURL = "wss://stream.bybit.com/v5/public/linear" }},
		{"private WS mainnet", func(c *Config) { c.WSPrivateURL = "wss://stream.bybit.com/v5/private" }},
		{"FIX mainnet", func(c *Config) { c.FIXAddress = "fix-oe.bybit.com:9000" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)
			if err := cfg.ValidateTestnet(); err == nil {
				t.Fatal("expected configuration rejection")
			}
		})
	}
}

func validConfig() Config {
	return Config{
		Environment:  "testnet",
		RESTBaseURL:  TestnetRESTBaseURL,
		WSPublicURL:  TestnetWSPublicURL,
		WSPrivateURL: TestnetWSPrivateURL,
		WSTradeURL:   TestnetWSTradeURL,
		FIXAddress:   TestnetFIXAddress,
	}
}
