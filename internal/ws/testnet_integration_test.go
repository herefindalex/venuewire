package ws

import (
	"context"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestPublicTestnetFiveMinutes is opt-in because routine CI must not depend on
// an external exchange. Its default duration is the five-minute acceptance
// interval; BYBIT_WS_TEST_DURATION may shorten local diagnostics only.
func TestPublicTestnetFiveMinutes(t *testing.T) {
	if os.Getenv("RUN_BYBIT_READ_TESTS") != "1" {
		t.Skip("set RUN_BYBIT_READ_TESTS=1 to run the public Testnet soak")
	}
	duration := 5 * time.Minute
	if configured := os.Getenv("BYBIT_WS_TEST_DURATION"); configured != "" {
		parsed, err := time.ParseDuration(configured)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid BYBIT_WS_TEST_DURATION %q", configured)
		}
		duration = parsed
	}
	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	client := NewClient(Config{
		URL:    "wss://stream-testnet.bybit.com/v5/public/linear",
		Topics: []string{"publicTrade.BTCUSDT", "orderbook.50.BTCUSDT"},
	})
	var trades, books atomic.Uint64
	if err := client.Run(ctx, func(_ context.Context, event Event) error {
		switch event.Kind {
		case EventTrade:
			trades.Add(1)
		case EventOrderBook:
			books.Add(1)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if trades.Load() == 0 || books.Load() == 0 {
		t.Fatalf("missing events: trades=%d books=%d stats=%+v", trades.Load(), books.Load(), client.Stats())
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseline+4 && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > baseline+4 {
		t.Fatalf("possible goroutine leak: baseline=%d after=%d", baseline, got)
	}
}
