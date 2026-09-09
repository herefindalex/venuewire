package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

const (
	TestnetRESTBaseURL  = "https://api-testnet.bybit.com"
	TestnetWSPublicURL  = "wss://stream-testnet.bybit.com/v5/public/linear"
	TestnetWSPrivateURL = "wss://stream-testnet.bybit.com/v5/private"
	TestnetWSTradeURL   = "wss://stream-testnet.bybit.com/v5/trade"
	TestnetFIXAddress   = "fix-oe-testnet.bybit.com:9000"
)

type Config struct {
	Environment       string
	APIKey            string
	APISecret         string
	FIXAPIKey         string
	FIXPrivateKeyPath string
	RESTBaseURL       string
	WSPublicURL       string
	WSPrivateURL      string
	WSTradeURL        string
	FIXAddress        string
	StateFile         string
}

// Load reads configuration from the process environment. Endpoint overrides
// exist for explicit validation and diagnostics; they may never bypass the
// Testnet allowlist.
func Load() Config {
	return Config{
		Environment:       envOrDefault("BYBIT_ENV", "testnet"),
		APIKey:            os.Getenv("BYBIT_API_KEY"),
		APISecret:         os.Getenv("BYBIT_API_SECRET"),
		FIXAPIKey:         os.Getenv("BYBIT_FIX_API_KEY"),
		FIXPrivateKeyPath: os.Getenv("BYBIT_FIX_PRIVATE_KEY_PATH"),
		RESTBaseURL:       envOrDefault("BYBIT_REST_BASE_URL", TestnetRESTBaseURL),
		WSPublicURL:       envOrDefault("BYBIT_WS_PUBLIC_URL", TestnetWSPublicURL),
		WSPrivateURL:      envOrDefault("BYBIT_WS_PRIVATE_URL", TestnetWSPrivateURL),
		WSTradeURL:        envOrDefault("BYBIT_WS_TRADE_URL", TestnetWSTradeURL),
		FIXAddress:        envOrDefault("BYBIT_FIX_ADDRESS", TestnetFIXAddress),
		StateFile:         envOrDefault("BYBIT_STATE_FILE", "state/orders.json"),
	}
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func (c Config) ValidateTestnet() error {
	if c.Environment != "testnet" {
		return fmt.Errorf("BYBIT_ENV must be testnet, got %q", c.Environment)
	}
	checks := []struct {
		name, got, want string
	}{
		{"REST", c.RESTBaseURL, TestnetRESTBaseURL},
		{"public WebSocket", c.WSPublicURL, TestnetWSPublicURL},
		{"private WebSocket", c.WSPrivateURL, TestnetWSPrivateURL},
		{"trade WebSocket", c.WSTradeURL, TestnetWSTradeURL},
	}
	for _, check := range checks {
		if err := validateExactURL(check.got, check.want); err != nil {
			return fmt.Errorf("%s endpoint rejected: %w", check.name, err)
		}
	}
	host, port, err := net.SplitHostPort(c.FIXAddress)
	if err != nil || !strings.EqualFold(host, "fix-oe-testnet.bybit.com") || port != "9000" {
		return fmt.Errorf("FIX endpoint must be %s", TestnetFIXAddress)
	}
	return nil
}

func validateExactURL(raw, allowed string) error {
	got, err := url.Parse(raw)
	if err != nil || got.User != nil || got.RawQuery != "" || got.Fragment != "" {
		return errors.New("endpoint is not a valid approved Testnet URL")
	}
	want, _ := url.Parse(allowed)
	if !strings.EqualFold(got.Scheme, want.Scheme) || !strings.EqualFold(got.Host, want.Host) || got.EscapedPath() != want.EscapedPath() {
		return fmt.Errorf("endpoint must be %s", allowed)
	}
	return nil
}

func (c Config) RequireRESTCredentials() error {
	if err := c.ValidateTestnet(); err != nil {
		return err
	}
	var missing []string
	if strings.TrimSpace(c.APIKey) == "" {
		missing = append(missing, "BYBIT_API_KEY")
	}
	if strings.TrimSpace(c.APISecret) == "" {
		missing = append(missing, "BYBIT_API_SECRET")
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c Config) RequireFIXCredentials() error {
	if err := c.ValidateTestnet(); err != nil {
		return err
	}
	var missing []string
	if strings.TrimSpace(c.FIXAPIKey) == "" {
		missing = append(missing, "BYBIT_FIX_API_KEY")
	}
	if strings.TrimSpace(c.FIXPrivateKeyPath) == "" {
		missing = append(missing, "BYBIT_FIX_PRIVATE_KEY_PATH")
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}
