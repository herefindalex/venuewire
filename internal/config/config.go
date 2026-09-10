package config

import (
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	TestnetRESTBaseURL        = "https://api-testnet.bybit.com"
	TestnetWSPublicURL        = "wss://stream-testnet.bybit.com/v5/public/linear"
	TestnetWSPrivateURL       = "wss://stream-testnet.bybit.com/v5/private"
	TestnetWSTradeURL         = "wss://stream-testnet.bybit.com/v5/trade"
	TestnetFIXAddress         = "fix-oe-testnet.bybit.com:9000"
	DeribitTestnetHTTPBaseURL = "https://test.deribit.com/api/v2"
	DeribitTestnetWSURL       = "wss://test.deribit.com/ws/api/v2"
	DeribitTestnetFIXAddress  = "fix-test.deribit.com:9883"
)

type RiskLimits struct {
	MaxOrderUSD          string
	MaxAggregateOpenUSD  string
	MaxPriceDeviationPct string
	MaxOpenOrders        int
}

type DeribitConfig struct {
	Enabled         bool
	Environment     string
	AccountAlias    string
	APIKey          string
	APISecret       string
	HTTPBaseURL     string
	WSURL           string
	FIXEnabled      bool
	FIXAddress      string
	FIXSenderCompID string
	Risk            RiskLimits
	PlanTTL         time.Duration
	loadErrors      []error
}

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
	IntentFile        string
	AccountAlias      string
	Deribit           DeribitConfig
}

// Load reads configuration from the process environment. Endpoint overrides
// exist for explicit validation and diagnostics; they may never bypass the
// Testnet allowlist.
func Load() Config {
	deribit := loadDeribit()
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
		IntentFile:        envOrDefault("MULTI_VENUE_INTENT_FILE", "state/intents.json"),
		AccountAlias:      envOrDefault("BYBIT_ACCOUNT_ALIAS", "bybit-test"),
		Deribit:           deribit,
	}
}

func loadDeribit() DeribitConfig {
	d := DeribitConfig{
		Environment:  envOrDefault("DERIBIT_ENV", "testnet"),
		AccountAlias: envOrDefault("DERIBIT_ACCOUNT_ALIAS", "deribit-test"),
		APIKey:       os.Getenv("DERIBIT_API_KEY"), APISecret: os.Getenv("DERIBIT_API_SECRET"),
		HTTPBaseURL:     envOrDefault("DERIBIT_HTTP_BASE_URL", DeribitTestnetHTTPBaseURL),
		WSURL:           envOrDefault("DERIBIT_WS_URL", DeribitTestnetWSURL),
		FIXAddress:      envOrDefault("DERIBIT_FIX_ADDRESS", DeribitTestnetFIXAddress),
		FIXSenderCompID: envOrDefault("DERIBIT_FIX_SENDER_COMP_ID", "venuewire"),
		Risk: RiskLimits{
			MaxOrderUSD:          envOrDefault("DERIBIT_MAX_ORDER_USD", "100"),
			MaxAggregateOpenUSD:  envOrDefault("DERIBIT_MAX_OPEN_USD", "500"),
			MaxPriceDeviationPct: envOrDefault("DERIBIT_MAX_PRICE_DEVIATION_PCT", "2"),
		},
	}
	var err error
	d.Enabled, err = parseBoolEnv("DERIBIT_ENABLED", false)
	if err != nil {
		d.loadErrors = append(d.loadErrors, err)
	}
	d.FIXEnabled, err = parseBoolEnv("DERIBIT_FIX_ENABLED", false)
	if err != nil {
		d.loadErrors = append(d.loadErrors, err)
	}
	d.Risk.MaxOpenOrders, err = parsePositiveIntEnv("DERIBIT_MAX_OPEN_ORDERS", 5)
	if err != nil {
		d.loadErrors = append(d.loadErrors, err)
	}
	d.PlanTTL, err = time.ParseDuration(envOrDefault("DERIBIT_PLAN_TTL", "30s"))
	if err != nil {
		d.loadErrors = append(d.loadErrors, fmt.Errorf("DERIBIT_PLAN_TTL: %w", err))
	}
	return d
}

func parseBoolEnv(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return value, nil
}

func parsePositiveIntEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func (d DeribitConfig) ValidateTestnet() error {
	if len(d.loadErrors) > 0 {
		return errors.Join(d.loadErrors...)
	}
	if d.Environment != "testnet" {
		return fmt.Errorf("DERIBIT_ENV must be testnet, got %q", d.Environment)
	}
	if strings.TrimSpace(d.AccountAlias) == "" {
		return errors.New("DERIBIT_ACCOUNT_ALIAS is required")
	}
	if err := validateExactURL(d.HTTPBaseURL, DeribitTestnetHTTPBaseURL); err != nil {
		return fmt.Errorf("Deribit HTTP endpoint rejected: %w", err)
	}
	if err := validateExactURL(d.WSURL, DeribitTestnetWSURL); err != nil {
		return fmt.Errorf("Deribit WebSocket endpoint rejected: %w", err)
	}
	host, port, err := net.SplitHostPort(d.FIXAddress)
	if err != nil || !strings.EqualFold(host, "fix-test.deribit.com") || port != "9883" {
		return fmt.Errorf("Deribit FIX endpoint must be %s", DeribitTestnetFIXAddress)
	}
	for name, raw := range map[string]string{
		"DERIBIT_MAX_ORDER_USD":           d.Risk.MaxOrderUSD,
		"DERIBIT_MAX_OPEN_USD":            d.Risk.MaxAggregateOpenUSD,
		"DERIBIT_MAX_PRICE_DEVIATION_PCT": d.Risk.MaxPriceDeviationPct,
	} {
		value, ok := new(big.Rat).SetString(raw)
		if !ok || value.Sign() <= 0 {
			return fmt.Errorf("%s must be a positive decimal", name)
		}
	}
	if d.Risk.MaxOpenOrders <= 0 {
		return errors.New("DERIBIT_MAX_OPEN_ORDERS must be positive")
	}
	if d.PlanTTL <= 0 {
		return errors.New("DERIBIT_PLAN_TTL must be positive")
	}
	return nil
}

func (d DeribitConfig) RequireCredentials() error {
	if err := d.ValidateTestnet(); err != nil {
		return err
	}
	var missing []string
	if strings.TrimSpace(d.APIKey) == "" {
		missing = append(missing, "DERIBIT_API_KEY")
	}
	if strings.TrimSpace(d.APISecret) == "" {
		missing = append(missing, "DERIBIT_API_SECRET")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
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
