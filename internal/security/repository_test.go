package security

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRepositoryContainsNoCredentialAssignmentsOrPrivateKeys(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	assignment := regexp.MustCompile(`(?m)(?:BYBIT_(?:API_KEY|API_SECRET|FIX_API_KEY)|DERIBIT_(?:API_KEY|API_SECRET))=[A-Za-z0-9_\-]{16,}`)
	privateKey := regexp.MustCompile(`BEGIN (?:RSA )?PRIVATE KEY`)
	specExamples := map[string]bool{
		"docs/1_bybit/05_TEST_PLAN.md":              true,
		"docs/1_bybit/06_SECURITY_OPERATIONS.md":    true,
		"docs/1_bybit/BYBIT_CONNECTOR_FULL_SPEC.md": true,
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, _ := filepath.Rel(root, path)
		if !entry.IsDir() && (entry.Name() == ".env" || entry.Name() == ".env~") {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".idea") {
			return filepath.SkipDir
		}
		if entry.IsDir() || specExamples[filepath.ToSlash(relative)] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if assignment.Match(data) {
			t.Errorf("possible Bybit credential assignment in %s", relative)
		}
		if privateKey.Match(data) {
			t.Errorf("possible private key in %s", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnvironmentExampleIsEmptyAndSecretsAreIgnored(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	file, err := os.Open(filepath.Join(root, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	allowed := map[string]string{
		"BYBIT_ENV":                       "testnet",
		"BYBIT_ENABLED":                   "false",
		"BYBIT_ACCOUNT_ALIAS":             "bybit-test",
		"BYBIT_API_KEY":                   "",
		"BYBIT_API_SECRET":                "",
		"BYBIT_REST_BASE_URL":             "https://api-testnet.bybit.com",
		"BYBIT_WS_PUBLIC_URL":             "wss://stream-testnet.bybit.com/v5/public/linear",
		"BYBIT_WS_SPOT_URL":               "wss://stream-testnet.bybit.com/v5/public/spot",
		"BYBIT_WS_PRIVATE_URL":            "wss://stream-testnet.bybit.com/v5/private",
		"BYBIT_WS_TRADE_URL":              "wss://stream-testnet.bybit.com/v5/trade",
		"BYBIT_FIX_ADDRESS":               "fix-oe-testnet.bybit.com:9000",
		"BYBIT_FIX_API_KEY":               "",
		"BYBIT_FIX_PRIVATE_KEY_PATH":      "",
		"BYBIT_STATE_FILE":                "state/orders.json",
		"BYBIT_WS_TEST_DURATION":          "5m",
		"MULTI_VENUE_INTENT_FILE":         "state/intents.json",
		"DERIBIT_ENV":                     "testnet",
		"DERIBIT_ENABLED":                 "false",
		"DERIBIT_ACCOUNT_ALIAS":           "deribit-test",
		"DERIBIT_API_KEY":                 "",
		"DERIBIT_API_SECRET":              "",
		"DERIBIT_HTTP_BASE_URL":           "https://test.deribit.com/api/v2",
		"DERIBIT_WS_URL":                  "wss://test.deribit.com/ws/api/v2",
		"DERIBIT_FIX_ENABLED":             "false",
		"DERIBIT_FIX_ADDRESS":             "fix-test.deribit.com:9883",
		"DERIBIT_FIX_SENDER_COMP_ID":      "venuewire",
		"DERIBIT_MAX_ORDER_USD":           "100",
		"DERIBIT_MAX_OPEN_USD":            "500",
		"DERIBIT_MAX_PRICE_DEVIATION_PCT": "2",
		"DERIBIT_MAX_OPEN_ORDERS":         "5",
		"DERIBIT_PLAN_TTL":                "30s",
		"RUN_MULTI_VENUE_E2E":             "0",
		"RUN_BYBIT_READ_TESTS":            "0",
		"RUN_BYBIT_TRADING_TESTS":         "0",
		"RUN_BYBIT_FIX_TESTS":             "0",
		"RUN_DERIBIT_READ_TESTS":          "0",
		"RUN_DERIBIT_TRADING_TESTS":       "0",
		"RUN_DERIBIT_FIX_TESTS":           "0",
		"WEB_HOST":                        "10.0.0.20",
		"WEB_PORT":                        "8080",
		"WEB_PUBLIC_ORIGIN":               "https://trade.example.com",
		"WEB_TRUSTED_PROXY_CIDRS":         "10.0.0.10/32",
		"WEB_USERNAME":                    "",
		"WEB_PASSWORD":                    "",
		"WEB_SESSION_SECRET":              "",
		"WEB_SESSION_TTL":                 "8h",
		"WEB_DEFAULT_VENUE":               "bybit",
		"WEB_TRADING_ENABLED":             "false",
		"WEB_PUSH_INTERVAL":               "250ms",
		"ACCOUNT_RECONCILE_INTERVAL":      "30s",
		"QUOTE_TTL":                       "5s",
		"TRADE_BOOK_MAX_AGE":              "3s",
		"VALUATION_PRICE_MAX_AGE":         "15s",
		"QUICK_TRADE_SLIPPAGE_BPS":        "50",
		"QUICK_TRADE_MAX_BTC":             "0.01",
		"QUICK_TRADE_MAX_ETH":             "1",
		"QUICK_TRADE_MAX_USDT":            "1000",
		"DEMO_MAX_TRADES_PER_SESSION":     "10",
		"DEMO_MAX_TRADES_PER_HOUR":        "30",
		"DEMO_MAX_CONCURRENT_TRADES":      "1",
		"DEMO_MAX_BYBIT_BTC_QTY":          "0.01",
		"DEMO_MAX_BYBIT_USDT_AMOUNT":      "1000",
		"DEMO_MAX_DERIBIT_BTC_AMOUNT":     "0.01",
		"DEMO_MAX_DERIBIT_ETH_AMOUNT":     "1",
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			t.Errorf("malformed .env.example line %q", line)
			continue
		}
		if expected, ok := allowed[key]; !ok || value != expected {
			t.Errorf("unsafe .env.example line %q", line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{".env\n", ".env.*", ".env~", "*.pem", "*.key", "secrets/", "state/"} {
		if !strings.Contains(string(ignore), pattern) {
			t.Errorf(".gitignore missing %q", pattern)
		}
	}
}

func TestNoEnvironmentOrKeyFileIsTrackedWhenGitIsAvailable(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	command := exec.Command("git", "-C", root, "ls-files", "-z")
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Skip("workspace is not a git repository")
		}
		t.Fatal(err)
	}
	for _, raw := range strings.Split(string(output), "\x00") {
		name := filepath.Base(raw)
		if name == ".env" || name == ".env~" || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") {
			t.Errorf("sensitive file is tracked: %s", raw)
		}
	}
}
