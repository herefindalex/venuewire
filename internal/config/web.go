package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/herefindalex/venuewire/internal/domain"
)

type WebConfig struct {
	Host                     string
	Port                     int
	PublicOrigin             string
	TrustedProxyCIDRs        []*net.IPNet
	Username                 string
	Password                 string
	SessionSecret            []byte
	SessionTTL               time.Duration
	DefaultVenue             domain.Venue
	BybitEnabled             bool
	DeribitEnabled           bool
	TradingEnabled           bool
	PushInterval             time.Duration
	AccountReconcileInterval time.Duration
	QuoteTTL                 time.Duration
	TradeBookMaxAge          time.Duration
	ValuationPriceMaxAge     time.Duration
	QuickTradeSlippageBPS    int
	QuickTradeMaxBTC         string
	QuickTradeMaxETH         string
	QuickTradeMaxUSDT        string
	QuickTradeMaxUSDC        string
	MaxTradesPerSession      int
	MaxTradesPerHour         int
	MaxConcurrentTrades      int
	MaxBybitBTCQty           string
	MaxBybitETHQty           string
	MaxBybitUSDTAmount       string
	MaxDeribitBTCAmount      string
	MaxDeribitUSDCAmount     string
}

func LoadWebConfig(base Config) (WebConfig, error) {
	cfg := WebConfig{
		Host:                 webEnv("WEB_HOST", ""),
		PublicOrigin:         webEnv("WEB_PUBLIC_ORIGIN", ""),
		Username:             webEnv("WEB_USERNAME", ""),
		Password:             webEnv("WEB_PASSWORD", ""),
		DefaultVenue:         domain.Venue(webEnv("WEB_DEFAULT_VENUE", "bybit")),
		QuickTradeMaxBTC:     webEnv("QUICK_TRADE_MAX_BTC", "0.01"),
		QuickTradeMaxETH:     webEnv("QUICK_TRADE_MAX_ETH", "1"),
		QuickTradeMaxUSDT:    webEnv("QUICK_TRADE_MAX_USDT", "1000"),
		QuickTradeMaxUSDC:    webEnv("QUICK_TRADE_MAX_USDC", "1000"),
		MaxBybitBTCQty:       webEnv("DEMO_MAX_BYBIT_BTC_QTY", "0.01"),
		MaxBybitETHQty:       webEnv("DEMO_MAX_BYBIT_ETH_QTY", "1"),
		MaxBybitUSDTAmount:   webEnv("DEMO_MAX_BYBIT_USDT_AMOUNT", "1000"),
		MaxDeribitBTCAmount:  webEnv("DEMO_MAX_DERIBIT_BTC_AMOUNT", "0.01"),
		MaxDeribitUSDCAmount: webEnv("DEMO_MAX_DERIBIT_USDC_AMOUNT", "1000"),
	}

	var errs []error
	cfg.Port = parseWebInt("WEB_PORT", 8080, 1, 65535, &errs)
	cfg.SessionTTL = parseWebDuration("WEB_SESSION_TTL", 8*time.Hour, &errs)
	cfg.PushInterval = parseWebDuration("WEB_PUSH_INTERVAL", 250*time.Millisecond, &errs)
	cfg.AccountReconcileInterval = parseWebDuration("ACCOUNT_RECONCILE_INTERVAL", 30*time.Second, &errs)
	cfg.QuoteTTL = parseWebDuration("QUOTE_TTL", 5*time.Second, &errs)
	cfg.TradeBookMaxAge = parseWebDuration("TRADE_BOOK_MAX_AGE", 3*time.Second, &errs)
	cfg.ValuationPriceMaxAge = parseWebDuration("VALUATION_PRICE_MAX_AGE", 15*time.Second, &errs)
	cfg.QuickTradeSlippageBPS = parseWebInt("QUICK_TRADE_SLIPPAGE_BPS", 50, 1, 50, &errs)
	cfg.MaxTradesPerSession = parseWebInt("DEMO_MAX_TRADES_PER_SESSION", 10, 1, 10000, &errs)
	cfg.MaxTradesPerHour = parseWebInt("DEMO_MAX_TRADES_PER_HOUR", 30, 1, 100000, &errs)
	cfg.MaxConcurrentTrades = parseWebInt("DEMO_MAX_CONCURRENT_TRADES", 1, 1, 1000, &errs)
	cfg.BybitEnabled = parseWebBool("BYBIT_ENABLED", false, &errs)
	cfg.DeribitEnabled = parseWebBool("DERIBIT_ENABLED", false, &errs)
	cfg.TradingEnabled = parseWebBool("WEB_TRADING_ENABLED", false, &errs)

	secretText := webEnv("WEB_SESSION_SECRET", "")
	if secretText != "" {
		secret, err := base64.StdEncoding.DecodeString(secretText)
		if err != nil || len(secret) < 32 {
			errs = append(errs, errors.New("WEB_SESSION_SECRET must be Base64 encoding of at least 32 random bytes"))
		} else {
			cfg.SessionSecret = secret
		}
	}

	proxyText := webEnv("WEB_TRUSTED_PROXY_CIDRS", "")
	if proxyText != "" {
		for _, raw := range strings.Split(proxyText, ",") {
			_, network, err := net.ParseCIDR(strings.TrimSpace(raw))
			if err != nil {
				errs = append(errs, fmt.Errorf("WEB_TRUSTED_PROXY_CIDRS contains invalid CIDR %q", strings.TrimSpace(raw)))
				continue
			}
			ones, bits := network.Mask.Size()
			if ones != bits {
				errs = append(errs, fmt.Errorf("WEB_TRUSTED_PROXY_CIDRS entry %q must identify one proxy IP", raw))
				continue
			}
			if !network.IP.IsPrivate() && !network.IP.IsLoopback() {
				errs = append(errs, fmt.Errorf("WEB_TRUSTED_PROXY_CIDRS entry %q must be private or loopback", raw))
				continue
			}
			cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, network)
		}
	}

	errs = append(errs, validateWebConfig(base, cfg)...)
	if len(errs) > 0 {
		return WebConfig{}, errors.Join(errs...)
	}
	return cfg, nil
}

func validateWebConfig(base Config, cfg WebConfig) []error {
	var errs []error
	if err := base.ValidateTestnet(); err != nil {
		errs = append(errs, fmt.Errorf("Bybit Testnet configuration: %w", err))
	}
	if err := base.Deribit.ValidateTestnet(); err != nil {
		errs = append(errs, fmt.Errorf("Deribit Testnet configuration: %w", err))
	}

	hostIP := net.ParseIP(cfg.Host)
	if hostIP == nil || (!hostIP.IsPrivate() && !hostIP.IsLoopback()) || hostIP.IsUnspecified() {
		errs = append(errs, errors.New("WEB_HOST must be a private or loopback IP address"))
	}
	if err := validatePublicOrigin(cfg.PublicOrigin); err != nil {
		errs = append(errs, err)
	}
	if len(cfg.TrustedProxyCIDRs) == 0 {
		errs = append(errs, errors.New("WEB_TRUSTED_PROXY_CIDRS must contain at least one private proxy IP"))
	}
	if strings.TrimSpace(cfg.Username) == "" {
		errs = append(errs, errors.New("WEB_USERNAME is required"))
	}
	if utf8.RuneCountInString(cfg.Password) < 12 {
		errs = append(errs, errors.New("WEB_PASSWORD must contain at least 12 characters"))
	}
	if len(cfg.SessionSecret) < 32 {
		errs = append(errs, errors.New("WEB_SESSION_SECRET is required"))
	}
	if !cfg.BybitEnabled && !cfg.DeribitEnabled {
		errs = append(errs, errors.New("at least one of BYBIT_ENABLED or DERIBIT_ENABLED must be true for web"))
	}
	if cfg.DefaultVenue != domain.VenueBybit && cfg.DefaultVenue != domain.VenueDeribit {
		errs = append(errs, errors.New("WEB_DEFAULT_VENUE must be bybit or deribit"))
	} else if (cfg.DefaultVenue == domain.VenueBybit && !cfg.BybitEnabled) || (cfg.DefaultVenue == domain.VenueDeribit && !cfg.DeribitEnabled) {
		errs = append(errs, errors.New("WEB_DEFAULT_VENUE must be enabled"))
	}
	if cfg.BybitEnabled {
		if err := base.RequireRESTCredentials(); err != nil {
			errs = append(errs, fmt.Errorf("enabled Bybit web connection: %w", err))
		}
	}
	if cfg.DeribitEnabled {
		if err := base.Deribit.RequireCredentials(); err != nil {
			errs = append(errs, fmt.Errorf("enabled Deribit web connection: %w", err))
		}
	}

	for name, value := range map[string]string{
		"QUICK_TRADE_MAX_BTC":          cfg.QuickTradeMaxBTC,
		"QUICK_TRADE_MAX_ETH":          cfg.QuickTradeMaxETH,
		"QUICK_TRADE_MAX_USDT":         cfg.QuickTradeMaxUSDT,
		"QUICK_TRADE_MAX_USDC":         cfg.QuickTradeMaxUSDC,
		"DEMO_MAX_BYBIT_BTC_QTY":       cfg.MaxBybitBTCQty,
		"DEMO_MAX_BYBIT_ETH_QTY":       cfg.MaxBybitETHQty,
		"DEMO_MAX_BYBIT_USDT_AMOUNT":   cfg.MaxBybitUSDTAmount,
		"DEMO_MAX_DERIBIT_BTC_AMOUNT":  cfg.MaxDeribitBTCAmount,
		"DEMO_MAX_DERIBIT_USDC_AMOUNT": cfg.MaxDeribitUSDCAmount,
	} {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s must not be empty", name))
			continue
		}
		valueRat, ok := new(big.Rat).SetString(value)
		if !ok || valueRat.Sign() <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive decimal", name))
		}
	}
	return errs
}

func validatePublicOrigin(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("WEB_PUBLIC_ORIGIN must be an HTTPS origin without path, userinfo, query, or fragment")
	}
	if parsed.Path == "/" {
		return errors.New("WEB_PUBLIC_ORIGIN must not contain a trailing path")
	}
	return nil
}

func parseWebBool(name string, fallback bool, errs *[]error) bool {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return fallback
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be true or false", name))
		return false
	}
	return value
}

func parseWebInt(name string, fallback, min, max int, errs *[]error) int {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < min || value > max {
		*errs = append(*errs, fmt.Errorf("%s must be an integer from %d to %d", name, min, max))
		return 0
	}
	return value
}

func parseWebDuration(name string, fallback time.Duration, errs *[]error) time.Duration {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return fallback
	}
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive duration", name))
		return 0
	}
	return value
}

func webEnv(name, fallback string) string {
	if value, exists := os.LookupEnv(name); exists {
		return strings.TrimSpace(value)
	}
	return fallback
}

func (c WebConfig) ListenAddress() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}
