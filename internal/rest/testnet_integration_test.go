package rest

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"
)

// TestTestnetOrderLifecycle is deliberately opt-in. CI remains credential-free;
// run it only with newly generated Testnet credentials:
// RUN_BYBIT_INTEGRATION=1 BYBIT_API_KEY=... BYBIT_API_SECRET=... go test ./internal/rest -run TestTestnetOrderLifecycle -v
func TestTestnetOrderLifecycle(t *testing.T) {
	if os.Getenv("RUN_BYBIT_INTEGRATION") != "1" {
		t.Skip("set RUN_BYBIT_INTEGRATION=1 with fresh Bybit Testnet credentials")
	}
	apiKey, apiSecret := os.Getenv("BYBIT_API_KEY"), os.Getenv("BYBIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		t.Fatal("BYBIT_API_KEY and BYBIT_API_SECRET are required")
	}
	client := NewClient("https://api-testnet.bybit.com", apiKey, apiSecret)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	serverTime, _, err := client.ServerTime(ctx)
	if err != nil || serverTime.UnixMilli == 0 {
		t.Fatalf("server time: value=%+v err=%v", serverTime, err)
	}
	instruments, _, err := client.Instruments(ctx, "linear", "BTCUSDT")
	if err != nil || len(instruments) != 1 {
		t.Fatalf("instrument metadata: count=%d err=%v", len(instruments), err)
	}
	tickers, _, err := client.Tickers(ctx, "linear", "BTCUSDT")
	if err != nil || len(tickers) != 1 {
		t.Fatalf("ticker: count=%d err=%v", len(tickers), err)
	}
	price, qty := safeFixtureOrder(t, instruments[0], tickers[0])
	linkID, err := GenerateOrderLinkID(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ack, _, err := client.PlaceOrder(ctx, PlaceOrderRequest{Category: "linear", Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: qty, Price: price, TimeInForce: "GTC", OrderLinkID: linkID})
	if err != nil {
		t.Fatalf("place safe limit order: %v", err)
	}
	// Cleanup is attempted even after later assertions; this order is priced
	// below the current bid but remains a real Testnet order.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _, _ = client.CancelOrder(cleanupCtx, CancelOrderRequest{Category: "linear", Symbol: "BTCUSDT", OrderID: ack.OrderID})
	})
	orders, _, err := client.Orders(ctx, "linear", "BTCUSDT", ack.OrderID, "", nil)
	if err != nil || len(orders) == 0 {
		t.Fatalf("query placed order: count=%d err=%v", len(orders), err)
	}
	if _, _, err := client.CancelOrder(ctx, CancelOrderRequest{Category: "linear", Symbol: "BTCUSDT", OrderID: ack.OrderID}); err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	openOnly := 2
	orders, _, err = client.Orders(ctx, "linear", "BTCUSDT", ack.OrderID, "", &openOnly)
	if err != nil || len(orders) == 0 {
		t.Fatalf("query terminal order: count=%d err=%v", len(orders), err)
	}
	if _, _, err := client.Executions(ctx, "linear", "BTCUSDT", ack.OrderID, ""); err != nil {
		t.Fatalf("execution query: %v", err)
	}
	if _, _, err := client.Positions(ctx, "linear", "BTCUSDT"); err != nil {
		t.Fatalf("position query: %v", err)
	}
}

func safeFixtureOrder(t *testing.T, instrument Instrument, ticker Ticker) (price, qty string) {
	t.Helper()
	last := mustRat(t, ticker.LastPrice)
	tick := mustRat(t, instrument.PriceFilter.TickSize)
	step := mustRat(t, instrument.LotSizeFilter.QtyStep)
	minQty := mustRat(t, instrument.LotSizeFilter.MinOrderQty)
	minNotional := mustRat(t, instrument.LotSizeFilter.MinNotional)

	// Five percent below last price is intended not to execute immediately.
	wantedPrice := new(big.Rat).Mul(last, big.NewRat(95, 100))
	priceRat := floorToStep(wantedPrice, tick)
	wantedQty := new(big.Rat).Quo(minNotional, priceRat)
	qtyRat := ceilToStep(wantedQty, step)
	if qtyRat.Cmp(minQty) < 0 {
		qtyRat = minQty
	}
	return ratDecimal(priceRat, decimalPlaces(instrument.PriceFilter.TickSize)), ratDecimal(qtyRat, decimalPlaces(instrument.LotSizeFilter.QtyStep))
}

func mustRat(t *testing.T, value string) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(value)
	if !ok || r.Sign() <= 0 {
		t.Fatalf("invalid positive decimal %q", value)
	}
	return r
}

func floorToStep(value, step *big.Rat) *big.Rat {
	units := new(big.Rat).Quo(value, step)
	whole := new(big.Int).Quo(units.Num(), units.Denom())
	return new(big.Rat).Mul(new(big.Rat).SetInt(whole), step)
}

func ceilToStep(value, step *big.Rat) *big.Rat {
	units := new(big.Rat).Quo(value, step)
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(units.Num(), units.Denom(), remainder)
	if remainder.Sign() != 0 {
		whole.Add(whole, big.NewInt(1))
	}
	return new(big.Rat).Mul(new(big.Rat).SetInt(whole), step)
}

func decimalPlaces(value string) int {
	for i := range value {
		if value[i] == '.' {
			return len(value) - i - 1
		}
	}
	return 0
}

func ratDecimal(value *big.Rat, places int) string { return value.FloatString(places) }
