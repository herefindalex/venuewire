package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/herefindalex/venuewire/internal/config"
	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/intent"
)

func TestDeribitFIXTradingRequiresAllIndependentGates(t *testing.T) {
	for _, name := range []string{
		"DERIBIT_ENV", "DERIBIT_ENABLED", "DERIBIT_API_KEY", "DERIBIT_API_SECRET",
		"DERIBIT_FIX_ENABLED", "DERIBIT_FIX_ADDRESS", "DERIBIT_HTTP_BASE_URL", "DERIBIT_WS_URL",
		"RUN_MULTI_VENUE_E2E", "RUN_DERIBIT_TRADING_TESTS", "RUN_DERIBIT_FIX_TESTS",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("DERIBIT_API_KEY", "fixture-key")
	t.Setenv("DERIBIT_API_SECRET", "fixture-secret")
	t.Setenv("DERIBIT_FIX_ENABLED", "true")
	for _, gate := range []string{"RUN_MULTI_VENUE_E2E", "RUN_DERIBIT_TRADING_TESTS", "RUN_DERIBIT_FIX_TESTS"} {
		t.Setenv("RUN_MULTI_VENUE_E2E", "1")
		t.Setenv("RUN_DERIBIT_TRADING_TESTS", "1")
		t.Setenv("RUN_DERIBIT_FIX_TESTS", "1")
		t.Setenv(gate, "0")
		if err := requireDeribitFIXTradingGates(config.Load()); err == nil {
			t.Fatalf("FIX write accepted with %s disabled", gate)
		}
	}
	t.Setenv("RUN_MULTI_VENUE_E2E", "1")
	t.Setenv("RUN_DERIBIT_TRADING_TESTS", "1")
	t.Setenv("RUN_DERIBIT_FIX_TESTS", "1")
	if err := requireDeribitFIXTradingGates(config.Load()); err != nil {
		t.Fatalf("all gates enabled: %v", err)
	}
}

func TestDeribitConnectionCODRequiresGeneralTradingGates(t *testing.T) {
	for _, gate := range []string{"RUN_MULTI_VENUE_E2E", "RUN_DERIBIT_TRADING_TESTS"} {
		t.Setenv("RUN_MULTI_VENUE_E2E", "1")
		t.Setenv("RUN_DERIBIT_TRADING_TESTS", "1")
		t.Setenv(gate, "0")
		if err := requireDeribitTradingGates(); err == nil {
			t.Fatalf("connection COD accepted %s disabled", gate)
		}
	}
	t.Setenv("RUN_MULTI_VENUE_E2E", "1")
	t.Setenv("RUN_DERIBIT_TRADING_TESTS", "1")
	if err := requireDeribitTradingGates(); err != nil {
		t.Fatalf("all general trading gates enabled: %v", err)
	}
}

func TestFIXAmendCancelRequiresConnectorOwnedIdentity(t *testing.T) {
	ctx := context.Background()
	store := intent.Store{Path: filepath.Join(t.TempDir(), "intents.json")}
	plan := intent.Plan{
		ID:            "intent-1",
		Instrument:    "BTC-PERPETUAL",
		Transport:     "fix",
		NativeOrderID: "native-1",
		Status:        intent.StatusSubmitted,
	}
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	order := deribit.Order{OrderID: "native-1", Label: "intent-1", InstrumentName: "BTC-PERPETUAL"}
	if _, err := requireConnectorOwnedFIXOrder(ctx, store, order); err != nil {
		t.Fatalf("owned order rejected: %v", err)
	}
	order.OrderID = "different-native"
	if _, err := requireConnectorOwnedFIXOrder(ctx, store, order); err == nil {
		t.Fatal("native-ID mismatch was accepted")
	}
	order = deribit.Order{OrderID: "native-2", Label: "external", InstrumentName: "BTC-PERPETUAL"}
	if _, err := requireConnectorOwnedFIXOrder(ctx, store, order); err == nil {
		t.Fatal("unowned order was accepted")
	}
}

func TestDeribitFIXStateMappingIsExplicit(t *testing.T) {
	tests := map[string]string{"0": "open", "1": "open", "2": "filled", "4": "cancelled", "8": "rejected", "unexpected": "unknown"}
	for input, want := range tests {
		if got := deribitOrderStateFromFIX(input); got != want {
			t.Errorf("status %q maps to %q, want %q", input, got, want)
		}
	}
}
