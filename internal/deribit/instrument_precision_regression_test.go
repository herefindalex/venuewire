package deribit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpotInstrumentUsesContractSizeWhenAmountStepIsAbsent(t *testing.T) {
	// Regression: ISSUE-004 — Deribit Spot omitted amount_step and quotes became unavailable.
	// Found by /qa on 2026-09-10
	// Report: .gstack/qa-reports/qa-report-venue-trade-alert-net-2026-09-10.md
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/public/get_instrument" {
			t.Fatalf("unexpected instrument request: %s", r.URL.String())
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"instrument_name":"ETH_BTC","kind":"spot","base_currency":"ETH","quote_currency":"BTC","tick_size":0.00001,"min_trade_amount":0.0001,"contract_size":0.0001,"maker_commission":0.05,"taker_commission":0,"is_active":true}}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/v2", "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	instrument, err := client.Instrument(context.Background(), "ETH_BTC")
	if err != nil {
		t.Fatal(err)
	}
	if instrument.AmountStep.String() != "0.0001" {
		t.Fatalf("Spot amount step = %q, want contract size", instrument.AmountStep.String())
	}
}
