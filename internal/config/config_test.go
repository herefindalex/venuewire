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
